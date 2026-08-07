package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/render/opencode"
)

// TestApply_OpenCodeRender_PreservesHandSetExternalDirectory reproduces the
// bug in #7: applying an opencode registry against a hand-maintained
// opencode.json that already sets permission.external_directory (a field
// with no registry-level global source) must not drop that key. Before the
// fix, Render's Managed list included the bare "permission" path, which
// merge.go's whole-subtree-replace semantics for a path ending mid-object
// wipe entirely — silently deleting external_directory (and any other
// hand-set permission.* key) on every apply.
func TestApply_OpenCodeRender_PreservesHandSetExternalDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	configPath := filepath.Join(configDir, "opencode.json")
	existing := map[string]any{
		"permission": map[string]any{
			"external_directory": map[string]any{"*": "ask"},
		},
	}
	data, err := json.Marshal(existing)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash: registry.BashPolicy{
			Profiles: map[string]registry.BashProfile{
				"global": {Base: registry.Allow},
			},
		},
		Agents: []registry.Agent{
			{Name: "lead", Mode: "primary", Class: "default", Prompt: registry.Prompt{Text: "You lead."}},
		},
	}

	plan, err := opencode.New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if _, err := Apply(plan, Options{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	perm, ok := got["permission"].(map[string]any)
	if !ok {
		t.Fatalf("permission missing or not an object: %#v", got["permission"])
	}

	extDir, ok := perm["external_directory"]
	if !ok {
		t.Fatal("permission.external_directory was dropped by apply")
	}
	want := map[string]any{"*": "ask"}
	if !reflect.DeepEqual(extDir, want) {
		t.Errorf("external_directory = %#v, want %#v", extDir, want)
	}

	// The agentcfg-managed leaf keys must still land correctly alongside
	// the preserved hand-set key.
	if perm["read"] != "allow" {
		t.Errorf("permission.read = %v, want allow", perm["read"])
	}
	bashMap, ok := perm["bash"].(map[string]any)
	if !ok || bashMap["*"] != "allow" {
		t.Errorf("permission.bash = %#v, want {\"*\": \"allow\"}", perm["bash"])
	}
}
