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
	"sort"
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
//
// Deliberately absent: CapPrimaryAgentToolPermission. omp's primary
// session gets only a system-prompt append (CapPromptAppend) — there is no
// per-agent config file or permission block for it the way subagents get
// (renderAgentFile's tools: frontmatter honors permissions.edit/write for
// every non-primary agent already). A permissions.edit/write set on a
// mode:primary agent is therefore dropped; DetectGaps reports it via
// detectPrimaryAgentToolPermissionGap.
//
// This is NOT the "omp's --tools= CLI flag is broken" bug it looks like at
// first glance (github.com/athal7/agentcfg/issues/9's original hypothesis).
// Investigated empirically against omp v17.2.5 (source:
// github.com/can1357/oh-my-pi, packages/coding-agent/src/tools/index.ts's
// createTools + sdk.ts's toolRegistry construction) and confirmed live:
// `omp --tools=<list-without-write-or-edit>` correctly removes both from
// the registered/callable tool set — verified via `--mode=json` raw
// tool-call transcripts, not just the model's own (unreliable) self-report.
// `tools.approval`/`tools.approvalMode` is confirmed orthogonal too: it
// only gates the approval PROMPT for tools already in the active set, it
// can't resurrect an excluded one.
//
// The actual reasons this can't be closed from agentcfg's side:
//  1. --tools= is a CLI-invocation-time flag. agentcfg only renders
//     persistent config files (~/.omp/agent/*) and runs `omp config set`
//     for settings that have a persistent key; it does not control how
//     the user invokes the `omp` binary, and omp's settings schema has no
//     persistent write.enabled/edit.enabled key (unlike bash.enabled,
//     lsp.enabled, etc.) it could set instead.
//  2. Even granted a lever, excluding write/edit alone doesn't stop file
//     mutation if bash or eval remain enabled for the primary session —
//     confirmed empirically: with bash allowed and write/edit excluded, a
//     restricted omp session wrote a file via `bash`'s shell redirection
//     instead, satisfying the request without ever calling `write`. Fully
//     enforcing "must delegate file changes" requires denying every
//     code-execution tool on the primary session, not just write/edit.
func (renderer) Capabilities() []render.Capability {
	return []render.Capability{
		render.CapAgentDefinitions,
		render.CapComposeIntoPrimary,
		render.CapPromptAppend,
		render.CapPromptFileRef,
		render.CapModelClassBinding,
		render.CapBashOrderedList,
		render.CapGlobalBashPolicy,
		render.CapMCPLocalTransport,
		render.CapMCPRemoteTransport,
		render.CapMCPToolGlobs,
		render.CapProjectModelPolicy,
		render.CapCustomCommands,
		render.CapStructuredWorkflowCommand,
	}
}

// Render produces a Plan that writes omp's native ~/.omp/agent config: per-agent
// markdown files, a system prompt append, a global bash policy sync command,
// an MCP server config file, and a `omp config set` command per harness
// setting the registry declares (tools.approval, derived from every
// configured MCP server's Tools allowlist plus harnesses.omp.extra, and
// any other harnesses.omp.extra entry verbatim). When the role: primary
// step has Agent.Opencode set, its own prompt is opencode-only (see
// renderAgentFiles/composedSections) and is never written to
// APPEND_SYSTEM.md; that file is written only if something is left to
// put in it (a non-overridden primary's own body, or a composed
// role: advisory section) — an Opencode-overridden primary with no
// advisory steps writes nothing at all.
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
		var body string
		if primary.Opencode == nil {
			var err error
			body, err = promptBody(*primary, readFile)
			if err != nil {
				return nil, fmt.Errorf("omp: primary agent %q: %w", primary.Name, err)
			}
		}
		composed, err := composedSections(reg, readFile)
		if err != nil {
			return nil, err
		}
		if body != "" || composed != "" {
			plan.Outputs = append(plan.Outputs, render.WriteFile{
				Path:    appendSystemPath,
				Mode:    0600,
				Content: []byte(body + composed),
			})
		}
	}

	bashCmd, err := renderBashPatternsCommand(reg)
	if err != nil {
		return nil, err
	}
	plan.Outputs = append(plan.Outputs, bashCmd)

	approvalCmd, ok, err := renderToolsApprovalCommand(reg)
	if err != nil {
		return nil, err
	}
	if ok {
		plan.Outputs = append(plan.Outputs, approvalCmd)
	}

	extraCmds, err := renderExtraSettingsCommands(reg)
	if err != nil {
		return nil, err
	}
	for _, c := range extraCmds {
		plan.Outputs = append(plan.Outputs, c)
	}

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

	commandsTree, err := render.RenderCommands(reg, readFile)
	if err != nil {
		return nil, fmt.Errorf("omp: rendering commands: %w", err)
	}
	plan.Outputs = append(plan.Outputs, commandsTree)

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

// renderAgentFiles builds one WriteFile per omp-targeting agent that isn't
// rendered some other way: role: primary is the session itself (no
// file); role: advisory is spliced into the primary's APPEND_SYSTEM.md
// instead (see composedSections) — but only when the registry actually
// has a role: primary agent to compose into. An advisory agent in a
// primary-less registry has nothing to splice into, so it falls back to
// a normal standalone file here rather than being silently dropped. A
// step naming an Opencode persona (Agent.Opencode != nil) is
// opencode-only by construction — omp has no standing named-agent-
// definition concept to render it as, so it's skipped here regardless of
// Targets/Role.
// Paths are relative to agentsDir, per RebuildDir's documented
// convention.
func renderAgentFiles(reg *registry.Registry, readFile func(string) ([]byte, error)) ([]render.WriteFile, error) {
	hasPrimary := render.PrimaryAgent(reg) != nil
	serversByName := make(map[string]registry.MCPServer, len(reg.MCPServers))
	for _, s := range reg.MCPServers {
		if !targets(s.Targets) {
			continue
		}
		serversByName[s.Name] = s
	}
	var files []render.WriteFile
	for _, a := range reg.Agents {
		if !targets(a.Targets) || a.Opencode != nil {
			continue
		}
		if a.Role == "primary" || (a.Role == "advisory" && hasPrimary) {
			continue
		}
		body, err := promptBody(a, readFile)
		if err != nil {
			return nil, fmt.Errorf("omp: agent %q: %w", a.Name, err)
		}
		files = append(files, render.WriteFile{
			Path:    a.Name + ".md",
			Mode:    0600,
			Content: []byte(renderAgentFile(a, serversByName, body)),
		})
	}
	return files, nil
}

// composedSections builds the Markdown appended after the primary agent's
// own prompt body in APPEND_SYSTEM.md: one "## <name>[: <description>]"
// section per role: advisory, omp-targeting agent, in registry
// declaration order. Returns "" (no-op) when no agent has role: advisory.
// A step with Agent.Opencode set is opencode-only (see renderAgentFiles)
// and is skipped here too, regardless of Role/Targets.
func composedSections(reg *registry.Registry, readFile func(string) ([]byte, error)) (string, error) {
	var b strings.Builder
	for _, a := range reg.Agents {
		if a.Role != "advisory" || !targets(a.Targets) || a.Opencode != nil {
			continue
		}
		body, err := promptBody(a, readFile)
		if err != nil {
			return "", fmt.Errorf("omp: composed agent %q: %w", a.Name, err)
		}
		b.WriteString("\n\n## ")
		b.WriteString(a.Name)
		if a.Description != "" {
			b.WriteString(": ")
			b.WriteString(a.Description)
		}
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	return b.String(), nil
}

// renderAgentFile builds one subagent markdown file: YAML frontmatter
// (name, description, tools, optional spawns/model) followed by "---" and
// the raw prompt body. tools additionally grants every MCP tool id
// serversByName resolves from the agent's mcp: entries (see
// mcpServerToolIDs) — omp's frontmatter tools: list is a hard visibility
// allowlist, not just an approval gate, so an agent granted an MCP server
// without this can see the server's presence but not call any of its
// tools.
func renderAgentFile(a registry.Agent, serversByName map[string]registry.MCPServer, body string) string {
	tools := append([]string{}, baseTools...)
	if a.Permissions.Write == "allow" {
		tools = append(tools, "write")
	}
	if a.Permissions.Edit == "allow" {
		tools = append(tools, "edit", "ast_edit")
	}
	for _, m := range a.MCP {
		if s, ok := serversByName[m.Server]; ok {
			tools = append(tools, mcpServerToolIDs(s)...)
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
// most-specific-first, via omp's CLI. Each rule becomes a {match, approval}
// object — the exact field names omp's BashTool.approval() reads via
// getBashApprovalPatternRules() (packages/coding-agent/src/tools/bash.ts;
// also documented in omp's own tools/bash.md, "Each rule has a `match` glob
// and an `approval` value of `allow`, `prompt`, or `deny`"). This renderer
// previously emitted {pattern, decision} — a plausible-looking but wrong
// pair of names that decoded to zero rules on the omp side (every item
// failed the `typeof record.match === "string"` guard and was silently
// dropped), so global bash policy was a complete no-op there: every
// command fell through to bare tier "exec" with no explicit policy, which
// let harnesses.omp.extra's tools.approval.bash: allow win by default —
// masked because that allow-list exists specifically to cover the *tool*
// grant, on the assumption bash.patterns independently gated dangerous
// commands beneath it. "ask" translates to omp's "prompt" decision name.
func renderBashPatternsCommand(reg *registry.Registry) (render.RunCommand, error) {
	compiled, err := bashpolicy.Compile(reg.Bash, globalBashProfile)
	if err != nil {
		return render.RunCommand{}, fmt.Errorf("omp: compiling global bash policy: %w", err)
	}

	rules := bashpolicy.AsOrderedList(compiled)
	patterns := make([]map[string]string, len(rules))
	for i, rule := range rules {
		patterns[i] = map[string]string{
			"match":    rule.Pattern,
			"approval": translateDecision(rule.Decision),
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

// renderToolsApprovalCommand syncs omp's per-tool approval allow-list: one
// "allow" entry per MCP tool id declared across every omp-targeting
// server's Tools list (the same ids renderAgentFile grants a subagent —
// every MCP tool a subagent can reach is already visibility-gated by its
// frontmatter, so there's nothing to gain by leaving it unlisted here),
// plus any static entries under harnesses.omp.extra["tools.approval"]
// (e.g. omp's own built-in write/edit/task/bash tools, which the registry
// declares rather than agentcfg opining on). Returns ok=false when
// there's nothing to set, so Render can skip emitting a no-op command.
func renderToolsApprovalCommand(reg *registry.Registry) (render.RunCommand, bool, error) {
	approval := map[string]any{}
	for _, s := range reg.MCPServers {
		if !targets(s.Targets) {
			continue
		}
		for _, toolID := range mcpServerToolIDs(s) {
			approval[toolID] = "allow"
		}
	}

	if raw, ok := reg.Harnesses[targetName].Extra["tools.approval"]; ok {
		extraApproval, ok := raw.(map[string]any)
		if !ok {
			return render.RunCommand{}, false, fmt.Errorf(`omp: harnesses.omp.extra["tools.approval"] must be an object mapping tool id to decision`)
		}
		for k, v := range extraApproval {
			approval[k] = v
		}
	}

	if len(approval) == 0 {
		return render.RunCommand{}, false, nil
	}

	approvalJSON, err := json.Marshal(approval)
	if err != nil {
		return render.RunCommand{}, false, fmt.Errorf("omp: marshaling tools.approval: %w", err)
	}
	return render.RunCommand{
		Argv: []string{"omp", "config", "set", "tools.approval", string(approvalJSON)},
		Why:  "sync mcp tool + built-in tool approval allow-list",
	}, true, nil
}

// renderExtraSettingsCommands emits one `omp config set <key> <value>` per
// harnesses.omp.extra entry, except "tools.approval" — that key is
// already handled by renderToolsApprovalCommand, which merges it with the
// registry-derived MCP tool grants rather than setting it verbatim.
// Sorted by key for a deterministic, reproducible Plan.
func renderExtraSettingsCommands(reg *registry.Registry) ([]render.RunCommand, error) {
	extra := reg.Harnesses[targetName].Extra
	if len(extra) == 0 {
		return nil, nil
	}

	keys := make([]string, 0, len(extra))
	for k := range extra {
		if k == "tools.approval" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cmds := make([]render.RunCommand, 0, len(keys))
	for _, k := range keys {
		valueArg, err := configSetArg(extra[k])
		if err != nil {
			return nil, fmt.Errorf("omp: harnesses.omp.extra[%q]: %w", k, err)
		}
		cmds = append(cmds, render.RunCommand{
			Argv: []string{"omp", "config", "set", k, valueArg},
			Why:  fmt.Sprintf("sync omp setting %q from harnesses.omp.extra", k),
		})
	}
	return cmds, nil
}

// configSetArg formats a harnesses.omp.extra value as `omp config set`
// expects it: a bare (unquoted) literal for a scalar string — confirmed
// empirically, omp's CLI parses a JSON-quoted string like `"always-ask"`
// as not matching its own unquoted enum values and rejects it — and JSON
// encoding for anything else (array, object, number, bool), which omp's
// CLI does expect JSON-encoded.
func configSetArg(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling value: %w", err)
	}
	return string(data), nil
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
