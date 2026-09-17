package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep the distributable skill independent of the plugin cache's working
// directory and ensure the manifest resolves to a real, bounded workflow.
func TestPluginPackage(t *testing.T) {
	root := "../../plugins/liminal-rail-metro"
	data, err := os.ReadFile(filepath.Join(root, ".codex-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Skills     string `json:"skills"`
		MCPServers any    `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != filepath.Base(root) || manifest.Version != "0.1.0" || manifest.Skills != "./skills/" || manifest.MCPServers != nil {
		t.Fatal("incorrect skills-only package manifest")
	}
	skill, err := os.ReadFile(filepath.Join(root, manifest.Skills, "metro-action/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"name: metro-action", "metro-codex run", "metro-codex verify", "REQUIRE_APPROVAL", "ESCALATE_SYSTEM2", "side_effect"} {
		if !strings.Contains(string(skill), required) {
			t.Fatalf("skill missing %q", required)
		}
	}
	if strings.Contains(string(data)+string(skill), "[TODO:") {
		t.Fatal("unfinished plugin package")
	}
}
