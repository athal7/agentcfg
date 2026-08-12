// Package claude renders an agentcfg registry into Claude Code's native
// configuration: ~/.claude/agents/<name>.md subagent files (one per
// claude-targeting workflow step, including the primary agent — see
// Capabilities' CapPrimaryAgent note), ~/.claude/settings.json (the
// resolved default model, the primary agent's `agent:` binding, and a
// global per-tool ask list), ~/.claude.json's mcpServers key (merged
// narrowly — that file also carries OAuth session state and caches this
// renderer must never touch), and Agent Skills SKILL.md files under
// Claude's own skills root. See docs at code.claude.com/docs/en (settings,
// sub-agents, mcp, permissions, skills, memory — fetched and verified
// against the current release while building this renderer).
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

// id is the renderer identifier used by ID() and capability checks.
const id = "claude"

// targetName is the harness name agents/servers list under targets: to
// opt into or out of this renderer specifically.
const targetName = "claude"

// Paths are left unexpanded (literal "~"): tilde-expansion is the apply
// layer's job, not Render's — Render must stay pure.
const (
	settingsPath = "~/.claude/settings.json"
	agentsDir    = "~/.claude/agents"

	// claudeJSONPath holds Claude Code's OAuth session, per-project
	// state, and caches alongside user/local-scope MCP server config
	// (code.claude.com/docs/en/settings: "Other configuration is stored
	// in ~/.claude.json ... MCP server configurations for user and
	// local scopes ... per-project state ... and various caches").
	// Render must touch only the "mcpServers" key — see the MergeJSON
	// output below — never the rest of this file.
	claudeJSONPath = "~/.claude.json"

	// claudeSkillsDir is Claude Code's own personal Agent Skills root
	// (code.claude.com/docs/en/skills, "Where skills live"). It is NOT
	// render.CommandsSkillsDir: Claude discovers skills only from
	// ~/.claude/skills/ and .claude/skills/, never from ~/.agents/skills
	// (opencode's and omp's shared discovery path), so this renderer
	// calls render.RenderCommands with its own dir instead.
	claudeSkillsDir = "~/.claude/skills"
)

// projectConfigDir/projectSettingsFile/projectAgentsDir are literal,
// non-"~" path segments relative to the resolved project directory —
// unlike settingsPath/agentsDir/claudeJSONPath (all user-scope,
// home-relative), these live inside the project itself.
const (
	projectConfigDir    = ".claude"
	projectSettingsFile = "settings.json"
	projectAgentsDir    = ".claude/agents"
)

// New returns a Renderer that produces Claude Code's native config.
func New() render.Renderer { return renderer{} }

// renderer implements render.Renderer for the claude harness.
type renderer struct{}

// ID returns the renderer's identifier, "claude".
func (renderer) ID() string { return id }

// Capabilities returns the set of registry features this renderer can
// express in Claude Code's native config.
//
// CapPrimaryAgent is declared directly (full fidelity, not the
// CapPromptAppend substitute codex/omp fall back to): Claude Code's
// `agent` setting.json key runs the main thread AS a named subagent,
// "applying that subagent's system prompt, tool restrictions, and model"
// (code.claude.com/docs/en/settings) — the same real per-agent config
// object every other Claude subagent gets, not a whole-session prompt
// append with no permission surface. CapPrimaryAgentToolPermission
// follows for the same reason: renderAgentFile applies permissions.edit/
// write/task to every agent uniformly, including the primary one, via
// its own disallowedTools list.
//
// CapAgentSteps: a subagent's `maxTurns` frontmatter field is a genuine
// per-agent step budget, not a coarse global knob.
//
// CapAgentTaskPermission: `disallowedTools: [Agent]` in a subagent's OWN
// frontmatter prevents that specific subagent from delegating further —
// this is a per-agent tool-pool restriction (like `tools`/
// `disallowedTools` generally), not one of the session-wide permission
// RULES discussed below, so it isn't subject to their precedence
// limitation.
//
// CapModelLiteralBinding: a subagent's `model` field takes a literal
// alias or model ID, resolved from Class at render time — the same
// literal-embedding choice codex and opencode make. CapModelClassBinding
// is satisfied via SubstituteOf, not declared directly (see
// renderer.go's capabilitySubstitutePairs).
//
// CapMCPLocalTransport/CapMCPRemoteTransport: full transport parity —
// stdio (command+args) and http (url+headers).
//
// CapMCPPerToolAsk IS declared, despite settings.json's permissions.ask
// being one global list, not a per-agent one: see renderAskList's doc
// for the resulting one-directional (adds prompts, never removes one)
// fidelity note — the same shape of reduction omp's applyMCPPerToolAsk
// and codex's own CapMCPPerToolAsk already accept.
//
// CapProjectModelPolicy: RenderProject pins each project's resolved
// class literals — see its own doc comment for why this requires full
// per-agent content, not the thin model-only stub codex/opencode write.
//
// CapCustomCommands: rendered as Agent Skills SKILL.md files under
// Claude's own claudeSkillsDir (not render.CommandsSkillsDir — see that
// constant's doc).
//
// Declined entirely, becoming Gaps via DetectGaps:
//
//   - CapBash* (unordered map, ordered list, interior glob),
//     CapPerAgentBashPolicy, CapGlobalBashPolicy, CapExternalDirectory,
//     CapMCPToolGlobs: Claude Code's permissions.allow/deny/ask are
//     genuinely a command/path/tool-glob-to-decision rule system — on
//     its face the same shape bash.yaml, permissions.external_directory,
//     and mcp_servers[].tools model. But Claude resolves overlapping
//     rules by CATEGORY precedence, not specificity: "Rules are
//     evaluated in order: deny, then ask, then allow. The first match in
//     that order determines the outcome, and rule specificity doesn't
//     change the order" (code.claude.com/docs/en/permissions). bash.yaml
//     and permissions.external_directory model "most-specific-pattern-
//     wins" policies (bashpolicy.Compile) — the entire point of a
//     specificity-ordered exception, e.g. deny "git *" but allow "git
//     status", is that the narrower rule overrides the broader one.
//     Claude's category-first evaluation breaks exactly that case: a
//     broad ask/deny rule still wins over a narrower allow for the same
//     command, silently reversing the intended exception (verified
//     against the documented precedence rule directly, not inferred).
//     Rendering these as literal allow/deny/ask lists would therefore
//     misrepresent the registry's actual security policy whenever an
//     override exists — the same category of concern codex's own
//     Capabilities doc raises for sandbox_mode, but here the mechanism
//     genuinely looks like the right shape and would pass a shallow
//     review, which is exactly why it's called out explicitly rather
//     than just left off the list. CapMCPToolGlobs has an additional,
//     independent reason: an MCPServer.Tools allowlist needs a true
//     "these tools, nothing else from this server" expression, but
//     Claude's per-server denylist (`disallowedTools: mcp__<server>`)
//     can only remove a server wholesale — restricting to a NAMED subset
//     would require enumerating every tool NOT in the allowlist, and the
//     registry never discovers a server's full tool inventory to compute
//     that complement.
//
//   - CapComposeIntoPrimary: like opencode (and unlike omp), a Claude
//     subagent is independently dispatchable and permission-enforced on
//     its own frontmatter (tools/disallowedTools/maxTurns) — there's no
//     "omp has no per-subagent enforcement surface to dispatch to
//     safely" gap to work around by splicing role: advisory content into
//     the primary's own file instead.
//
//   - CapPromptFileRef: a subagent's Markdown body is read eagerly, like
//     codex's developer_instructions and omp's frontmatter body — there
//     is no lazy "{file:...}"-style reference Claude resolves against a
//     subagent's own body at dispatch time (CLAUDE.md's `@path` import
//     syntax is a distinct, whole-session memory-file mechanism this
//     renderer doesn't use for agent prompts at all).
//
//   - CapStructuredWorkflowCommand: no native multi-phase pipeline
//     trigger analogous to omp's `workflowz` keyword; a multi-step
//     command still renders as flattened, numbered prose.
func (renderer) Capabilities() []render.Capability {
	return []render.Capability{
		render.CapAgentDefinitions,
		render.CapPrimaryAgent,
		render.CapPrimaryAgentToolPermission,
		render.CapAgentSteps,
		render.CapAgentTaskPermission,
		render.CapModelLiteralBinding,
		render.CapMCPLocalTransport,
		render.CapMCPRemoteTransport,
		render.CapMCPPerToolAsk,
		render.CapProjectModelPolicy,
		render.CapCustomCommands,
	}
}

// Render produces a Plan that writes Claude Code's native
// ~/.claude/agents/<name>.md (one per claude-targeting agent), a
// ~/.claude/settings.json merge (default model, the primary agent's
// `agent:` binding, and a global permissions.ask list), a ~/.claude.json
// merge scoped to just the "mcpServers" key, and one Agent Skills
// SKILL.md per registry command.
func (r renderer) Render(reg *registry.Registry, opt render.Options) (*render.Plan, error) {
	plan := &render.Plan{}
	plan.Gaps = append(plan.Gaps, render.DetectGaps(reg, r.Capabilities())...)

	readFile := opt.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	classes := reg.EffectiveModelClasses("claude")
	modelFor := func(class string) string { return classes[class] }

	agentFiles, err := renderAgentFiles(reg, modelFor, readFile)
	if err != nil {
		return nil, err
	}
	plan.Outputs = append(plan.Outputs, render.RebuildDir{
		Dir:   agentsDir,
		Glob:  "*.md",
		Files: agentFiles,
	})

	settingsObj := map[string]any{}
	var managed []string

	if model := classes["default"]; model != "" {
		settingsObj["model"] = model
		managed = append(managed, "model")
	}

	primary := render.PrimaryAgent(reg)
	if primary != nil && claudeTargets(*primary) {
		settingsObj["agent"] = primary.Name
		managed = append(managed, "agent")
	}

	if askList := renderAskList(reg); len(askList) > 0 {
		settingsObj["permissions"] = map[string]any{"ask": askList}
		managed = append(managed, "permissions.ask")
	}

	if len(managed) > 0 {
		plan.Outputs = append(plan.Outputs, render.MergeJSON{
			Path:    settingsPath,
			Mode:    0600,
			Managed: managed,
			Object:  settingsObj,
		})
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
	if len(mcpServers) > 0 {
		plan.Outputs = append(plan.Outputs, render.MergeJSON{
			Path:    claudeJSONPath,
			Mode:    0600,
			Managed: []string{"mcpServers"},
			Object:  map[string]any{"mcpServers": mcpServers},
		})
	}

	commandsTree, err := render.RenderCommands(claudeSkillsDir, reg, readFile)
	if err != nil {
		return nil, fmt.Errorf("claude: rendering commands: %w", err)
	}
	plan.Outputs = append(plan.Outputs, commandsTree)

	return plan, nil
}

// RenderProject implements render.ProjectScopeRenderer: a directory-local
// .claude/settings.json pinning the resolved "default" class as the
// literal model, plus one FULL .claude/agents/<name>.md per class-bearing
// claude-targeting agent — not the thin model-only stub codex's and
// opencode's own RenderProject write.
//
// This has to be full content because Claude resolves a subagent name to
// exactly ONE file: "When multiple subagents share the same name, Claude
// Code uses the one from the higher-priority location"
// (code.claude.com/docs/en/sub-agents) — project scope (priority 3) wins
// outright over user scope (priority 4), it does not merge fields the
// way codex's TOML config layering or opencode's MergeJSON managed-path
// merge do. A thin ".claude/agents/<name>.md" carrying just `model:`
// would therefore silently replace, not layer over, the user-scope
// file's tools/disallowedTools/maxTurns/prompt body. renderAgentFile is
// reused verbatim from Render, just fed classes[a.Class] instead of
// reg.ModelClasses[a.Class].
func (r renderer) RenderProject(classes map[string]string, reg *registry.Registry, dir string) (*render.Plan, error) {
	var outputs []render.Output

	if model := classes["default"]; model != "" {
		outputs = append(outputs, render.MergeJSON{
			Path:    filepath.Join(dir, projectConfigDir, projectSettingsFile),
			Mode:    0600,
			Managed: []string{"model"},
			Object:  map[string]any{"model": model},
		})
	}

	var agentFiles []render.WriteFile
	for _, a := range reg.Agents {
		if a.Class == "" || !claudeTargets(a) {
			continue
		}
		model, ok := classes[a.Class]
		if !ok || model == "" {
			// A real class name should always resolve; skip
			// defensively rather than writing a zero-value model.
			continue
		}
		// RenderProject carries no Options/ReadFile injection point
		// (see the ProjectScopeRenderer interface) — os.ReadFile is
		// the only source available for a file-backed prompt here,
		// same as Options.ReadFile's own os.ReadFile default.
		body, err := promptBody(a, os.ReadFile)
		if err != nil {
			return nil, fmt.Errorf("claude: project agent %q: %w", a.Name, err)
		}
		agentFiles = append(agentFiles, render.WriteFile{
			Path:    a.Name + ".md",
			Mode:    0600,
			Content: []byte(renderAgentFile(reg, a, model, body)),
		})
	}
	// Unconditional, even when agentFiles is empty: RebuildDir is the
	// pruning mechanism for this directory, so a project that drops
	// its last class-bearing agent (or opts every agent out of claude)
	// must still clear a stale file that would otherwise keep pinning
	// an old model and prompt body for that subagent name.
	outputs = append(outputs, render.RebuildDir{
		Dir:   filepath.Join(dir, projectAgentsDir),
		Glob:  "*.md",
		Files: agentFiles,
	})

	return &render.Plan{Outputs: outputs}, nil
}

// claudeTargets reports whether agent a is rendered for claude at all: it
// must opt in via Targets (or declare none, meaning every harness), and
// must not name a standing Opencode persona (Agent.Opencode != nil) —
// that construct is opencode-only by schema.go's own contract ("every
// renderer other than opencode... renders nothing for [it] at all...
// regardless of Targets/Role"), matching codex and omp.
func claudeTargets(a registry.Agent) bool {
	return targets(a.Targets) && a.Opencode == nil
}

// targets reports whether an omitted/empty targets list (meaning "every
// harness") or an explicit list naming claude applies to this renderer.
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

// renderAgentFiles builds one WriteFile per claude-targeting agent —
// role: primary included, unlike codex/omp: Claude's `agent` setting
// binds the primary session to a real subagent file, so primary gets the
// exact same full-content treatment as delegate/advisory (see
// Capabilities' CapPrimaryAgent note). Paths are relative to agentsDir,
// per RebuildDir's documented convention.
func renderAgentFiles(reg *registry.Registry, modelFor func(string) string, readFile func(string) ([]byte, error)) ([]render.WriteFile, error) {
	var files []render.WriteFile
	for _, a := range reg.Agents {
		if !claudeTargets(a) {
			continue
		}
		body, err := promptBody(a, readFile)
		if err != nil {
			return nil, fmt.Errorf("claude: agent %q: %w", a.Name, err)
		}
		files = append(files, render.WriteFile{
			Path:    a.Name + ".md",
			Mode:    0600,
			Content: []byte(renderAgentFile(reg, a, modelFor(a.Class), body)),
		})
	}
	return files, nil
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

// renderAgentFile builds one subagent's full Markdown file: YAML
// frontmatter (name, description, optional model/maxTurns/
// disallowedTools) followed by "---" and the raw prompt body.
// description falls back to a.Name when the registry left it empty
// (description is required by Claude's own schema, mirroring codex's
// identical fallback), and is strconv.Quote-d like commands.go's
// renderSkillFile — arbitrary author text may contain ": ", "#", or a
// newline, any of which would corrupt unquoted YAML frontmatter.
//
// disallowedTools composes three independent restrictions, each a bare
// tool-name removal (never a path/command-glob rule, so none of it is
// subject to Capabilities' category-precedence limitation):
//   - permissions.edit/write == "deny" removes Edit/Write.
//   - permissions.task == "deny" removes Agent, preventing this specific
//     subagent from delegating further (CapAgentTaskPermission).
//   - every claude-targeting MCP server this agent's own mcp: list does
//     NOT reference is removed via "mcp__<server>" (a per-server, not
//     per-tool, denial — see Capabilities' CapMCPToolGlobs note for why
//     per-tool isn't attempted), giving each agent exactly the server
//     visibility its mcp: grants declare, the same hard allowlist
//     omp's frontmatter tools: list and opencode's per-agent tools map
//     already enforce.
func renderAgentFile(reg *registry.Registry, a registry.Agent, model, body string) string {
	description := a.Description
	if description == "" {
		description = a.Name
	}

	var disallowed []string
	if a.Permissions.Edit == "deny" {
		disallowed = append(disallowed, "Edit")
	}
	if a.Permissions.Write == "deny" {
		disallowed = append(disallowed, "Write")
	}
	if a.Permissions.Task == "deny" {
		disallowed = append(disallowed, "Agent")
	}

	granted := make(map[string]bool, len(a.MCP))
	for _, m := range a.MCP {
		granted[m.Server] = true
	}
	for _, s := range reg.MCPServers {
		if !targets(s.Targets) || granted[s.Name] {
			continue
		}
		disallowed = append(disallowed, "mcp__"+s.Name)
	}

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", a.Name)
	fmt.Fprintf(&b, "description: %s\n", strconv.Quote(description))
	if model != "" {
		fmt.Fprintf(&b, "model: %s\n", strconv.Quote(model))
	}
	if a.Steps != nil {
		fmt.Fprintf(&b, "maxTurns: %d\n", *a.Steps)
	}
	if len(disallowed) > 0 {
		fmt.Fprintf(&b, "disallowedTools: %s\n", strings.Join(disallowed, ", "))
	}
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

// renderAskList collects every claude-targeting agent's AgentMCP.Ask
// glob patterns into settings.json's global permissions.ask list, as
// "mcp__<server>__<pattern>" entries — Claude's deny/ask rules accept an
// arbitrary glob in the tool-name position (code.claude.com/docs/en/
// permissions, "Tool name wildcards"), so the pattern is embedded
// literally, unlike codex's exact-tool-name TOML keys, which must
// pre-expand a glob against the server's own Tools list first.
//
// permissions.ask is one global list, not a per-agent one — Claude has
// no per-subagent permission-rule surface (see Capabilities' CapBash*
// note), only the coarser tools/disallowedTools allowlist. This
// deliberately widens each ask pattern to every caller sharing that
// server, including one that granted the same tool without an ask —
// the identical one-directional (adds prompts, never removes one)
// fidelity reduction omp's applyMCPPerToolAsk and codex's own
// CapMCPPerToolAsk already accept, not a per-registry condition
// DetectGaps can point at, so it's documented here rather than as a Gap.
// Deduplicated and sorted for a deterministic, reproducible Plan.
func renderAskList(reg *registry.Registry) []string {
	seen := map[string]bool{}
	for _, a := range reg.Agents {
		if !claudeTargets(a) {
			continue
		}
		for _, m := range a.MCP {
			for _, pattern := range m.Ask {
				seen["mcp__"+m.Server+"__"+pattern] = true
			}
		}
	}
	list := make([]string, 0, len(seen))
	for k := range seen {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}

// renderMCPServer resolves one mcp_servers entry into the shape Claude's
// mcpServers config expects (code.claude.com/docs/en/mcp): "local"
// splits Command into a command string plus an args array, typed
// "stdio"; "remote" becomes url + headers, typed "http" (the modern,
// recommended transport per Claude's own docs — "sse" is deprecated and
// "ws" has no registry-level equivalent to bind to). A resolver failure
// skips the server rather than failing the whole Render; the caller
// records the returned Gap.
func renderMCPServer(s registry.MCPServer) (map[string]any, bool, *render.Gap) {
	entry := map[string]any{}
	switch s.Transport {
	case "remote":
		entry["type"] = "http"
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
			entry["headers"] = headers
		}
	case "local":
		entry["type"] = "stdio"
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
