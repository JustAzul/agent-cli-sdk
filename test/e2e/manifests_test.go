package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func repoJSON(t *testing.T, rel string) map[string]any {
	t.Helper()
	return readJSONFile(t, filepath.Join(repoRoot, rel))
}

func TestPluginManifest(t *testing.T) {
	m := repoJSON(t, "plugin/.claude-plugin/plugin.json")
	if _, has := m["version"]; has {
		t.Error("plugin.json must not carry a version field")
	}
	for k, want := range map[string]any{
		"name":       "agent-cli",
		"license":    "MIT",
		"homepage":   "https://github.com/JustAzul/agent-cli-sdk",
		"repository": "https://github.com/JustAzul/agent-cli-sdk",
		"author":     map[string]any{"name": "Diego (Azul) Ferreira"},
	} {
		if !reflect.DeepEqual(m[k], want) {
			t.Errorf("plugin.json %s = %v, want %v", k, m[k], want)
		}
	}
	if d, _ := m["description"].(string); d == "" {
		t.Error("plugin.json has no description")
	}
}

func TestMarketplaceManifest(t *testing.T) {
	m := repoJSON(t, ".claude-plugin/marketplace.json")
	if m["name"] != "agent-cli-sdk" {
		t.Errorf("marketplace name = %v", m["name"])
	}
	if !reflect.DeepEqual(m["owner"], map[string]any{"name": "JustAzul"}) {
		t.Errorf("owner = %v", m["owner"])
	}
	plugins, _ := m["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("plugins = %v, want exactly one", m["plugins"])
	}
	p := plugins[0].(map[string]any)
	if p["name"] != "agent-cli" {
		t.Errorf("plugin name = %v", p["name"])
	}
	want := map[string]any{"source": "github", "repo": "JustAzul/agent-cli-sdk", "ref": "dist"}
	if !reflect.DeepEqual(p["source"], want) {
		t.Errorf("source = %v, want %v (no sha)", p["source"], want)
	}
}

func TestHooksRunLinkOnSessionStart(t *testing.T) {
	m := repoJSON(t, "plugin/hooks/hooks.json")
	groups, _ := m["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(groups) != 1 {
		t.Fatalf("SessionStart = %v", groups)
	}
	hooks, _ := groups[0].(map[string]any)["hooks"].([]any)
	if len(hooks) != 1 {
		t.Fatalf("SessionStart hooks = %v", hooks)
	}
	h := hooks[0].(map[string]any)
	if h["type"] != "command" || h["command"] != `"${CLAUDE_PLUGIN_ROOT}"/bin/agentcli link --quiet` || h["timeout"] != float64(5) {
		t.Errorf("hook = %v", h)
	}
}

func TestShimIsExecutableShellScript(t *testing.T) {
	p := filepath.Join(repoRoot, "plugin", "bin", "agentcli")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable (%v)", p, st.Mode())
	}
	b, _ := json.Marshal(readFile(t, p)[:10])
	if got := readFile(t, p)[:10]; got != "#!/bin/sh\n" {
		t.Errorf("shim starts with %s", b)
	}
}
