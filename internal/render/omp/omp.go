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

func New() render.Renderer { return renderer{} }

type renderer struct{}

func (renderer) ID() string { return id }

func (renderer) Capabilities() []render.Capability {
	return []render.Capability{
		render.CapAgentDefinitions,
		render.CapPromptAppend,
		render.CapPromptFileRef,
		render.CapModelClassBinding,
		render.CapBashOrderedList,
		render.CapGlobalBashPolicy,
		render.CapMCPLocalTransport,
		render.CapMCPToolAllowlist,
		render.CapMCPPerToolAsk,
		render.CapProjectModelPolicy,
	}
}

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

	approvalCmd, approvalGaps := renderToolsApprovalCommand(reg)
	plan.Gaps = append(plan.Gaps, approvalGaps...)
	plan.Outputs = append(plan.Outputs,
		render.RunCommand{Argv: []string{"omp", "config", "set", "tools.approvalMode", "always-ask"}, Why: "sync tool approval mode"},
		approvalCmd,
	)

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
	serverByName := make(map[string]registry.MCPServer, len(reg.MCPServers))
	for _, s := range reg.MCPServers {
		serverByName[s.Name] = s
	}

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
			Content: []byte(renderAgentFile(a, body, serverByName)),
		})
	}
	return files, nil
}

// renderAgentFile builds one subagent markdown file: YAML frontmatter
// (name, description, tools, optional spawns/model) followed by "---" and
// the raw prompt body. MCP grants come from the agent's own mcp: list:
// each granted server's tools (minus that entry's ask list — an
// ask-listed tool is excluded from this subagent's visibility entirely,
// not merely gated, because omp cannot prompt a headless subagent; see
// renderToolsApprovalCommand for why granting it visibility without a
// matching tools.approval entry would silently auto-allow it instead of
// asking, via the subagent's own forced-yolo fallback).
func renderAgentFile(a registry.Agent, body string, serverByName map[string]registry.MCPServer) string {
	tools := append([]string{}, baseTools...)
	if a.Permissions.Write == "allow" {
		tools = append(tools, "write")
	}
	if a.Permissions.Edit == "allow" {
		tools = append(tools, "edit", "ast_edit")
	}
	for _, m := range a.MCP {
		server, ok := serverByName[m.Server]
		if !ok || !targets(server.Targets) {
			continue
		}
		asked := make(map[string]bool, len(m.Ask))
		for _, t := range m.Ask {
			asked[t] = true
		}
		for _, tool := range server.Tools {
			if asked[tool] {
				continue
			}
			tools = append(tools, mcpToolID(m.Server, tool))
		}
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

func translateDecision(d bashpolicy.Decision) string {
	if d == bashpolicy.Ask {
		return "prompt"
	}
	return string(d)
}

// renderToolsApprovalCommand builds omp's tools.approval allow-list: the
// only lever available to implement ask-by-default MCP approval, since
// omp's tools.approvalMode is a single harness-wide setting (paired with
// this via Render's "always-ask" command) and tools.approval itself has
// no glob support (exact tool names only) and no per-agent scoping (one
// map, shared by the interactive primary session and every subagent) —
// both confirmed empirically against a live omp session, not documented
// upstream.
//
// Because there is no per-agent scoping, an agent's mcp[].ask list can't
// stay agent-local the way it does for opencode's per-agent permission
// block: every agent's ask lists for a given server are unioned into one
// harness-wide ask set for that server, and a GapReduction is recorded
// wherever that aggregation actually discards per-agent granularity.
// Every tool in a targeted server's Tools list NOT in its ask set is
// allow-listed; everything else (ask-listed, or simply outside every
// server's Tools list) is left unset and falls through to always-ask's
// tier default — MCP tools all declare tier "write", so an unset one
// already prompts with zero extra bookkeeping.
//
// task/write/edit/ast_edit are allow-listed too, conditionally: only when
// some targeted agent's permissions actually grant them (task allow
// means dispatching a subagent shouldn't itself need approval; write/
// edit mirror whichever agent's frontmatter already grants them, e.g.
// build — tools.approval has no per-agent split, so this also makes the
// primary agent's own edit/write frictionless, a real trade-off with no
// workaround given the single global map).
func renderToolsApprovalCommand(reg *registry.Registry) (render.RunCommand, []render.Gap) {
	askSet := map[string]map[string]bool{}
	for _, a := range reg.Agents {
		for _, m := range a.MCP {
			if len(m.Ask) == 0 {
				continue
			}
			if askSet[m.Server] == nil {
				askSet[m.Server] = map[string]bool{}
			}
			for _, tool := range m.Ask {
				askSet[m.Server][tool] = true
			}
		}
	}

	approval := map[string]string{}
	var gaps []render.Gap
	for _, s := range reg.MCPServers {
		if !targets(s.Targets) || len(s.Tools) == 0 {
			continue
		}
		asked := askSet[s.Name]
		if len(asked) > 0 {
			gaps = append(gaps, render.Gap{
				Kind:       render.GapReduction,
				Capability: render.CapMCPPerToolAsk,
				Subject:    "mcp:" + s.Name,
				Detail: fmt.Sprintf(
					"mcp server %q's per-tool ask list is enforced harness-wide, not per agent — omp's tools.approval has no per-agent scoping, so every agent granted this server shares the same ask set.",
					s.Name,
				),
			})
		}
		for _, tool := range s.Tools {
			if asked[tool] {
				continue
			}
			approval[mcpToolID(s.Name, tool)] = "allow"
		}
	}

	for _, name := range builtinAlwaysAllow {
		approval[name] = "allow"
	}
	if agentGrants(reg, func(p registry.Permissions) bool { return p.Task == "allow" }) {
		approval["task"] = "allow"
	}
	if agentGrants(reg, func(p registry.Permissions) bool { return p.Write == "allow" }) {
		approval["write"] = "allow"
	}
	if agentGrants(reg, func(p registry.Permissions) bool { return p.Edit == "allow" }) {
		approval["edit"] = "allow"
		approval["ast_edit"] = "allow"
	}

	approvalJSON, err := json.Marshal(approval)
	if err != nil {
		// map[string]string always marshals; unreachable in practice.
		approvalJSON = []byte("{}")
	}

	return render.RunCommand{
		Argv: []string{"omp", "config", "set", "tools.approval", string(approvalJSON)},
		Why:  "sync MCP + built-in tool approval allow-list",
	}, gaps
}

// agentGrants reports whether any agent targeting omp satisfies pred
// against its own Permissions.
func agentGrants(reg *registry.Registry, pred func(registry.Permissions) bool) bool {
	for _, a := range reg.Agents {
		if targets(a.Targets) && pred(a.Permissions) {
			return true
		}
	}
	return false
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
			return nil, false, resolveFailureGap(s.Name, "url", err)
		}
		entry["url"] = url
	case "local":
		cmd := make([]any, 0, len(s.Command))
		for _, part := range s.Command {
			resolved, err := part.Resolve()
			if err != nil {
				return nil, false, resolveFailureGap(s.Name, "command", err)
			}
			cmd = append(cmd, resolved)
		}
		entry["command"] = cmd
	default:
		return nil, false, resolveFailureGap(s.Name, "transport", fmt.Errorf("unknown transport %q", s.Transport))
	}
	return entry, true, nil
}

func resolveFailureGap(server, field string, err error) *render.Gap {
	return &render.Gap{
		Kind:       render.GapSkip,
		Capability: render.CapMCPLocalTransport,
		Subject:    "mcp:" + server,
		Detail: fmt.Sprintf(
			"mcp server %q %s could not be resolved (%s); it was omitted from this harness's config.",
			server, field, err,
		),
	}
}
