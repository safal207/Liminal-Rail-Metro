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
	addr := flag.String("addr", "127.0.0.1:8787", "listen address; localhost is the safe development default")
	mcpPath := flag.String("mcp-path", "/mcp", "MCP Streamable HTTP endpoint path")
	flag.Parse()

	runtime, err := codexplugin.NewRuntimeFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	server := runtime.NewMCPServer()

	mux := http.NewServeMux()
	mux.Handle(*mcpPath, codexplugin.MCPHandler(server))
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"status":  "ok",
			"version": codexplugin.Version,
		})
	})

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Liminal Rail Codex MCP v%s listening on http://%s%s", codexplugin.Version, *addr, *mcpPath)
	log.Fatal(httpServer.ListenAndServe())
}
