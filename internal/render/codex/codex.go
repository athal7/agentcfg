// Package codex renders an agentcfg registry into Codex CLI's native
// configuration: ~/.codex/config.toml (model, sandbox_mode, mcp_servers),
// ~/.codex/AGENTS.md (a whole-session instructions append), and standalone
// ~/.codex/agents/<name>.toml custom-agent files for independently
// dispatchable subagents. See docs at developers.openai.com/codex for the
// TOML schema this renderer targets (config-reference, subagents, mcp,
// agents-md, skills — fetched and verified against the current release
// while building this renderer).
package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

// id is the renderer identifier used by ID() and capability checks.
const id = "codex"

// targetName is the harness name agents/servers list under targets: to
// opt into or out of this renderer specifically.
const targetName = "codex"

// Paths are left unexpanded (literal "~"): tilde-expansion is the apply
// layer's job, not Render's — Render must stay pure.
const (
	configPath   = "~/.codex/config.toml"
	agentsMdPath = "~/.codex/AGENTS.md"
	agentsDir    = "~/.codex/agents"
)

// projectConfigDir/projectConfigFile/projectAgentsDir are literal,
// non-"~" path segments relative to the resolved project directory —
// unlike configPath/agentsMdPath/agentsDir (all user-scope, home-relative),
// these live inside the project itself.
const (
	projectConfigDir  = ".codex"
	projectConfigFile = "config.toml"
	projectAgentsDir  = ".codex/agents"
)

// New returns a Renderer that produces Codex CLI's native config.
func New() render.Renderer { return renderer{} }

// renderer implements render.Renderer for the codex harness.
type renderer struct{}

// ID returns the renderer's identifier, "codex".
func (renderer) ID() string { return id }

// Capabilities returns the set of registry features this renderer can
// express in Codex's native config.
//
// CapPromptAppend, not CapPrimaryAgent: Codex has no per-agent config
// object for its own interactive session — the primary agent's prompt is
// appended to ~/.codex/AGENTS.md, the same whole-session instructions
// file every Codex run loads, matching omp's APPEND_SYSTEM.md pattern.
//
// CapPrimaryAgentToolPermission IS declared, unlike omp (see omp.go's
// extensive comment declining it): Codex's config.toml has a real
// persistent sandbox_mode key (read-only/workspace-write/
// danger-full-access), so the primary agent's permissions.edit/write can
// bind to it — omp's blocker (--tools= is a CLI-invocation flag with no
// persistent equivalent) doesn't apply here. See sandboxMode's doc for
// the fidelity this reduces to (one filesystem axis, not two).
//
// CapAgentDefinitions: Codex supports standalone custom-agent TOML files
// under ~/.codex/agents/<name>.toml (personal) — confirmed current via
// developers.openai.com/codex/subagents — analogous to omp's per-agent
// markdown files. Deliberately NOT relying on any finer per-agent field
// this doc page doesn't name (e.g. a task-dispatch/step-budget knob):
// several 2026 openai/codex GitHub issues (#15250, #18823, #26868)
// report this exact file mechanism not always being picked up on spawn,
// so this renderer sticks to the documented, minimal field set
// (name/description/model/sandbox_mode/developer_instructions) rather
// than inventing behavior around an unconfirmed field.
//
// CapMCPPerToolAsk IS declared, despite mcp_servers.<server> being one
// global table in config.toml, not a per-agent one: see
// applyMCPPerToolAsk's doc for the resulting one-directional (adds
// prompts, never removes one) fidelity note.
//
// Declined entirely, becoming Gaps via DetectGaps:
//   - CapBash* / CapPerAgentBashPolicy / CapGlobalBashPolicy: Codex's
//     bash control surface is sandbox_mode/approval_policy, a coarse
//     global sandbox class — not a command-glob-to-decision policy
//     engine like bash.yaml models. Mapping one onto the other would
//     misrepresent the registry's actual security policy.
//   - CapExternalDirectory: sandbox_workspace_write.writable_roots takes
//     literal additional directories, not the glob-to-decision
//     (allow/deny/ask) map permissions.external_directory encodes.
//   - CapAgentSteps / CapAgentTaskPermission: no documented Codex field
//     for a per-agent step budget or sub-dispatch permission.
//   - CapPromptFileRef: developer_instructions/AGENTS.md content is
//     read eagerly into the rendered file, like omp; there's no
//     "{file:...}" lazy-load equivalent to point at instead.
func (renderer) Capabilities() []render.Capability {
	return []render.Capability{
		render.CapAgentDefinitions,
		render.CapComposeIntoPrimary,
		render.CapPromptAppend,
		render.CapPrimaryAgentToolPermission,
		render.CapModelLiteralBinding,
		render.CapMCPLocalTransport,
		render.CapMCPRemoteTransport,
		render.CapMCPToolGlobs,
		render.CapMCPPerToolAsk,
		render.CapProjectModelPolicy,
		render.CapCustomCommands,
	}
}

// Render produces a Plan that writes Codex's native ~/.codex/config.toml
// (model, sandbox_mode, mcp_servers), ~/.codex/AGENTS.md (primary agent
// prompt plus composed role: advisory sections), and one
// ~/.codex/agents/<name>.toml per independently dispatchable agent.
func (r renderer) Render(reg *registry.Registry, opt render.Options) (*render.Plan, error) {
	plan := &render.Plan{}
	plan.Gaps = append(plan.Gaps, render.DetectGaps(reg, r.Capabilities())...)

	readFile := opt.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	agentFiles, agentGaps, err := renderAgentFiles(reg, readFile)
	if err != nil {
		return nil, err
	}
	plan.Gaps = append(plan.Gaps, agentGaps...)
	plan.Outputs = append(plan.Outputs, render.RebuildDir{
		Dir:   agentsDir,
		Glob:  "*.toml",
		Files: agentFiles,
	})

	primary := render.PrimaryAgent(reg)
	if primary != nil && (!targets(primary.Targets) || primary.Opencode != nil) {
		// A role: primary agent that opts out of codex via targets:, or
		// that names a standing Opencode persona (Agent.Opencode != nil),
		// is invisible to codex entirely: no model/sandbox_mode binding,
		// no AGENTS.md content. The Opencode-persona case mirrors
		// schema.go's Agent.Opencode contract verbatim — "every renderer
		// other than opencode... renders nothing for [it] at all...
		// regardless of Targets/Role" — not just its prompt body; the
		// step's own Class/Permissions are opencode-persona-adjacent
		// metadata here too, not independent workflow-level truth to
		// bind elsewhere.
		primary = nil
	}

	if primary != nil {
		body, err := promptBody(*primary, readFile)
		if err != nil {
			return nil, fmt.Errorf("codex: primary agent %q: %w", primary.Name, err)
		}
		composed, err := composedSections(reg, readFile)
		if err != nil {
			return nil, err
		}
		if body != "" || composed != "" {
			plan.Outputs = append(plan.Outputs, render.WriteFile{
				Path:    agentsMdPath,
				Mode:    0600,
				Content: []byte(body + composed),
			})
		}
	}

	configObj := map[string]any{}
	var managed []string

	if primary != nil {
		if model := reg.ModelClasses[primary.Class]; model != "" {
			configObj["model"] = model
			managed = append(managed, "model")
		}
		if mode := sandboxMode(primary.Permissions); mode != "" {
			configObj["sandbox_mode"] = mode
			managed = append(managed, "sandbox_mode")
		}
		if gap := sandboxModeReductionGap("agent:"+primary.Name, render.CapPrimaryAgentToolPermission, primary.Permissions); gap != nil {
			plan.Gaps = append(plan.Gaps, *gap)
		}
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
	plan.Gaps = append(plan.Gaps, applyMCPPerToolAsk(reg, mcpServers)...)
	if len(mcpServers) > 0 {
		configObj["mcp_servers"] = mcpServers
		managed = append(managed, "mcp_servers")
	}

	if len(configObj) > 0 {
		plan.Outputs = append(plan.Outputs, render.MergeTOML{
			Path:    configPath,
			Mode:    0600,
			Managed: managed,
			Object:  configObj,
		})
	}

	commandsTree, err := render.RenderCommands(reg, readFile)
	if err != nil {
		return nil, fmt.Errorf("codex: rendering commands: %w", err)
	}
	plan.Outputs = append(plan.Outputs, commandsTree)

	return plan, nil
}

// RenderProject implements render.ProjectScopeRenderer: a directory-local
// .codex/config.toml pinning the resolved "default" class as the literal
// model, plus one thin .codex/agents/<name>.toml per class-bearing
// standalone agent carrying just its own resolved model. Codex layers
// project config over user config key-by-key (confirmed via
// developers.openai.com/codex/subagents: "Codex loads these files as
// configuration layers for spawned sessions"), so a project file naming
// only "model" overrides that one key while description/
// developer_instructions/sandbox_mode still come from the user-scope
// file of the same name — mirroring opencode's RenderProject, which
// does the analogous per-agent model-only override under its single
// opencode.json.
func (r renderer) RenderProject(classes map[string]string, reg *registry.Registry, dir string) (*render.Plan, error) {
	var outputs []render.Output

	if model := classes["default"]; model != "" {
		outputs = append(outputs, render.MergeTOML{
			Path:    filepath.Join(dir, projectConfigDir, projectConfigFile),
			Mode:    0600,
			Managed: []string{"model"},
			Object:  map[string]any{"model": model},
		})
	}

	for _, a := range reg.Agents {
		if a.Class == "" || !isStandaloneAgent(reg, a) {
			continue
		}
		model, ok := classes[a.Class]
		if !ok || model == "" {
			// A real class name should always resolve; skip
			// defensively rather than writing a zero-value model.
			continue
		}
		outputs = append(outputs, render.MergeTOML{
			Path:    filepath.Join(dir, projectAgentsDir, a.Name+".toml"),
			Mode:    0600,
			Managed: []string{"model"},
			Object:  map[string]any{"model": model},
		})
	}

	return &render.Plan{Outputs: outputs}, nil
}

// isStandaloneAgent reports whether agent a is rendered as its own
// ~/.codex/agents/<name>.toml file (by Render, with full content, and by
// RenderProject, with a thin model-only override): every codex-targeting
// agent except role: primary (the interactive session itself, not a
// file) and role: advisory when a role: primary agent exists to compose
// it into instead (see composedSections). A step naming an Opencode
// persona (Agent.Opencode != nil) is opencode-only by construction —
// Codex has no standing named-agent-definition concept it maps to, so
// it's excluded here regardless of Targets/Role, matching omp.
func isStandaloneAgent(reg *registry.Registry, a registry.Agent) bool {
	if !targets(a.Targets) || a.Opencode != nil || a.Role == "primary" {
		return false
	}
	if a.Role == "advisory" && render.PrimaryAgent(reg) != nil {
		return false
	}
	return true
}

// renderAgentFiles builds one WriteFile per standalone codex-targeting
// agent (see isStandaloneAgent), TOML-encoding name/description/model/
// sandbox_mode/developer_instructions per developers.openai.com/codex/
// subagents' documented custom-agent-file schema. Paths are relative to
// agentsDir, per RebuildDir's documented convention.
func renderAgentFiles(reg *registry.Registry, readFile func(string) ([]byte, error)) ([]render.WriteFile, []render.Gap, error) {
	var files []render.WriteFile
	var gaps []render.Gap
	for _, a := range reg.Agents {
		if !isStandaloneAgent(reg, a) {
			continue
		}
		body, err := promptBody(a, readFile)
		if err != nil {
			return nil, nil, fmt.Errorf("codex: agent %q: %w", a.Name, err)
		}
		content, err := toml.Marshal(renderAgentFile(reg, a, body))
		if err != nil {
			return nil, nil, fmt.Errorf("codex: agent %q: encoding TOML: %w", a.Name, err)
		}
		files = append(files, render.WriteFile{
			Path:    a.Name + ".toml",
			Mode:    0600,
			Content: content,
		})
		if gap := sandboxModeReductionGap("agent:"+a.Name, render.CapAgentDefinitions, a.Permissions); gap != nil {
			gaps = append(gaps, *gap)
		}
	}
	return files, gaps, nil
}

// composedSections builds the Markdown appended after the primary
// agent's own prompt body in AGENTS.md: one "## <name>[: <description>]"
// section per role: advisory, codex-targeting agent, in registry
// declaration order. Returns "" (no-op) when no agent has role:
// advisory. A step with Agent.Opencode set is opencode-only (see
// isStandaloneAgent) and is skipped here too, regardless of
// Role/Targets.
func composedSections(reg *registry.Registry, readFile func(string) ([]byte, error)) (string, error) {
	var b strings.Builder
	for _, a := range reg.Agents {
		if a.Role != "advisory" || !targets(a.Targets) || a.Opencode != nil {
			continue
		}
		body, err := promptBody(a, readFile)
		if err != nil {
			return "", fmt.Errorf("codex: composed agent %q: %w", a.Name, err)
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

// renderAgentFile builds one custom agent's TOML object per Codex's
// documented schema: name, description (required — falls back to the
// agent's own name when the registry left description empty, so a
// registry author isn't forced to add one just to satisfy Codex),
// optional model (resolved from Class) and sandbox_mode (derived from
// Permissions — see sandboxMode), and developer_instructions (the
// agent's full prompt body).
func renderAgentFile(reg *registry.Registry, a registry.Agent, body string) map[string]any {
	description := a.Description
	if description == "" {
		description = a.Name
	}
	obj := map[string]any{
		"name":                   a.Name,
		"description":            description,
		"developer_instructions": body,
	}
	if model := reg.ModelClasses[a.Class]; model != "" {
		obj["model"] = model
	}
	if mode := sandboxMode(a.Permissions); mode != "" {
		obj["sandbox_mode"] = mode
	}
	return obj
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
// harness") or an explicit list naming codex applies to this renderer.
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

// sandboxMode derives Codex's coarse sandbox_mode from an agent's
// edit/write permissions: an explicit "allow" on either means files can
// be created or modified, so it maps to the permissive
// "workspace-write"; both explicitly "deny" (or one denied and the
// other unset) maps to the conservative "read-only". Neither set
// returns "" (no opinion — Codex's own default, "workspace-write",
// applies unchanged). See sandboxModeReductionGap for the case this
// can't express faithfully.
func sandboxMode(p registry.Permissions) string {
	switch {
	case p.Edit == "allow" || p.Write == "allow":
		return "workspace-write"
	case p.Edit == "deny" || p.Write == "deny":
		return "read-only"
	default:
		return ""
	}
}

// sandboxModeReductionGap flags the one case sandboxMode can't express
// faithfully: edit and write set to opposite decisions. sandboxMode
// collapses both onto Codex's single filesystem axis and resolves any
// "allow" to "workspace-write" (the permissive direction), so
// edit:deny/write:allow and edit:allow/write:deny both end up
// over-granting relative to the finer edit-existing-file-vs-write-
// new-file split permissions.edit/write actually encodes.
func sandboxModeReductionGap(subject string, cap render.Capability, p registry.Permissions) *render.Gap {
	if p.Edit == "" || p.Write == "" || p.Edit == p.Write {
		return nil
	}
	return &render.Gap{
		Kind:       render.GapReduction,
		Capability: cap,
		Subject:    subject,
		Detail: fmt.Sprintf(
			"codex's sandbox_mode has one filesystem axis, not separate edit/write decisions; permissions.edit=%q and permissions.write=%q on %s collapsed to sandbox_mode=%q.",
			p.Edit, p.Write, subject, sandboxMode(p),
		),
	}
}

// renderMCPServer resolves one mcp_servers entry into Codex's native
// [mcp_servers.<name>] table shape (see developers.openai.com/codex/mcp):
// "local" splits Command into a single command string plus an args
// array — Codex has no argv-array primitive the way omp/opencode's own
// "command" key does; "remote" becomes url + http_headers. Tools (an
// explicit allowlist) becomes enabled_tools. A resolver failure skips
// the server rather than failing the whole Render; the caller records
// the returned Gap.
func renderMCPServer(s registry.MCPServer) (map[string]any, bool, *render.Gap) {
	entry := map[string]any{}
	switch s.Transport {
	case "remote":
		url, err := s.URL.Resolve()
		if err != nil {
			return nil, false, resolveFailureGap(s, "url", err)
		}
		entry["url"] = url
		if len(s.Headers) > 0 {
			headers := map[string]any{}
			for name, v := range s.Headers {
				resolved, err := v.Resolve()
				if err != nil {
					return nil, false, resolveFailureGap(s, "headers."+name, err)
				}
				headers[name] = resolved
			}
			entry["http_headers"] = headers
		}
	case "local":
		if len(s.Command) == 0 {
			return nil, false, resolveFailureGap(s, "command", fmt.Errorf("local transport declares no command"))
		}
		cmd, err := s.Command[0].Resolve()
		if err != nil {
			return nil, false, resolveFailureGap(s, "command", err)
		}
		entry["command"] = cmd
		if len(s.Command) > 1 {
			args := make([]any, 0, len(s.Command)-1)
			for _, part := range s.Command[1:] {
				resolved, err := part.Resolve()
				if err != nil {
					return nil, false, resolveFailureGap(s, "args", err)
				}
				args = append(args, resolved)
			}
			entry["args"] = args
		}
	default:
		return nil, false, resolveFailureGap(s, "transport", fmt.Errorf("unknown transport %q", s.Transport))
	}
	if len(s.Tools) > 0 {
		tools := make([]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			tools = append(tools, t)
		}
		entry["enabled_tools"] = tools
	}
	return entry, true, nil
}

// resolveFailureGap builds a GapSkip for an MCP server whose URL,
// command, or transport could not be resolved at render time.
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

// applyMCPPerToolAsk overlays each codex-targeting agent's per-tool ask
// patterns onto the already-built mcpServers map, as
// mcp_servers.<server>.tools.<tool>.approval_mode = "prompt" (see
// developers.openai.com/codex/mcp's tools.<tool>.approval_mode).
// Codex's tools.<tool> table key must be an exact tool name — unlike
// opencode's glob-capable permission key — so a pattern containing a
// glob character is dropped with a GapReduction instead of silently
// producing a TOML key that will never match a real tool call.
//
// mcp_servers.<server> is one global table, not a per-agent one — so
// this deliberately widens each ask pattern to every agent sharing
// that server, including one that granted the same tool without an
// ask. This is a one-directional (adds prompts, never removes one)
// fidelity reduction inherent to Codex's config surface (see
// Capabilities()'s CapMCPPerToolAsk note), not a per-registry
// condition DetectGaps can point at, so it's recorded here rather than
// as a Gap.
func applyMCPPerToolAsk(reg *registry.Registry, mcpServers map[string]any) []render.Gap {
	var gaps []render.Gap
	for _, a := range reg.Agents {
		if !targets(a.Targets) || a.Opencode != nil {
			continue
		}
		for _, m := range a.MCP {
			server, ok := mcpServers[m.Server].(map[string]any)
			if !ok {
				continue
			}
			for _, pattern := range m.Ask {
				if strings.ContainsAny(pattern, "*?[") {
					gaps = append(gaps, render.Gap{
						Kind:       render.GapReduction,
						Capability: render.CapMCPPerToolAsk,
						Subject:    fmt.Sprintf("agent:%s.mcp[%s].ask[%s]", a.Name, m.Server, pattern),
						Detail: fmt.Sprintf(
							"codex's per-tool approval override key must be an exact tool name; the glob pattern %q was dropped.",
							pattern,
						),
					})
					continue
				}
				tools, ok := server["tools"].(map[string]any)
				if !ok {
					tools = map[string]any{}
					server["tools"] = tools
				}
				tools[pattern] = map[string]any{"approval_mode": "prompt"}
			}
		}
	}
	return gaps
}
