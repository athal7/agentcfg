package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"

	"github.com/athal7/agentcfg/internal/registry"
)

// ImportedData represents raw extracted settings across one or more harnesses.
type ImportedData struct {
	ModelClasses  map[string]string
	BashProfiles  map[string]registry.BashProfile
	BashLists     map[string]map[string]registry.Decision
	DefaultLists  []string
	WorkflowSteps []registry.Agent
	MCPServers    map[string]registry.MCPServer
	HarnessExtra  map[string]map[string]any
}

// NewImportedData initializes an empty ImportedData structure.
func NewImportedData() *ImportedData {
	return &ImportedData{
		ModelClasses: make(map[string]string),
		BashProfiles: make(map[string]registry.BashProfile),
		BashLists:    make(map[string]map[string]registry.Decision),
		MCPServers:   make(map[string]registry.MCPServer),
		HarnessExtra: make(map[string]map[string]any),
	}
}

// Options controls import options such as home directory overriding and force overwriting.
type Options struct {
	HomeDir string
	Force   bool
}

// Result holds the generated files to be written to the registry directory.
type Result struct {
	Files map[string]string
}

// ImportTarget identifies a supported harness importer.
type ImportTarget string

const (
	TargetOpencode ImportTarget = "opencode"
	TargetOMP      ImportTarget = "omp"
	TargetCodex    ImportTarget = "codex"
	TargetClaude   ImportTarget = "claude"
)

// ImportHarnesses reads native configs for the requested targets from homeDir and synthesizes registry files.
func ImportHarnesses(targets []ImportTarget, opt Options) (*Result, error) {
	if opt.HomeDir == "" {
		var err error
		opt.HomeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("user home dir: %w", err)
		}
	}

	data := NewImportedData()

	for _, target := range targets {
		switch target {
		case TargetOpencode:
			if err := importOpencode(opt.HomeDir, data); err != nil {
				return nil, fmt.Errorf("import opencode: %w", err)
			}
		case TargetOMP:
			if err := importOMP(opt.HomeDir, data); err != nil {
				return nil, fmt.Errorf("import omp: %w", err)
			}
		case TargetCodex:
			if err := importCodex(opt.HomeDir, data); err != nil {
				return nil, fmt.Errorf("import codex: %w", err)
			}
		case TargetClaude:
			if err := importClaude(opt.HomeDir, data); err != nil {
				return nil, fmt.Errorf("import claude: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown import target %q", target)
		}
	}

	return SynthesizeRegistry(data)
}

// importOpencode parses ~/.config/opencode/opencode.json
func importOpencode(home string, data *ImportedData) error {
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	bytes, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return fmt.Errorf("parsing opencode.json: %w", err)
	}

	// Model classes
	if m, ok := raw["model"].(string); ok && m != "" {
		if _, exists := data.ModelClasses["default"]; !exists {
			data.ModelClasses["default"] = m
		}
	}
	if sm, ok := raw["small_model"].(string); ok && sm != "" {
		if _, exists := data.ModelClasses["smol"]; !exists {
			data.ModelClasses["smol"] = sm
		}
	}

	// MCP servers
	if mcpMap, ok := raw["mcp"].(map[string]any); ok {
		for name, val := range mcpMap {
			srvObj, ok := val.(map[string]any)
			if !ok {
				continue
			}
			srv := registry.MCPServer{Name: name}
			t, _ := srvObj["type"].(string)
			if t == "remote" {
				srv.Transport = "remote"
				if url, ok := srvObj["url"].(string); ok {
					srv.URL = registry.Value{Literal: url}
				}
				if hdrs, ok := srvObj["headers"].(map[string]any); ok {
					srv.Headers = make(map[string]registry.Value)
					for k, v := range hdrs {
						if vs, ok := v.(string); ok {
							srv.Headers[k] = registry.Value{Literal: vs}
						}
					}
				}
			} else if t == "local" {
				srv.Transport = "local"
				if cmdSlice, ok := srvObj["command"].([]any); ok {
					for _, item := range cmdSlice {
						if s, ok := item.(string); ok {
							srv.Command = append(srv.Command, registry.Value{Literal: s})
						}
					}
				}
			}
			if srv.Transport != "" {
				data.MCPServers[name] = srv
			}
		}
	}

	// Agents / Default Agent
	defaultAgent, _ := raw["default_agent"].(string)
	if agentsMap, ok := raw["agent"].(map[string]any); ok {
		for name, val := range agentsMap {
			aObj, ok := val.(map[string]any)
			if !ok {
				continue
			}
			desc, _ := aObj["description"].(string)
			promptStr, _ := aObj["prompt"].(string)
			role := "delegate"
			if name == defaultAgent || aObj["mode"] == "primary" {
				role = "primary"
			}

			step := registry.Agent{
				Name:        name,
				Description: desc,
				Role:        role,
				Class:       "default",
				Prompt:      registry.Prompt{Text: promptStr},
			}
			if stepsFloat, ok := aObj["steps"].(float64); ok {
				st := int(stepsFloat)
				step.Steps = &st
			}
			data.WorkflowSteps = append(data.WorkflowSteps, step)
		}
	}

	return nil
}

// importOMP parses ~/.omp/config.yml and ~/.omp/agent/agents/*.md
func importOMP(home string, data *ImportedData) error {
	configPath := filepath.Join(home, ".omp", "config.yml")
	bytes, err := os.ReadFile(configPath)
	if err == nil {
		var raw map[string]any
		if err := yaml.Unmarshal(bytes, &raw); err == nil {
			if mr, ok := raw["modelRoles"].(map[string]any); ok {
				if def, ok := mr["default"].(string); ok && def != "" {
					if _, exists := data.ModelClasses["default"]; !exists {
						data.ModelClasses["default"] = def
					}
				}
				if sm, ok := mr["smol"].(string); ok && sm != "" {
					if _, exists := data.ModelClasses["smol"]; !exists {
						data.ModelClasses["smol"] = sm
					}
				}
			}
		}
	}

	// Subagents in ~/.omp/agent/agents/*.md
	agentsDir := filepath.Join(home, ".omp", "agent", "agents")
	files, err := os.ReadDir(agentsDir)
	if err == nil {
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			agentName := strings.TrimSuffix(f.Name(), ".md")
			content, err := os.ReadFile(filepath.Join(agentsDir, f.Name()))
			if err != nil {
				continue
			}
			data.WorkflowSteps = append(data.WorkflowSteps, registry.Agent{
				Name:   agentName,
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: string(content)},
			})
		}
	}

	return nil
}

// importCodex parses ~/.codex/config.toml and ~/.codex/agents/*.toml
func importCodex(home string, data *ImportedData) error {
	configPath := filepath.Join(home, ".codex", "config.toml")
	bytes, err := os.ReadFile(configPath)
	if err == nil {
		var raw map[string]any
		if err := toml.Unmarshal(bytes, &raw); err == nil {
			if m, ok := raw["model"].(string); ok && m != "" {
				if _, exists := data.ModelClasses["default"]; !exists {
					data.ModelClasses["default"] = m
				}
			}
			if mcpMap, ok := raw["mcp_servers"].(map[string]any); ok {
				for name, val := range mcpMap {
					sObj, ok := val.(map[string]any)
					if !ok {
						continue
					}
					srv := registry.MCPServer{Name: name}
					if url, ok := sObj["url"].(string); ok {
						srv.Transport = "remote"
						srv.URL = registry.Value{Literal: url}
					} else if cmdStr, ok := sObj["command"].(string); ok {
						srv.Transport = "local"
						srv.Command = append(srv.Command, registry.Value{Literal: cmdStr})
						if argsSlice, ok := sObj["args"].([]any); ok {
							for _, arg := range argsSlice {
								if as, ok := arg.(string); ok {
									srv.Command = append(srv.Command, registry.Value{Literal: as})
								}
							}
						}
					}
					if srv.Transport != "" {
						data.MCPServers[name] = srv
					}
				}
			}
		}
	}

	// Agents in ~/.codex/agents/*.toml
	agentsDir := filepath.Join(home, ".codex", "agents")
	files, err := os.ReadDir(agentsDir)
	if err == nil {
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".toml") {
				continue
			}
			var aRaw map[string]any
			content, err := os.ReadFile(filepath.Join(agentsDir, f.Name()))
			if err != nil {
				continue
			}
			if err := toml.Unmarshal(content, &aRaw); err == nil {
				name, _ := aRaw["name"].(string)
				if name == "" {
					name = strings.TrimSuffix(f.Name(), ".toml")
				}
				desc, _ := aRaw["description"].(string)
				instructions, _ := aRaw["developer_instructions"].(string)

				data.WorkflowSteps = append(data.WorkflowSteps, registry.Agent{
					Name:        name,
					Description: desc,
					Role:        "delegate",
					Class:       "default",
					Prompt:      registry.Prompt{Text: instructions},
				})
			}
		}
	}

	return nil
}

// importClaude parses ~/.claude/settings.json, ~/.claude.json, and ~/.claude/agents/*.md
func importClaude(home string, data *ImportedData) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	bytes, err := os.ReadFile(settingsPath)
	if err == nil {
		var raw map[string]any
		if err := json.Unmarshal(bytes, &raw); err == nil {
			if m, ok := raw["model"].(string); ok && m != "" {
				if _, exists := data.ModelClasses["default"]; !exists {
					data.ModelClasses["default"] = m
				}
			}
		}
	}

	claudeJSONPath := filepath.Join(home, ".claude.json")
	cBytes, err := os.ReadFile(claudeJSONPath)
	if err == nil {
		var raw map[string]any
		if err := json.Unmarshal(cBytes, &raw); err == nil {
			if mcpMap, ok := raw["mcpServers"].(map[string]any); ok {
				for name, val := range mcpMap {
					sObj, ok := val.(map[string]any)
					if !ok {
						continue
					}
					srv := registry.MCPServer{Name: name}
					t, _ := sObj["type"].(string)
					if t == "http" || t == "remote" {
						srv.Transport = "remote"
						if url, ok := sObj["url"].(string); ok {
							srv.URL = registry.Value{Literal: url}
						}
					} else if t == "stdio" || t == "local" {
						srv.Transport = "local"
						if cmdStr, ok := sObj["command"].(string); ok {
							srv.Command = append(srv.Command, registry.Value{Literal: cmdStr})
						}
						if argsSlice, ok := sObj["args"].([]any); ok {
							for _, arg := range argsSlice {
								if as, ok := arg.(string); ok {
									srv.Command = append(srv.Command, registry.Value{Literal: as})
								}
							}
						}
					}
					if srv.Transport != "" {
						data.MCPServers[name] = srv
					}
				}
			}
		}
	}

	return nil
}

// SynthesizeRegistry generates agentcfg.yaml, models.yaml, bash.yaml, workflow.yaml, mcp.yaml from ImportedData.
func SynthesizeRegistry(data *ImportedData) (*Result, error) {
	// Fallback model classes if none were imported
	if data.ModelClasses["default"] == "" {
		data.ModelClasses["default"] = "anthropic/claude-sonnet-4-5"
	}
	if data.ModelClasses["smol"] == "" {
		data.ModelClasses["smol"] = "anthropic/claude-haiku-4-5"
	}

	// Fallback workflow step if none were imported
	if len(data.WorkflowSteps) == 0 {
		data.WorkflowSteps = append(data.WorkflowSteps, registry.Agent{
			Name:   "lead",
			Role:   "primary",
			Class:  "default",
			Prompt: registry.Prompt{Text: "You are a helpful assistant."},
		})
	}

	// Deduplicate workflow steps by Name
	seenStepNames := make(map[string]bool)
	var uniqueSteps []registry.Agent
	hasPrimary := false

	for _, step := range data.WorkflowSteps {
		if seenStepNames[step.Name] {
			continue
		}
		seenStepNames[step.Name] = true
		if step.Role == "primary" {
			if hasPrimary {
				step.Role = "delegate"
			} else {
				hasPrimary = true
			}
		}
		uniqueSteps = append(uniqueSteps, step)
	}
	if !hasPrimary && len(uniqueSteps) > 0 {
		uniqueSteps[0].Role = "primary"
	}

	files := make(map[string]string)

	// agentcfg.yaml
	imports := []string{"models.yaml", "bash.yaml", "workflow.yaml"}
	if len(data.MCPServers) > 0 {
		imports = append(imports, "mcp.yaml")
	}

	agentcfgYAML := fmt.Sprintf(`version: 1
imports:
%s
harnesses:
  opencode:
    out: ~/.config/opencode/opencode.json
  omp:
    agents_dir: ~/.omp/agent/agents
`, formatYAMLList(imports, "  - "))
	files["agentcfg.yaml"] = agentcfgYAML

	// models.yaml
	var mcLines []string
	keys := make([]string, 0, len(data.ModelClasses))
	for k := range data.ModelClasses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		mcLines = append(mcLines, fmt.Sprintf("  %s: %s", k, data.ModelClasses[k]))
	}
	files["models.yaml"] = fmt.Sprintf("model_classes:\n%s\n", strings.Join(mcLines, "\n"))

	// bash.yaml
	files["bash.yaml"] = `bash:
  profiles:
    global:
      base: allow
`

	// workflow.yaml
	var stepBlocks []string
	for _, step := range uniqueSteps {
		sb := fmt.Sprintf("    - name: %s\n      role: %s\n      class: %s", step.Name, step.Role, step.Class)
		if step.Description != "" {
			sb += fmt.Sprintf("\n      description: %s", strconvQuoteIfNeeded(step.Description))
		}
		promptText := strings.TrimSpace(step.Prompt.Text)
		if strings.Contains(promptText, "\n") {
			indentedPrompt := "        " + strings.ReplaceAll(promptText, "\n", "\n        ")
			sb += fmt.Sprintf("\n      prompt:\n        text: |\n%s", indentedPrompt)
		} else if promptText != "" {
			sb += fmt.Sprintf("\n      prompt:\n        text: %s", strconvQuoteIfNeeded(promptText))
		} else {
			sb += "\n      prompt:\n        text: \"You are a helpful assistant.\""
		}
		if step.Steps != nil {
			sb += fmt.Sprintf("\n      steps: %d", *step.Steps)
		}
		stepBlocks = append(stepBlocks, sb)
	}
	files["workflow.yaml"] = fmt.Sprintf("workflow:\n  steps:\n%s\n", strings.Join(stepBlocks, "\n"))

	// mcp.yaml
	if len(data.MCPServers) > 0 {
		var srvBlocks []string
		mcpKeys := make([]string, 0, len(data.MCPServers))
		for k := range data.MCPServers {
			mcpKeys = append(mcpKeys, k)
		}
		sort.Strings(mcpKeys)
		for _, k := range mcpKeys {
			srv := data.MCPServers[k]
			sb := fmt.Sprintf("  - name: %s\n    transport: %s", srv.Name, srv.Transport)
			if srv.Transport == "remote" && srv.URL.Literal != "" {
				sb += fmt.Sprintf("\n    url: %s", strconvQuoteIfNeeded(srv.URL.Literal))
			} else if srv.Transport == "local" && len(srv.Command) > 0 {
				var cmdParts []string
				for _, c := range srv.Command {
					cmdParts = append(cmdParts, strconvQuoteIfNeeded(c.Literal))
				}
				sb += fmt.Sprintf("\n    command: [%s]", strings.Join(cmdParts, ", "))
			}
			srvBlocks = append(srvBlocks, sb)
		}
		files["mcp.yaml"] = fmt.Sprintf("mcp_servers:\n%s\n", strings.Join(srvBlocks, "\n"))
	}

	return &Result{Files: files}, nil
}

func formatYAMLList(list []string, prefix string) string {
	var lines []string
	for _, item := range list {
		lines = append(lines, prefix+item)
	}
	return strings.Join(lines, "\n")
}

func strconvQuoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t\n:\"'#[]{}") || s == "" {
		bytes, _ := json.Marshal(s)
		return string(bytes)
	}
	return s
}
