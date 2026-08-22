package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
)

func TestImportOpencode(t *testing.T) {
	home := t.TempDir()
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
				"prompt": "You orchestrate tasks.",
				"mode": "primary"
			},
			"builder": {
				"description": "Implementer",
				"prompt": "You build code.",
				"steps": 20
			}
		},
		"mcp": {
			"context7": {
				"type": "remote",
				"url": "https://mcp.context7.com/mcp"
			},
			"local-fs": {
				"type": "local",
				"command": ["mcp-server-filesystem", "/tmp"]
			}
		}
	}`

	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), []byte(opencodeJSON), 0o644); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}

	data := NewImportedData()
	if err := importOpencode(home, data); err != nil {
		t.Fatalf("importOpencode error: %v", err)
	}

	if data.ModelClasses["default"] != "anthropic/claude-sonnet-4-5" {
		t.Errorf("got default model %q, want anthropic/claude-sonnet-4-5", data.ModelClasses["default"])
	}
	if data.ModelClasses["smol"] != "anthropic/claude-haiku-4-5" {
		t.Errorf("got smol model %q, want anthropic/claude-haiku-4-5", data.ModelClasses["smol"])
	}
	if len(data.WorkflowSteps) != 2 {
		t.Fatalf("got %d workflow steps, want 2", len(data.WorkflowSteps))
	}
	if len(data.MCPServers) != 2 {
		t.Fatalf("got %d mcp servers, want 2", len(data.MCPServers))
	}

	res, err := SynthesizeRegistry(data)
	if err != nil {
		t.Fatalf("SynthesizeRegistry error: %v", err)
	}

	regDir := t.TempDir()
	for name, content := range res.Files {
		if err := os.WriteFile(filepath.Join(regDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write synthesized %s: %v", name, err)
		}
	}

	reg, _, _, err := registry.Load(regDir)
	if err != nil {
		t.Fatalf("Load synthesized registry error: %v", err)
	}
	if errs, _ := registry.Validate(reg); len(errs) > 0 {
		t.Fatalf("Validate synthesized registry failed with errors: %v", errs)
	}
}

func TestImportOMP(t *testing.T) {
	home := t.TempDir()
	ompDir := filepath.Join(home, ".omp")
	agentsDir := filepath.Join(ompDir, "agent", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	configYML := `modelRoles:
  default: openai/gpt-5.6-terra
  smol: openai/gpt-5.6-mini
`
	if err := os.WriteFile(filepath.Join(ompDir, "config.yml"), []byte(configYML), 0o644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}

	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte("You review code."), 0o644); err != nil {
		t.Fatalf("write reviewer.md: %v", err)
	}

	data := NewImportedData()
	if err := importOMP(home, data); err != nil {
		t.Fatalf("importOMP error: %v", err)
	}

	if data.ModelClasses["default"] != "openai/gpt-5.6-terra" {
		t.Errorf("got default model %q, want openai/gpt-5.6-terra", data.ModelClasses["default"])
	}
	if len(data.WorkflowSteps) != 1 {
		t.Fatalf("got %d workflow steps, want 1", len(data.WorkflowSteps))
	}
}

func TestImportCodex(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	agentsDir := filepath.Join(codexDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	configTOML := `model = "openai/gpt-5"

[mcp_servers.remote-mcp]
url = "https://mcp.example.com/mcp"
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(configTOML), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	agentTOML := `name = "planner"
description = "Architect persona"
developer_instructions = "You plan software designs."
`
	if err := os.WriteFile(filepath.Join(agentsDir, "planner.toml"), []byte(agentTOML), 0o644); err != nil {
		t.Fatalf("write planner.toml: %v", err)
	}

	data := NewImportedData()
	if err := importCodex(home, data); err != nil {
		t.Fatalf("importCodex error: %v", err)
	}

	if data.ModelClasses["default"] != "openai/gpt-5" {
		t.Errorf("got default model %q, want openai/gpt-5", data.ModelClasses["default"])
	}
	if len(data.MCPServers) != 1 {
		t.Fatalf("got %d mcp servers, want 1", len(data.MCPServers))
	}
	if len(data.WorkflowSteps) != 1 {
		t.Fatalf("got %d workflow steps, want 1", len(data.WorkflowSteps))
	}
}

func TestImportClaude(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	settingsJSON := `{"model": "claude-3-5-sonnet-20241022"}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settingsJSON), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	claudeJSON := `{
		"mcpServers": {
			"docs": {
				"type": "http",
				"url": "https://mcp.docs.org"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(claudeJSON), 0o644); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}

	data := NewImportedData()
	if err := importClaude(home, data); err != nil {
		t.Fatalf("importClaude error: %v", err)
	}

	if data.ModelClasses["default"] != "claude-3-5-sonnet-20241022" {
		t.Errorf("got default model %q, want claude-3-5-sonnet-20241022", data.ModelClasses["default"])
	}
	if len(data.MCPServers) != 1 {
		t.Fatalf("got %d mcp servers, want 1", len(data.MCPServers))
	}
}
