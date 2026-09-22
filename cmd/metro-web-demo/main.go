// metro-web-demo is a loopback-only, deterministic read-only graph demonstrator.
// It is not a public gateway, LLM integration, or general-purpose file executor.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed index.html
var page string

// handler serves the local UI and bounded demo API after Host and Origin checks.
func handler(e *engine, host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		if r.Host != host {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/":
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			html := page
			mode := ""
			if e.store != nil {
				mode = "window.__PERSISTENT_MEMORY__=true;"
			}
			if e.resource != nil {
				resourceMode := "window.__FILE_RESOURCE__=true;"
				if e.resource.origin().Transport == "http" {
					resourceMode = "window.__HTTP_RESOURCE__=true;"
				}
				mode += resourceMode
				if e.remoteGraph != nil || e.graphFile != nil {
					mode += "window.__PUBLISHED_GRAPH__=true;"
				}
			}
			html = strings.Replace(html, "/*REPLAY_DATA*/", mode, 1)
			_, _ = io.WriteString(w, html)
		case "/api/manifest":
			e.serveGraph(w, r)
		case "/api/resource", "/api/resource/content":
			e.serveResource(w, r)
		case "/api/run":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2048)
			defer r.Body.Close()
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			var req runRequest
			if err := dec.Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			var trailing any
			if dec.Decode(&trailing) != io.EOF || !req.valid() {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			out, err := e.runContext(r.Context(), req)
			if err != nil {
				http.Error(w, "local execution error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		default:
			http.NotFound(w, r)
		}
	})
}

type recordedCase struct {
	Label   string     `json:"label"`
	Request runRequest `json:"request"`
	Result  runResult  `json:"result"`
}

// exportReplay writes an offline viewer containing results from nine actual local runs.
func exportReplay(path string) error {
	e := newEngine()
	cases := []recordedCase{}
	labels := []string{"Первый проход", "Повтор: память маршрута", "Другой бюджет: свежий результат", "Новая версия карты", "Доступ запрещён", "Ответ потерян", "Подмена результата", "Цикл: цели нет", "Лимит переходов"}
	requests := []runRequest{{300, "normal"}, {300, "normal"}, {200, "normal"}, {300, "new_version"}, {300, "denied"}, {300, "lost_response"}, {300, "tampered"}, {300, "cycle"}, {300, "step_limit"}}
	for i, req := range requests {
		out, err := e.run(req)
		if err != nil {
			return err
		}
		cases = append(cases, recordedCase{labels[i], req, out})
	}
	payload, err := json.Marshal(map[string]any{"recorded_at": time.Now().UTC().Format(time.RFC3339), "cases": cases})
	if err != nil {
		return err
	}
	// encoding/json escapes '<', preventing script termination by content.
	html := strings.Replace(page, "/*REPLAY_DATA*/", "window.__REPLAY__="+string(payload)+";", 1)
	return os.WriteFile(path, []byte(html), 0600)
}

// main exports recorded runs or starts the demo on an IPv4 loopback listener.
func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "loopback IPv4 listen address")
	export := flag.String("export", "", "write an offline viewer of actual local runs, then exit")
	resource := flag.String("resource", "", "read a menu JSON file afresh for each run")
	remote := flag.String("remote", "", "read a Metro menu from a fixed HTTP(S) origin")
	graphFile := flag.String("graph-file", "", "publish a v0.2 menu graph from a local JSON file (requires -resource)")
	remoteGraph := flag.Bool("remote-graph", false, "fetch a fresh v0.2 graph from the configured -remote origin")
	memory := flag.String("memory", "", "persist bounded route memory in a dedicated local file (one process per file)")
	flag.Parse()
	if *export != "" && *memory != "" {
		log.Fatal("-memory cannot be combined with -export")
	}
	if (*export != "" && (*resource != "" || *remote != "")) || (*resource != "" && *remote != "") {
		log.Fatal("choose one of -export, -resource, or -remote")
	}
	if (*graphFile != "" && (*resource == "" || *remoteGraph)) || (*remoteGraph && *remote == "") {
		log.Fatal("-graph-file requires -resource; -remote-graph requires -remote")
	}
	if *export != "" {
		if err := exportReplay(*export); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Recorded local runs:", *export)
		return
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || net.ParseIP(host).To4() == nil {
		log.Fatal("only IPv4 loopback addresses are supported")
	}
	listener, err := net.Listen("tcp4", *addr)
	if err != nil {
		log.Fatal(err)
	}
	actual := listener.Addr().String()
	e := newEngine()
	if *memory != "" {
		if err := e.enableMemory(*memory); err != nil {
			log.Fatalf("route memory: %v", err)
		}
		defer e.store.close()
	}

	if *resource != "" {
		e.resource, err = newResourceSource(*resource)
		if err != nil {
			log.Fatal(err)
		}
		defer e.resource.close()
		if _, err := e.resource.read(context.Background()); err != nil {
			log.Fatal(err)
		}
	}
	if *remote != "" {
		source, sourceErr := newHTTPResourceSource(*remote)
		if sourceErr != nil {
			log.Fatal(sourceErr)
		}
		if source.targets(actual) {
			log.Fatal("remote origin cannot be this server")
		}
		e.resource = source
		defer source.close()
		if *remoteGraph {
			e.remoteGraph = source
		}
	}
	if *graphFile != "" {
		e.graphFile, err = newResourceSource(*graphFile)
		if err != nil {
			log.Fatal(err)
		}
		defer e.graphFile.close()
		if _, err := e.loadGraph(context.Background()); err != nil {
			log.Fatal(err)
		}
	}
	server := &http.Server{Handler: handler(e, actual), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	log.Printf("Metro Web local demo: http://%s (file: %t; HTTP reader: %t; read-only)", actual, *resource != "", *remote != "")
	log.Fatal(server.Serve(listener))
}
