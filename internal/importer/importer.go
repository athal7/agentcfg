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

type Options struct {
	HomeDir string
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
		if err := yaml.Unmarshal(bytes, &raw); err != nil {
			return fmt.Errorf("parsing config.yml: %w", err)
		}
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
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading config.yml: %w", err)
	}

	// Subagents in ~/.omp/agent/agents/*.md
	agentsDir := filepath.Join(home, ".omp", "agent", "agents")
	files, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading agents directory: %w", err)
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		agentName := strings.TrimSuffix(f.Name(), ".md")
		content, err := os.ReadFile(filepath.Join(agentsDir, f.Name()))
		if err != nil {
			return fmt.Errorf("reading agent %s: %w", f.Name(), err)
		}
		data.WorkflowSteps = append(data.WorkflowSteps, registry.Agent{
			Name:   agentName,
			Role:   "delegate",
			Class:  "default",
			Prompt: registry.Prompt{Text: string(content)},
		})
	}

	return nil
}

// importCodex parses ~/.codex/config.toml and ~/.codex/agents/*.toml
func importCodex(home string, data *ImportedData) error {
	configPath := filepath.Join(home, ".codex", "config.toml")
	bytes, err := os.ReadFile(configPath)
	if err == nil {
		var raw map[string]any
		if err := toml.Unmarshal(bytes, &raw); err != nil {
			return fmt.Errorf("parsing config.toml: %w", err)
		}
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
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading config.toml: %w", err)
	}

	// Agents in ~/.codex/agents/*.toml
	agentsDir := filepath.Join(home, ".codex", "agents")
	files, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading agents directory: %w", err)
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".toml") {
			continue
		}
		var aRaw map[string]any
		content, err := os.ReadFile(filepath.Join(agentsDir, f.Name()))
		if err != nil {
			return fmt.Errorf("reading agent %s: %w", f.Name(), err)
		}
		if err := toml.Unmarshal(content, &aRaw); err != nil {
			return fmt.Errorf("parsing agent %s: %w", f.Name(), err)
		}
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

	return nil
}

// importClaude parses ~/.claude/settings.json, ~/.claude.json, and ~/.claude/agents/*.md
func importClaude(home string, data *ImportedData) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	bytes, err := os.ReadFile(settingsPath)
	if err == nil {
		var raw map[string]any
		if err := json.Unmarshal(bytes, &raw); err != nil {
			return fmt.Errorf("parsing settings.json: %w", err)
		}
		if m, ok := raw["model"].(string); ok && m != "" {
			if _, exists := data.ModelClasses["default"]; !exists {
				data.ModelClasses["default"] = m
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading settings.json: %w", err)
	}

	claudeJSONPath := filepath.Join(home, ".claude.json")
	cBytes, err := os.ReadFile(claudeJSONPath)
	if err == nil {
		var raw map[string]any
		if err := json.Unmarshal(cBytes, &raw); err != nil {
			return fmt.Errorf("parsing .claude.json: %w", err)
		}
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
					if headers, ok := sObj["headers"].(map[string]any); ok {
						srv.Headers = make(map[string]registry.Value, len(headers))
						for headerName, value := range headers {
							if literal, ok := value.(string); ok {
								srv.Headers[headerName] = registry.Value{Literal: literal}
							}
						}
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
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading .claude.json: %w", err)
	}

	agentsDir := filepath.Join(home, ".claude", "agents")
	files, err := os.ReadDir(agentsDir)
	if err == nil {
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(agentsDir, file.Name()))
			if err != nil {
				return err
			}
			agent, err := importClaudeAgent(file.Name(), string(content))
			if err != nil {
				return fmt.Errorf("parsing Claude agent %s: %w", file.Name(), err)
			}
			data.WorkflowSteps = append(data.WorkflowSteps, agent)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}

func SynthesizeRegistry(data *ImportedData) (*Result, error) {
	if data.ModelClasses["default"] == "" {
		data.ModelClasses["default"] = "anthropic/claude-sonnet-4-5"
	}
	if data.ModelClasses["smol"] == "" {
		data.ModelClasses["smol"] = "anthropic/claude-haiku-4-5"
	}

	seenStepNames := make(map[string]bool)
	uniqueSteps := make([]registry.Agent, 0, len(data.WorkflowSteps))
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

	imports := []string{"models.yaml", "bash.yaml", "workflow.yaml"}
	if len(data.MCPServers) > 0 {
		imports = append(imports, "mcp.yaml")
	}
	files := make(map[string]string, len(imports)+1)

	agentcfg := struct {
		Version   int                               `yaml:"version"`
		Imports   []string                          `yaml:"imports"`
		Harnesses map[string]registry.HarnessConfig `yaml:"harnesses"`
	}{
		Version: 1,
		Imports: imports,
		Harnesses: map[string]registry.HarnessConfig{
			"opencode": {Out: "~/.config/opencode/opencode.json"},
			"omp":      {AgentsDir: "~/.omp/agent/agents"},
		},
	}
	models := struct {
		ModelClasses map[string]string `yaml:"model_classes"`
	}{ModelClasses: data.ModelClasses}
	bash := struct {
		Bash registry.BashPolicy `yaml:"bash"`
	}{Bash: registry.BashPolicy{Profiles: map[string]registry.BashProfile{
		"global": {Base: registry.Allow},
	}}}
	workflow := struct {
		Workflow registry.Workflow `yaml:"workflow"`
	}{Workflow: registry.Workflow{Steps: uniqueSteps}}

	var err error
	if files["agentcfg.yaml"], err = marshalYAML(agentcfg); err != nil {
		return nil, err
	}
	if files["models.yaml"], err = marshalYAML(models); err != nil {
		return nil, err
	}
	if files["bash.yaml"], err = marshalYAML(bash); err != nil {
		return nil, err
	}
	if files["workflow.yaml"], err = marshalYAML(workflow); err != nil {
		return nil, err
	}
	if len(data.MCPServers) > 0 {
		content, err := marshalMCPServers(data.MCPServers)
		if err != nil {
			return nil, err
		}
		files["mcp.yaml"] = content
	}

	return &Result{Files: files}, nil
}

func importClaudeAgent(filename, content string) (registry.Agent, error) {
	agent := registry.Agent{
		Name:   strings.TrimSuffix(filename, ".md"),
		Role:   "delegate",
		Class:  "default",
		Prompt: registry.Prompt{Text: content},
	}
	if !strings.HasPrefix(content, "---\n") {
		return agent, nil
	}

	frontmatterEnd := strings.Index(content[4:], "\n---\n")
	if frontmatterEnd < 0 {
		return agent, nil
	}
	frontmatterEnd += 4

	var frontmatter map[string]any
	if err := yaml.Unmarshal([]byte(content[4:frontmatterEnd]), &frontmatter); err != nil {
		return registry.Agent{}, err
	}
	agent.Prompt.Text = content[frontmatterEnd+5:]
	if name, ok := frontmatter["name"].(string); ok && name != "" {
		agent.Name = name
	}
	if description, ok := frontmatter["description"].(string); ok {
		agent.Description = description
	}
	switch maxTurns := frontmatter["maxTurns"].(type) {
	case int:
		agent.Steps = &maxTurns
	case uint64:
		steps := int(maxTurns)
		agent.Steps = &steps
	}
	agent.Extra = map[string]map[string]any{"claude": frontmatter}
	delete(agent.Extra["claude"], "name")
	delete(agent.Extra["claude"], "description")
	delete(agent.Extra["claude"], "maxTurns")
	if len(agent.Extra["claude"]) == 0 {
		agent.Extra = nil
	}
	return agent, nil
}

func marshalYAML(value any) (string, error) {
	content, err := yaml.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func marshalMCPServers(servers map[string]registry.MCPServer) (string, error) {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("mcp_servers:\n")
	for _, name := range names {
		server := servers[name]
		serverName, err := json.Marshal(server.Name)
		if err != nil {
			return "", err
		}
		transport, err := json.Marshal(server.Transport)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "  - name: %s\n    transport: %s\n", serverName, transport)
		if server.URL.Literal != "" {
			url, err := json.Marshal(server.URL.Literal)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "    url: %s\n", url)
		}
		if len(server.Command) > 0 {
			b.WriteString("    command:\n")
			for _, value := range server.Command {
				command, err := json.Marshal(value.Literal)
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&b, "      - %s\n", command)
			}
		}
		if len(server.Headers) > 0 {
			headerNames := make([]string, 0, len(server.Headers))
			for headerName := range server.Headers {
				headerNames = append(headerNames, headerName)
			}
			sort.Strings(headerNames)
			b.WriteString("    headers:\n")
			for _, headerName := range headerNames {
				headerNameYAML, err := json.Marshal(headerName)
				if err != nil {
					return "", err
				}
				value, err := json.Marshal(server.Headers[headerName].Literal)
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&b, "      %s: %s\n", headerNameYAML, value)
			}
		}
	}
	return b.String(), nil
}
