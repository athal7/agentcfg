package codex

import (
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

func baseBashPolicy() registry.BashPolicy {
	return registry.BashPolicy{
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Allow},
		},
	}
}

func fixtureReadFile(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		body, ok := files[path]
		if !ok {
			return nil, &pathNotFoundError{path}
		}
		return []byte(body), nil
	}
}

type pathNotFoundError struct{ path string }

func (e *pathNotFoundError) Error() string { return "no fixture for path: " + e.path }

func outputByType[T render.Output](t *testing.T, outputs []render.Output) T {
	t.Helper()
	for _, o := range outputs {
		if v, ok := o.(T); ok {
			return v
		}
	}
	t.Fatalf("no output of type %T found among %d outputs", *new(T), len(outputs))
	panic("unreachable")
}

func gapForSubject(gaps []render.Gap, subject string) (render.Gap, bool) {
	for _, g := range gaps {
		if g.Subject == subject {
			return g, true
		}
	}
	return render.Gap{}, false
}

// TestRender_LeadAdvisoryAndBuild covers the happy path: a role: primary
// agent, a role: advisory agent composed into AGENTS.md, a role: delegate
// standalone agent, one local and one remote MCP server, an exact-match
// per-tool ask pattern, and a glob per-tool ask pattern (which Codex can't
// express as an exact TOML table key).
func TestRender_LeadAdvisoryAndBuild(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "gpt-5.5", "smol": "gpt-5.5-mini", "big": "gpt-5.6"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:        "lead",
				Role:        "primary",
				Class:       "big",
				Prompt:      registry.Prompt{Text: "You are the lead."},
				Permissions: registry.Permissions{Edit: "allow", Write: "allow"},
			},
			{
				Name:        "reviewer",
				Role:        "advisory",
				Description: "reviews diffs",
				Class:       "smol",
				Prompt:      registry.Prompt{Text: "You review diffs."},
			},
			{
				Name:               "build",
				Role:               "delegate",
				Class:              "default",
				Prompt:             registry.Prompt{File: "prompts/build.md"},
				ResolvedPromptFile: "/registry/prompts/build.md",
				Permissions:        registry.Permissions{Edit: "allow", Write: "allow"},
				MCP: []registry.AgentMCP{
					{Server: "fs", Ask: []string{"write_file"}},
					{Server: "gh", Ask: []string{"create_*"}},
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "fs",
				Transport: "local",
				Command:   []registry.Value{{Literal: "mcp-fs"}, {Literal: "--root"}, {Literal: "/tmp"}},
				Tools:     []string{"read_file", "write_file"},
			},
			{
				Name:      "gh",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://api.github.com/mcp"},
				Headers:   map[string]registry.Value{"Authorization": {Literal: "Bearer tok"}},
			},
		},
	}

	readFile := fixtureReadFile(map[string]string{
		"/registry/prompts/build.md": "You build things.",
	})

	plan, err := New().Render(reg, render.Options{ReadFile: readFile})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	// Only the glob ask pattern should surface as a gap; everything else
	// in this fixture is fully expressible.
	if len(plan.Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(plan.Gaps), plan.Gaps)
	}
	gap := plan.Gaps[0]
	if gap.Kind != render.GapReduction || gap.Capability != render.CapMCPPerToolAsk {
		t.Errorf("got gap %+v, want a CapMCPPerToolAsk GapReduction", gap)
	}

	if len(plan.Outputs) != 4 {
		t.Fatalf("got %d outputs, want 4 (agents RebuildDir, AGENTS.md, config.toml MergeTOML, commands RebuildTree): %+v", len(plan.Outputs), plan.Outputs)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if rebuild.Dir != agentsDir || rebuild.Glob != "*.toml" {
		t.Errorf("got RebuildDir{Dir:%q,Glob:%q}, want %s,*.toml", rebuild.Dir, rebuild.Glob, agentsDir)
	}
	if len(rebuild.Files) != 1 {
		t.Fatalf("got %d agent files, want 1 (primary and composed advisory excluded): %+v", len(rebuild.Files), rebuild.Files)
	}
	if rebuild.Files[0].Path != "build.toml" {
		t.Errorf("got path %q, want build.toml", rebuild.Files[0].Path)
	}
	buildContent := string(rebuild.Files[0].Content)
	for _, want := range []string{
		`name = "build"`,
		`description = "build"`, // falls back to name: registry left it empty
		`model = "gpt-5.5"`,
		`sandbox_mode = "workspace-write"`,
		"You build things.",
	} {
		if !strings.Contains(buildContent, want) {
			t.Errorf("build.toml missing %q; got:\n%s", want, buildContent)
		}
	}

	agentsMd := outputByType[render.WriteFile](t, plan.Outputs)
	if agentsMd.Path != agentsMdPath {
		t.Errorf("got WriteFile path %q, want %q", agentsMd.Path, agentsMdPath)
	}
	body := string(agentsMd.Content)
	if !strings.Contains(body, "You are the lead.") {
		t.Errorf("AGENTS.md missing primary body; got:\n%s", body)
	}
	if !strings.Contains(body, "## reviewer: reviews diffs") || !strings.Contains(body, "You review diffs.") {
		t.Errorf("AGENTS.md missing composed advisory section; got:\n%s", body)
	}

	cfg := outputByType[render.MergeTOML](t, plan.Outputs)
	if cfg.Path != configPath {
		t.Errorf("got MergeTOML path %q, want %q", cfg.Path, configPath)
	}
	if cfg.Object["model"] != "gpt-5.6" {
		t.Errorf("got model %v, want gpt-5.6 (lead's class)", cfg.Object["model"])
	}
	if cfg.Object["sandbox_mode"] != "workspace-write" {
		t.Errorf("got sandbox_mode %v, want workspace-write", cfg.Object["sandbox_mode"])
	}
	mcpServers, ok := cfg.Object["mcp_servers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers is %T, want map[string]any", cfg.Object["mcp_servers"])
	}
	fsEntry, ok := mcpServers["fs"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers[fs] is %T, want map[string]any", mcpServers["fs"])
	}
	if fsEntry["command"] != "mcp-fs" {
		t.Errorf("got fs command %v, want mcp-fs", fsEntry["command"])
	}
	if args, ok := fsEntry["args"].([]any); !ok || len(args) != 2 || args[0] != "--root" || args[1] != "/tmp" {
		t.Errorf("got fs args %v, want [--root /tmp]", fsEntry["args"])
	}
	fsTools, ok := fsEntry["tools"].(map[string]any)
	if !ok {
		t.Fatalf("fs.tools is %T, want map[string]any (from build's exact-match ask pattern)", fsEntry["tools"])
	}
	writeFileTool, ok := fsTools["write_file"].(map[string]any)
	if !ok || writeFileTool["approval_mode"] != "prompt" {
		t.Errorf("got fs.tools[write_file] %v, want {approval_mode: prompt}", fsTools["write_file"])
	}
	ghEntry, ok := mcpServers["gh"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_servers[gh] is %T, want map[string]any", mcpServers["gh"])
	}
	if ghEntry["url"] != "https://api.github.com/mcp" {
		t.Errorf("got gh url %v, want https://api.github.com/mcp", ghEntry["url"])
	}
	if _, hasTools := ghEntry["tools"]; hasTools {
		t.Errorf("gh.tools should be unset: the only ask pattern on it (create_*) is a glob and was dropped")
	}

	_ = outputByType[render.RebuildTree](t, plan.Outputs) // commands tree always present
}

// TestRender_MCPResolveFailureSkipsServer covers an MCP server whose URL
// can't be resolved: it's omitted from mcp_servers and a GapSkip is
// recorded, rather than failing the whole Render.
func TestRender_MCPResolveFailureSkipsServer(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken",
				Transport: "remote",
				URL:       registry.Value{From: "command", Run: []string{}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	gap, found := gapForSubject(plan.Gaps, "mcp:broken")
	if !found {
		t.Fatalf("expected a gap for mcp:broken, got %+v", plan.Gaps)
	}
	if gap.Kind != render.GapSkip || gap.Capability != render.CapMCPRemoteTransport {
		t.Errorf("got gap %+v, want a CapMCPRemoteTransport GapSkip", gap)
	}

	for _, o := range plan.Outputs {
		if m, ok := o.(render.MergeTOML); ok {
			t.Errorf("no MergeTOML output expected: mcp_servers is empty since the only server failed to resolve and there's no primary agent, got %+v", m)
		}
	}
}

// TestRender_AsymmetricEditWriteReportsReduction covers an agent whose
// edit/write permissions disagree: sandboxMode collapses them onto one
// axis, so the mismatch is flagged as a GapReduction rather than silently
// resolved.
func TestRender_AsymmetricEditWriteReportsReduction(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:        "primary",
				Role:        "primary",
				Prompt:      registry.Prompt{Text: "hi"},
				Permissions: registry.Permissions{Edit: "deny", Write: "allow"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	gap, found := gapForSubject(plan.Gaps, "agent:primary")
	if !found {
		t.Fatalf("expected a gap for agent:primary, got %+v", plan.Gaps)
	}
	if gap.Kind != render.GapReduction || gap.Capability != render.CapPrimaryAgentToolPermission {
		t.Errorf("got gap %+v, want a CapPrimaryAgentToolPermission GapReduction", gap)
	}

	cfg := outputByType[render.MergeTOML](t, plan.Outputs)
	if cfg.Object["sandbox_mode"] != "workspace-write" {
		t.Errorf("got sandbox_mode %v, want workspace-write (write:allow wins over edit:deny)", cfg.Object["sandbox_mode"])
	}
}

// TestRender_UndeclaredCapabilitiesSurfaceAsGaps covers the capabilities
// this renderer deliberately doesn't express: a per-agent bash profile,
// external_directory, and permissions.task should each surface via
// DetectGaps, not be silently dropped.
func TestRender_UndeclaredCapabilitiesSurfaceAsGaps(t *testing.T) {
	reg := &registry.Registry{
		Bash: registry.BashPolicy{
			Profiles: map[string]registry.BashProfile{
				"global":     {Base: registry.Allow},
				"restricted": {Base: registry.Deny},
			},
		},
		Agents: []registry.Agent{
			{
				Name:   "build",
				Role:   "delegate",
				Prompt: registry.Prompt{Text: "hi"},
				Permissions: registry.Permissions{
					Task:              "allow",
					Bash:              registry.BashPermission{Profile: "restricted"},
					ExternalDirectory: map[string]registry.Decision{"~/code/**": registry.Allow},
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	wantCaps := map[render.Capability]bool{
		render.CapPerAgentBashPolicy:  false,
		render.CapExternalDirectory:   false,
		render.CapAgentTaskPermission: false,
	}
	for _, g := range plan.Gaps {
		if _, ok := wantCaps[g.Capability]; ok {
			wantCaps[g.Capability] = true
		}
	}
	for cap, seen := range wantCaps {
		if !seen {
			t.Errorf("expected a gap for undeclared capability %q, got %+v", cap, plan.Gaps)
		}
	}
}

// TestRenderProject_PinsModelPerAgent covers RenderProject: the
// project-root config.toml pins the resolved "default" class, and each
// class-bearing standalone agent gets its own thin model-only override
// file; the primary agent (whose model lives in the project config.toml
// itself, not a per-agent file) and an advisory agent composed into
// AGENTS.md are both excluded.
func TestRenderProject_PinsModelPerAgent(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "big"},
			{Name: "reviewer", Role: "advisory", Class: "smol"},
			{Name: "build", Role: "delegate", Class: "default"},
		},
	}
	classes := map[string]string{"default": "gpt-5.5-ctx", "smol": "gpt-5.5-mini-ctx", "big": "gpt-5.6-ctx"}

	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	if len(plan.Outputs) != 2 {
		t.Fatalf("got %d outputs, want 2 (project config.toml + build.toml override): %+v", len(plan.Outputs), plan.Outputs)
	}

	root := outputByType[render.MergeTOML](t, plan.Outputs)
	if root.Path != "/repo/.codex/config.toml" {
		t.Errorf("got path %q, want /repo/.codex/config.toml", root.Path)
	}
	if root.Object["model"] != "gpt-5.5-ctx" {
		t.Errorf("got model %v, want gpt-5.5-ctx", root.Object["model"])
	}

	var buildOverride render.MergeTOML
	found := false
	for _, o := range plan.Outputs {
		if m, ok := o.(render.MergeTOML); ok && m.Path == "/repo/.codex/agents/build.toml" {
			buildOverride, found = m, true
		}
	}
	if !found {
		t.Fatalf("expected a /repo/.codex/agents/build.toml override, got %+v", plan.Outputs)
	}
	if buildOverride.Object["model"] != "gpt-5.5-ctx" {
		t.Errorf("got build override model %v, want gpt-5.5-ctx", buildOverride.Object["model"])
	}
	if buildOverride.Managed[0] != "model" {
		t.Errorf("got Managed %v, want [model]", buildOverride.Managed)
	}
}

// TestCapabilities_MatchesDetectGaps is a light sanity check that
// Capabilities() is non-empty and includes the two required substitutes
// this renderer relies on (CapPromptAppend in place of CapPrimaryAgent,
// and CapCustomCommands for the shared Agent Skills mechanism).
func TestCapabilities_MatchesDetectGaps(t *testing.T) {
	caps := New().Capabilities()
	has := map[render.Capability]bool{}
	for _, c := range caps {
		has[c] = true
	}
	for _, want := range []render.Capability{render.CapPromptAppend, render.CapCustomCommands, render.CapModelLiteralBinding} {
		if !has[want] {
			t.Errorf("Capabilities() missing %q", want)
		}
	}
}

func TestID(t *testing.T) {
	if got := New().ID(); got != "codex" {
		t.Errorf("got ID() %q, want codex", got)
	}
}

// TestCapabilities_ExcludesUndeclaredOnes locks in the deliberate
// omissions documented on Capabilities(): bash policy, external
// directory, agent steps, agent task permission, and prompt-file
// references have no Codex config surface this renderer maps to, so
// declaring them would make DetectGaps silently swallow a real,
// permanently-dropped registry feature.
func TestCapabilities_ExcludesUndeclaredOnes(t *testing.T) {
	declined := []render.Capability{
		render.CapBashUnorderedMap,
		render.CapBashOrderedList,
		render.CapBashInteriorGlob,
		render.CapPerAgentBashPolicy,
		render.CapGlobalBashPolicy,
		render.CapExternalDirectory,
		render.CapAgentSteps,
		render.CapAgentTaskPermission,
		render.CapPromptFileRef,
	}
	has := map[render.Capability]bool{}
	for _, c := range New().Capabilities() {
		has[c] = true
	}
	for _, c := range declined {
		if has[c] {
			t.Errorf("Capabilities() declares %q, want it absent (see Capabilities()'s doc comment for why it's declined)", c)
		}
	}
}

// TestRender_UnresolvableLocalMCPServerSkippedWithGap is
// TestRender_MCPResolveFailureSkipsServer's local-transport twin: a
// command that fails to resolve skips the server with a
// CapMCPLocalTransport GapSkip, distinct from the remote-transport
// capability the other test exercises.
func TestRender_UnresolvableLocalMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken-local",
				Transport: "local",
				Command:   []registry.Value{{From: "command", Run: []string{}}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	gap, found := gapForSubject(plan.Gaps, "mcp:broken-local")
	if !found {
		t.Fatalf("expected a gap for mcp:broken-local, got %+v", plan.Gaps)
	}
	if gap.Kind != render.GapSkip || gap.Capability != render.CapMCPLocalTransport {
		t.Errorf("got gap %+v, want a CapMCPLocalTransport GapSkip", gap)
	}
}

// TestRender_NoPrimaryAgentOmitsModelAndAgentsMd covers a registry with
// no role: primary agent at all: there's no session to bind a model/
// sandbox_mode to and nothing to append to AGENTS.md, so neither output
// is produced — Render must not write an empty/zero-value config.toml
// or an empty AGENTS.md just because a standalone agent exists.
func TestRender_NoPrimaryAgentOmitsModelAndAgentsMd(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Prompt: registry.Prompt{Text: "hi"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if len(plan.Outputs) != 2 {
		t.Fatalf("got %d outputs, want 2 (agents RebuildDir + commands RebuildTree): %+v", len(plan.Outputs), plan.Outputs)
	}
	for _, o := range plan.Outputs {
		switch o.(type) {
		case render.WriteFile:
			t.Errorf("no WriteFile (AGENTS.md) expected without a primary agent, got %+v", o)
		case render.MergeTOML:
			t.Errorf("no MergeTOML (config.toml) expected: no primary agent to bind model/sandbox_mode and no MCP servers, got %+v", o)
		}
	}
}

// TestRenderProject_UnknownClassSkipsAgentDefensively mirrors
// opencode's RenderProject test of the same name: a real class name
// should always resolve, but a class missing from the caller's map is
// skipped rather than writing a zero-value model override.
func TestRenderProject_UnknownClassSkipsAgentDefensively(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Class: "missing"},
		},
	}

	plan, err := New().(render.ProjectScopeRenderer).RenderProject(map[string]string{"default": "gpt-5.5"}, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	for _, o := range plan.Outputs {
		if m, ok := o.(render.MergeTOML); ok && m.Path == "/repo/.codex/agents/build.toml" {
			t.Fatalf("expected no override for build (class %q unresolved), got %+v", "missing", m)
		}
	}
}

// TestRender_CommandsRenderAsSkillFiles is a content-level check (the
// happy-path test above only asserts the RebuildTree's presence via
// outputByType) that codex renders registry commands through the exact
// same shared Agent Skills mechanism opencode/omp use.
func TestRender_CommandsRenderAsSkillFiles(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		Commands: []registry.Command{
			{Name: "review", Description: "Reviews a diff", Prompt: registry.Prompt{Text: "Review the diff."}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0 (codex declares CapCustomCommands): %+v", len(plan.Gaps), plan.Gaps)
	}

	tree := outputByType[render.RebuildTree](t, plan.Outputs)
	if tree.Dir != render.CommandsSkillsDir {
		t.Errorf("got Dir %q, want %q", tree.Dir, render.CommandsSkillsDir)
	}
	files, ok := tree.Dirs["review"]
	if !ok || len(files) != 1 || files[0].Path != "SKILL.md" {
		t.Fatalf("Dirs[review] = %+v, want exactly one SKILL.md entry", tree.Dirs["review"])
	}
	want := "---\nname: review\ndescription: \"Reviews a diff\"\n---\nReview the diff."
	if string(files[0].Content) != want {
		t.Errorf("Content = %q, want %q", files[0].Content, want)
	}
}

// TestRender_TargetsRestrictsOutput covers both an agent and an MCP
// server excluded from codex via targets: naming a different harness:
// the agent gets no standalone file, and the server is absent from
// mcp_servers entirely.
func TestRender_TargetsRestrictsOutput(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "vscode-only", Role: "delegate", Prompt: registry.Prompt{Text: "x"}, Targets: []string{"vscode"}},
			{Name: "everywhere", Role: "delegate", Prompt: registry.Prompt{Text: "y"}},
		},
		MCPServers: []registry.MCPServer{
			{Name: "vscode-server", Transport: "local", Command: []registry.Value{{Literal: "x"}}, Targets: []string{"vscode"}},
			{Name: "everywhere-server", Transport: "local", Command: []registry.Value{{Literal: "y"}}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 1 || rebuild.Files[0].Path != "everywhere.toml" {
		t.Errorf("got agent files %+v, want exactly [everywhere.toml]", rebuild.Files)
	}

	cfg := outputByType[render.MergeTOML](t, plan.Outputs)
	servers := cfg.Object["mcp_servers"].(map[string]any)
	if _, ok := servers["vscode-server"]; ok {
		t.Error("vscode-server should be excluded: its targets don't include codex")
	}
	if _, ok := servers["everywhere-server"]; !ok {
		t.Error("everywhere-server should be included: it has no targets restriction")
	}
}

// TestRender_OpencodeOverrideStepRendersNothingForCodex covers a step
// whose Opencode field names a standing OpencodeAgent persona: per
// Agent.Opencode's contract, every renderer other than opencode treats
// this as opencode-only and renders nothing for it, regardless of
// Targets/Role — codex has no standing named-agent-definition concept
// this mechanism maps to.
func TestRender_OpencodeOverrideStepRendersNothingForCodex(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Prompt: registry.Prompt{Text: "You are the lead."}},
			{
				Name:     "verify",
				Role:     "delegate",
				Prompt:   registry.Prompt{Text: "Verify the change."},
				Opencode: &registry.StepOpencode{Agent: "qa"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Errorf("got %d standalone agent files, want 0 (opencode-override step must not render): %+v", len(rebuild.Files), rebuild.Files)
	}

	agentsMd := outputByType[render.WriteFile](t, plan.Outputs)
	want := "You are the lead."
	if string(agentsMd.Content) != want {
		t.Errorf("got AGENTS.md content %q, want %q (opencode-override step must not appear)", agentsMd.Content, want)
	}
}
