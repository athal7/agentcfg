package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunImport_FreshRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create dummy opencode config in home
	configDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	opencodeJSON := `{
		"model": "anthropic/claude-sonnet-4-5",
		"small_model": "anthropic/claude-haiku-4-5",
		"default_agent": "lead",
		"agent": {
			"lead": {
				"description": "Primary orchestrator",
				"prompt": "You orchestrate.",
				"mode": "primary"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), []byte(opencodeJSON), 0o644); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}

	regDir := filepath.Join(t.TempDir(), "imported-registry")

	var out bytes.Buffer
	if err := runImport(&out, "opencode", regDir, false); err != nil {
		t.Fatalf("runImport error: %v", err)
	}

	if !strings.Contains(out.String(), "imported configuration into agentcfg registry at") {
		t.Errorf("unexpected output: %q", out.String())
	}

	// Verify agentcfg.yaml exists in regDir
	if _, err := os.Stat(filepath.Join(regDir, "agentcfg.yaml")); err != nil {
		t.Errorf("agentcfg.yaml not created: %v", err)
	}

	// Verify running import again without --force fails
	out.Reset()
	if err := runImport(&out, "opencode", regDir, false); err == nil {
		t.Errorf("runImport second run should have failed without --force")
	}

	// Verify running import again with --force succeeds
	out.Reset()
	if err := runImport(&out, "opencode", regDir, true); err != nil {
		t.Errorf("runImport second run with --force failed: %v", err)
	}
}
