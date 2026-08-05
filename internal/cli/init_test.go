package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/renderers"
)

func TestRunInit_WritesLoadableAndRenderableRegistry(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	if err := runInit(&out, dir); err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	reg, verrs, vwarns, err := registry.Load(dir)
	if err != nil {
		t.Fatalf("registry.Load(%q) returned hard error: %v", dir, err)
	}
	if len(verrs) != 0 {
		t.Fatalf("registry.Load(%q) returned validation errors: %v", dir, verrs)
	}
	if len(vwarns) != 0 {
		t.Errorf("registry.Load(%q) returned unexpected warnings: %v", dir, vwarns)
	}

	// The regression this guards against: a fresh init'd registry with no
	// bash.yaml has no "global" bash profile, and both renderers
	// unconditionally compile it — Render used to fail with
	// `bash profile "global" not found` immediately after init.
	for _, r := range renderers.All() {
		if _, err := r.Render(reg, render.Options{RegistryRoot: reg.RootDir}); err != nil {
			t.Errorf("renderer %q failed to render a freshly-init'd registry: %v", r.ID(), err)
		}
	}
}

func TestRunInit_WritesBashYAMLWithGlobalProfile(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	if err := runInit(&out, dir); err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	path := filepath.Join(dir, "bash.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected init to write %s: %v", path, err)
	}

	var fc struct {
		Bash *registry.BashPolicy `yaml:"bash"`
	}
	if err := yaml.Unmarshal(data, &fc); err != nil {
		t.Fatalf("bash.yaml did not parse: %v", err)
	}
	if fc.Bash == nil {
		t.Fatalf("bash.yaml has no top-level bash: key")
	}
	profile, ok := fc.Bash.Profiles["global"]
	if !ok {
		t.Fatalf("bash.yaml has no \"global\" profile: %+v", fc.Bash.Profiles)
	}
	if profile.Base != registry.Allow {
		t.Errorf("global profile base = %q, want %q", profile.Base, registry.Allow)
	}
}

func TestRunInit_AgentcfgYAMLImportsBashYAML(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	if err := runInit(&out, dir); err != nil {
		t.Fatalf("runInit returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agentcfg.yaml"))
	if err != nil {
		t.Fatalf("ReadFile agentcfg.yaml: %v", err)
	}

	var fc struct {
		Imports []string `yaml:"imports"`
	}
	if err := yaml.Unmarshal(data, &fc); err != nil {
		t.Fatalf("agentcfg.yaml did not parse: %v", err)
	}

	found := false
	for _, imp := range fc.Imports {
		if imp == "bash.yaml" {
			found = true
		}
	}
	if !found {
		t.Errorf("agentcfg.yaml imports = %v, want it to include bash.yaml", fc.Imports)
	}
}
