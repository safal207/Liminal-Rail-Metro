package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginPackage(t *testing.T) {
	root := "../../plugins/liminal-rail-metro"

	portableData, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var portable struct {
		Schema     string                     `json:"$schema"`
		Name       string                     `json:"name"`
		Version    string                     `json:"version"`
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	if err := json.Unmarshal(portableData, &portable); err != nil {
		t.Fatal(err)
	}
	if portable.Schema != "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json" ||
		portable.Name != filepath.Base(root) ||
		portable.Version != "0.1.0" {
		t.Fatalf("incorrect portable plugin manifest: %#v", portable)
	}
	if _, ok := portable.Extensions["com.openai"]; !ok {
		t.Fatal("portable manifest missing extensions.com.openai")
	}

	mcpData, err := os.ReadFile(filepath.Join(root, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpManifest struct {
		Schema     string `json:"$schema"`
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpData, &mcpManifest); err != nil {
		t.Fatal(err)
	}
	server, ok := mcpManifest.MCPServers["liminal_rail"]
	if mcpManifest.Schema != "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json" ||
		!ok ||
		server.Type != "streamable-http" ||
		server.URL != "http://127.0.0.1:8787/mcp" {
		t.Fatalf("incorrect MCP manifest: %#v", mcpManifest)
	}

	compatData, err := os.ReadFile(filepath.Join(root, ".codex-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compat struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Skills  string `json:"skills"`
	}
	if err := json.Unmarshal(compatData, &compat); err != nil {
		t.Fatal(err)
	}
	if compat.Name != filepath.Base(root) || compat.Version != "0.1.0" || compat.Skills != "./skills/" {
		t.Fatalf("incorrect compatibility manifest: %#v", compat)
	}

	metroSkill, err := os.ReadFile(filepath.Join(root, compat.Skills, "metro-action/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"name: metro-action",
		"liminal_decide",
		"liminal_verify",
		"metro-codex run",
		"metro-codex verify",
		"REQUIRE_APPROVAL",
		"ESCALATE_SYSTEM2",
		"side_effect",
		"UNKNOWN",
	} {
		if !strings.Contains(string(metroSkill), required) {
			t.Fatalf("metro-action skill missing %q", required)
		}
	}

	verifySkill, err := os.ReadFile(filepath.Join(root, compat.Skills, "verify-proof/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"name: verify-proof",
		"liminal_verify",
		"verified=true",
		"SUCCEEDED",
		"UNKNOWN",
	} {
		if !strings.Contains(string(verifySkill), required) {
			t.Fatalf("verify-proof skill missing %q", required)
		}
	}

	if strings.Contains(string(portableData)+string(mcpData)+string(metroSkill)+string(verifySkill), "[TODO:") {
		t.Fatal("unfinished plugin package")
	}
}
