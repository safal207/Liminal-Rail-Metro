package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/safal207/Liminal-Rail-Metro/internal/codexplugin"
)

func main() {
	defaultAddr, err := codexplugin.ListenAddressFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	addr := flag.String("addr", defaultAddr, "listen address; localhost by default, 0.0.0.0:$PORT on hosted platforms")
	mcpPath := flag.String("mcp-path", "/mcp", "MCP Streamable HTTP endpoint path")
	flag.Parse()

	runtime, err := codexplugin.NewRuntimeFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	publicConfig, err := codexplugin.PublicHTTPConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	server := runtime.NewMCPServer()
	mcpHandler, err := codexplugin.HardenMCPHandler(codexplugin.MCPHandler(server), publicConfig)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle(*mcpPath, mcpHandler)
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"status":  "ok",
			"version": codexplugin.Version,
		})
	})

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Liminal Rail Codex MCP v%s listening on %s%s", codexplugin.Version, *addr, *mcpPath)
	log.Fatal(httpServer.ListenAndServe())
}
