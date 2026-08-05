package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"

	"github.com/athal7/agentcfg/internal/render"
)

func TestApplyMergeJSON_TargetFileDoesNotExistYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "opencode.json")

	applied, skipped, err := applyMergeJSON(render.MergeJSON{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeJSON returned error: %v", err)
	}
	if skipped != "" {
		t.Fatalf("skipped = %q, want empty", skipped)
	}
	if applied == "" {
		t.Errorf("applied is empty, want a description")
	}

	got := readJSON(t, path)
	want := map[string]any{"model": "claude-opus"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeJSON_UnrelatedExistingKeysSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	writeJSON(t, path, map[string]any{
		"model":            "old-model",
		"user_preferences": map[string]any{"theme": "dark"},
	})

	_, _, err := applyMergeJSON(render.MergeJSON{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeJSON returned error: %v", err)
	}

	got := readJSON(t, path)
	want := map[string]any{
		"model":            "claude-opus",
		"user_preferences": map[string]any{"theme": "dark"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeJSON_WildcardPathDoesNotDeleteUnlistedSiblings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	writeJSON(t, path, map[string]any{
		"agent": map[string]any{
			"reviewer": map[string]any{"temperature": 0.2, "model": "old"},
			"lead":     map[string]any{"temperature": 0.9},
		},
	})

	_, _, err := applyMergeJSON(render.MergeJSON{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"agent.*.model"},
		Object: map[string]any{
			"agent": map[string]any{
				"lead":  map[string]any{"model": "claude-opus", "description": "leads"},
				"build": map[string]any{"model": "claude-sonnet"},
			},
		},
	})
	if err != nil {
		t.Fatalf("applyMergeJSON returned error: %v", err)
	}

	got := readJSON(t, path)
	want := map[string]any{
		"agent": map[string]any{
			// reviewer isn't in Object.agent at all — must survive untouched.
			"reviewer": map[string]any{"temperature": 0.2, "model": "old"},
			// lead is in Object.agent — only .model is spliced in;
			// temperature (not part of the managed leaf) survives.
			"lead": map[string]any{"temperature": 0.9, "model": "claude-opus"},
			// build is new — created from the managed leaf.
			"build": map[string]any{"model": "claude-sonnet"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeJSON_WholeSubtreePathFullyReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	writeJSON(t, path, map[string]any{
		"permission": map[string]any{
			"bash": map[string]any{"*": "allow"},
			"edit": "deny",
		},
	})

	_, _, err := applyMergeJSON(render.MergeJSON{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"permission"},
		Object: map[string]any{
			"permission": map[string]any{
				"bash": map[string]any{"*": "ask"},
			},
		},
	})
	if err != nil {
		t.Fatalf("applyMergeJSON returned error: %v", err)
	}

	got := readJSON(t, path)
	// permission is a whole-subtree managed path: "edit" from the old file
	// must NOT survive — the whole subtree is replaced by Object.permission.
	want := map[string]any{
		"permission": map[string]any{
			"bash": map[string]any{"*": "ask"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeJSON_MultipleManagedPathsInSameObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	writeJSON(t, path, map[string]any{
		"small_model": "old-small",
		"unrelated":   "keep-me",
	})

	_, _, err := applyMergeJSON(render.MergeJSON{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model", "small_model"},
		Object: map[string]any{
			"model":       "claude-opus",
			"small_model": "claude-haiku",
		},
	})
	if err != nil {
		t.Fatalf("applyMergeJSON returned error: %v", err)
	}

	got := readJSON(t, path)
	want := map[string]any{
		"model":       "claude-opus",
		"small_model": "claude-haiku",
		"unrelated":   "keep-me",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeJSON_GitTrackedFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	path := filepath.Join(dir, "opencode.json")
	writeJSON(t, path, map[string]any{"model": "hand-edited"})
	gitAdd(t, dir, "opencode.json")

	applied, skipped, err := applyMergeJSON(render.MergeJSON{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeJSON returned error: %v", err)
	}
	if applied != "" {
		t.Errorf("applied = %q, want empty (should have been skipped)", applied)
	}
	if skipped == "" {
		t.Fatalf("skipped is empty, want a git-tracked skip reason")
	}

	got := readJSON(t, path)
	want := map[string]any{"model": "hand-edited"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("file was modified despite being git-tracked: got %#v, want %#v", got, want)
	}
}

func TestApplyMergeYAML_TargetFileDoesNotExistYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	_, _, err := applyMergeYAML(render.MergeYAML{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeYAML returned error: %v", err)
	}

	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	want := map[string]any{"model": "claude-opus"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeYAML_UnrelatedExistingKeysSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("model: old\nunrelated: keep-me\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := applyMergeYAML(render.MergeYAML{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeYAML returned error: %v", err)
	}

	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	want := map[string]any{"model": "claude-opus", "unrelated": "keep-me"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeTOML_SyntheticRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("model = \"old\"\nunrelated = \"keep-me\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := applyMergeTOML(render.MergeTOML{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeTOML returned error: %v", err)
	}

	var got map[string]any
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("toml.DecodeFile: %v", err)
	}
	want := map[string]any{"model": "claude-opus", "unrelated": "keep-me"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestApplyMergeTOML_TargetFileDoesNotExistYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	_, _, err := applyMergeTOML(render.MergeTOML{
		Path:    path,
		Mode:    0o600,
		Managed: []string{"model"},
		Object:  map[string]any{"model": "claude-opus"},
	})
	if err != nil {
		t.Fatalf("applyMergeTOML returned error: %v", err)
	}

	var got map[string]any
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("toml.DecodeFile: %v", err)
	}
	want := map[string]any{"model": "claude-opus"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return v
}
