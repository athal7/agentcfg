// Package omp renders an agentcfg registry into omp's native
// ~/.omp/agent config: per-subagent markdown files, a whole-session system
// prompt append for the primary agent, a global bash policy sync command,
// and an mcp server config file.
package omp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/athal7/agentcfg/internal/bashpolicy"
	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

// id is the renderer identifier used by ID() and capability checks.
const id = "omp"

// Paths are left unexpanded (literal "~"): tilde-expansion is the apply
// layer's job, not Render's — Render must stay pure.
const (
	agentsDir         = "~/.omp/agent/agents"
	appendSystemPath  = "~/.omp/agent/APPEND_SYSTEM.md"
	mcpConfigPath     = "~/.omp/agent/mcp.json"
	globalBashProfile = "global"
	targetName        = "omp"
)

// New returns a Renderer that produces omp's native ~/.omp/agent config.
func New() render.Renderer { return renderer{} }

// renderer implements render.Renderer for the omp harness.
type renderer struct{}

// ID returns the renderer's identifier, "omp".
func (renderer) ID() string { return id }

// Capabilities returns the set of registry features this renderer can
// express in omp's native config.
func (renderer) Capabilities() []render.Capability {
	return []render.Capability{
		render.CapAgentDefinitions,
		render.CapPromptAppend,
		render.CapPromptFileRef,
		render.CapModelClassBinding,
		render.CapBashOrderedList,
		render.CapGlobalBashPolicy,
		render.CapMCPLocalTransport,
		render.CapMCPRemoteTransport,
		render.CapProjectModelPolicy,
	}
}

// Render produces a Plan that writes omp's native ~/.omp/agent config: per-agent
// markdown files, a system prompt append, a global bash policy sync command,
// and an MCP server config file.
func (r renderer) Render(reg *registry.Registry, opt render.Options) (*render.Plan, error) {
	plan := &render.Plan{}
	plan.Gaps = append(plan.Gaps, render.DetectGaps(reg, r.Capabilities())...)

	readFile := opt.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	agentFiles, err := renderAgentFiles(reg, readFile)
	if err != nil {
		return nil, err
	}
	plan.Outputs = append(plan.Outputs, render.RebuildDir{
		Dir:   agentsDir,
		Glob:  "*.md",
		Files: agentFiles,
	})

	if primary := render.PrimaryAgent(reg); primary != nil {
		body, err := promptBody(*primary, readFile)
		if err != nil {
			return nil, fmt.Errorf("omp: primary agent %q: %w", primary.Name, err)
		}
		plan.Outputs = append(plan.Outputs, render.WriteFile{
			Path:    appendSystemPath,
			Mode:    0600,
			Content: []byte(body),
		})
	}

	bashCmd, err := renderBashPatternsCommand(reg)
	if err != nil {
		return nil, err
	}
	plan.Outputs = append(plan.Outputs, bashCmd)

	mcpServers := map[string]any{}
	for _, s := range reg.MCPServers {
		if !targets(s.Targets) {
			continue
		}
		entry, ok, gap := renderMCPServer(s)
		if gap != nil {
			plan.Gaps = append(plan.Gaps, *gap)
		}
		if ok {
			mcpServers[s.Name] = entry
		}
	}
	plan.Outputs = append(plan.Outputs, render.MergeJSON{
		Path:    mcpConfigPath,
		Mode:    0600,
		Managed: []string{"mcpServers"},
		Object: map[string]any{
			"mcpServers": mcpServers,
		},
	})

	return plan, nil
}

// projectConfigDir, projectConfigFile are literal, non-"~" path segments
// relative to the resolved project directory — unlike agentsDir/
// appendSystemPath/mcpConfigPath (all user-scope, home-relative), this
// file lives inside the project itself.
const projectConfigDir, projectConfigFile = ".omp", "config.yml"

// RenderProject implements render.ProjectScopeRenderer: a directory-local
// config.yml naming the resolved class map under modelRoles. Unlike
// opencode's RenderProject, omp does NOT get literal model IDs here — omp
// resolves "@<class>" references against modelRoles at its own runtime
// (the same "@<class>" syntax renderAgentFile already emits in agent
// frontmatter for the user-scope config), so the class map itself is the
// output, not each class's resolved literal. This is the asymmetry the
// design calls out explicitly between the two harnesses.
func (r renderer) RenderProject(classes map[string]string, _ *registry.Registry, dir string) (*render.Plan, error) {
	return &render.Plan{
		Outputs: []render.Output{
			render.MergeYAML{
				Path:    filepath.Join(dir, projectConfigDir, projectConfigFile),
				Mode:    0600,
				Managed: []string{"modelRoles"},
				Object:  map[string]any{"modelRoles": classes},
			},
		},
	}, nil
}

// renderAgentFiles builds one WriteFile per non-primary agent targeting
// omp. Paths are relative to agentsDir, per RebuildDir's documented
// convention.
func renderAgentFiles(reg *registry.Registry, readFile func(string) ([]byte, error)) ([]render.WriteFile, error) {
	var files []render.WriteFile
	for _, a := range reg.Agents {
		if a.Mode == "primary" || !targets(a.Targets) {
			continue
		}
		body, err := promptBody(a, readFile)
		if err != nil {
			return nil, fmt.Errorf("omp: agent %q: %w", a.Name, err)
		}
		files = append(files, render.WriteFile{
			Path:    a.Name + ".md",
			Mode:    0600,
			Content: []byte(renderAgentFile(a, body)),
		})
	}
	return files, nil
}

// renderAgentFile builds one subagent markdown file: YAML frontmatter
// (name, description, tools, optional spawns/model) followed by "---" and
// the raw prompt body.
func renderAgentFile(a registry.Agent, body string) string {
	tools := append([]string{}, baseTools...)
	if a.Permissions.Write == "allow" {
		tools = append(tools, "write")
	}
	if a.Permissions.Edit == "allow" {
		tools = append(tools, "edit", "ast_edit")
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", a.Name)
	fmt.Fprintf(&b, "description: %s\n", a.Description)
	fmt.Fprintf(&b, "tools: %s\n", strings.Join(tools, ","))
	if a.Permissions.Task == "allow" {
		b.WriteString("spawns: \"*\"\n")
	}
	if a.Class != "" {
		// Literal class name, not the resolved model — omp binds classes
		// by name (CapModelClassBinding), it doesn't take literal model IDs.
		fmt.Fprintf(&b, "model: \"@%s\"\n", a.Class)
	}
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

// promptBody reads a file-backed prompt's content, or returns an inline
// prompt's text as-is.
func promptBody(a registry.Agent, readFile func(string) ([]byte, error)) (string, error) {
	if a.ResolvedPromptFile != "" {
		data, err := readFile(a.ResolvedPromptFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return a.Prompt.Text, nil
}

// targets reports whether an omitted/empty targets list (meaning "every
// harness") or an explicit list naming omp applies to this renderer.
func targets(list []string) bool {
	if len(list) == 0 {
		return true
	}
	for _, t := range list {
		if t == targetName {
			return true
		}
	}
	return false
}

// renderBashPatternsCommand syncs the compiled global bash policy, ordered
// most-specific-first, via omp's CLI. Each rule becomes a {pattern,
// decision} object; "ask" translates to omp's "prompt" decision name.
func renderBashPatternsCommand(reg *registry.Registry) (render.RunCommand, error) {
	compiled, err := bashpolicy.Compile(reg.Bash, globalBashProfile)
	if err != nil {
		return render.RunCommand{}, fmt.Errorf("omp: compiling global bash policy: %w", err)
	}

	rules := bashpolicy.AsOrderedList(compiled)
	patterns := make([]map[string]string, len(rules))
	for i, rule := range rules {
		patterns[i] = map[string]string{
			"pattern":  rule.Pattern,
			"decision": translateDecision(rule.Decision),
		}
	}

	patternsJSON, err := json.Marshal(patterns)
	if err != nil {
		return render.RunCommand{}, fmt.Errorf("omp: marshaling bash patterns: %w", err)
	}

	return render.RunCommand{
		Argv: []string{"omp", "config", "set", "bash.patterns", string(patternsJSON)},
		Why:  "sync global bash policy",
	}, nil
}

// translateDecision maps a bashpolicy.Decision to omp's native decision
// name — "ask" becomes "prompt" in omp's vocabulary.
func translateDecision(d bashpolicy.Decision) string {
	if d == bashpolicy.Ask {
		return "prompt"
	}
	return string(d)
}

// renderMCPServer resolves one mcp_servers entry into omp's native mcp.json
// entry shape. A resolver failure skips the server rather than failing the
// whole Render; the caller records the returned Gap.
func renderMCPServer(s registry.MCPServer) (map[string]any, bool, *render.Gap) {
	entry := map[string]any{"lifecycle": "lazy"}
	switch s.Transport {
	case "remote":
		url, err := s.URL.Resolve()
		if err != nil {
			return nil, false, resolveFailureGap(s, "url", err)
		}
		entry["url"] = url
	case "local":
		cmd := make([]any, 0, len(s.Command))
		for _, part := range s.Command {
			resolved, err := part.Resolve()
			if err != nil {
				return nil, false, resolveFailureGap(s, "command", err)
			}
			cmd = append(cmd, resolved)
		}
		entry["command"] = cmd
	default:
		return nil, false, resolveFailureGap(s, "transport", fmt.Errorf("unknown transport %q", s.Transport))
	}
	return entry, true, nil
}

// resolveFailureGap builds a GapSkip for an MCP server whose URL, command,
// or transport could not be resolved at render time.
func resolveFailureGap(s registry.MCPServer, field string, err error) *render.Gap {
	cap := render.CapMCPLocalTransport
	if s.Transport == "remote" {
		cap = render.CapMCPRemoteTransport
	}
	return &render.Gap{
		Kind:       render.GapSkip,
		Capability: cap,
		Subject:    "mcp:" + s.Name,
		Detail: fmt.Sprintf(
			"mcp server %q %s could not be resolved (%s); it was omitted from this harness's config.",
			s.Name, field, err,
		),
	}
}
