package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

func fixtureReadFile(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		body, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("no fixture for path: %s", path)
		}
		return []byte(body), nil
	}
}

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

func outputsByType[T render.Output](outputs []render.Output) []T {
	var out []T
	for _, o := range outputs {
		if v, ok := o.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func gapForSubject(gaps []render.Gap, subject string) (render.Gap, bool) {
	for _, g := range gaps {
		if g.Subject == subject {
			return g, true
		}
	}
	return render.Gap{}, false
}

func agentFile(t *testing.T, rebuild render.RebuildDir, name string) render.WriteFile {
	t.Helper()
	for _, f := range rebuild.Files {
		if f.Path == name {
			return f
		}
	}
	t.Fatalf("no agent file %q among %+v", name, rebuild.Files)
	panic("unreachable")
}

// TestRender_LeadAdvisoryAndBuild covers the happy path: a role: primary
// agent (bound via the `agent` setting, full permission/tool surface), a
// role: advisory agent (a standalone file, not spliced into anything —
// see Capabilities' CapComposeIntoPrimary note), a role: delegate agent
// with a step budget and per-tool ask patterns, one local and one remote
// MCP server, and per-agent MCP server visibility scoping.
func TestRender_LeadAdvisoryAndBuild(t *testing.T) {
	steps := 40
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "sonnet", "big": "opus"},
		Agents: []registry.Agent{
			{
				Name:        "lead",
				Role:        "primary",
				Class:       "big",
				Prompt:      registry.Prompt{Text: "You are the lead."},
				Permissions: registry.Permissions{Edit: "deny", Write: "deny", Task: "allow"},
			},
			{
				Name:        "reviewer",
				Role:        "advisory",
				Description: "reviews diffs",
				Prompt:      registry.Prompt{Text: "You review diffs."},
				Permissions: registry.Permissions{Edit: "deny", Write: "deny", Task: "deny"},
			},
			{
				Name:               "build",
				Role:               "delegate",
				Class:              "default",
				Steps:              &steps,
				Prompt:             registry.Prompt{File: "prompts/build.md"},
				ResolvedPromptFile: "/registry/prompts/build.md",
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
	// role: advisory always surfaces a compose_into_primary reduction:
	// this renderer doesn't declare CapComposeIntoPrimary (see
	// Capabilities' doc) — the agent is still fully rendered as a
	// standalone file, just not spliced into anything.
	if len(plan.Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(plan.Gaps), plan.Gaps)
	}
	if g := plan.Gaps[0]; g.Kind != render.GapReduction || g.Capability != render.CapComposeIntoPrimary || g.Subject != "agent:reviewer" {
		t.Errorf("got gap %+v, want a CapComposeIntoPrimary GapReduction for agent:reviewer", g)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if rebuild.Dir != agentsDir || rebuild.Glob != "*.md" {
		t.Fatalf("got RebuildDir %+v, want Dir=%q Glob=*.md", rebuild, agentsDir)
	}
	if len(rebuild.Files) != 3 {
		t.Fatalf("got %d agent files, want 3: %+v", len(rebuild.Files), rebuild.Files)
	}

	lead := agentFile(t, rebuild, "lead.md")
	wantLead := "---\n" +
		"name: lead\n" +
		"description: \"lead\"\n" +
		"model: \"opus\"\n" +
		"disallowedTools: Edit, Write, mcp__fs, mcp__gh\n" +
		"---\n" +
		"You are the lead."
	if string(lead.Content) != wantLead {
		t.Errorf("got lead.md:\n%s\nwant:\n%s", lead.Content, wantLead)
	}

	reviewer := agentFile(t, rebuild, "reviewer.md")
	if !strings.Contains(string(reviewer.Content), "disallowedTools: Edit, Write, Agent") {
		t.Errorf("reviewer.md missing expected disallowedTools: %s", reviewer.Content)
	}

	build := agentFile(t, rebuild, "build.md")
	wantBuild := "---\n" +
		"name: build\n" +
		"description: \"build\"\n" +
		"model: \"sonnet\"\n" +
		"maxTurns: 40\n" +
		"---\n" +
		"You build things."
	if string(build.Content) != wantBuild {
		t.Errorf("got build.md:\n%s\nwant:\n%s", build.Content, wantBuild)
	}

	settings := outputByType[render.MergeJSON](t, plan.Outputs)
	if settings.Path != settingsPath {
		t.Fatalf("got settings path %q, want %q", settings.Path, settingsPath)
	}
	if settings.Object["model"] != "sonnet" {
		t.Errorf("got model %v, want sonnet", settings.Object["model"])
	}
	if settings.Object["agent"] != "lead" {
		t.Errorf("got agent %v, want lead", settings.Object["agent"])
	}
	perm, ok := settings.Object["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing or wrong type: %+v", settings.Object)
	}
	ask, _ := perm["ask"].([]string)
	wantAsk := []string{"mcp__fs__write_file", "mcp__gh__create_*"}
	if len(ask) != len(wantAsk) || ask[0] != wantAsk[0] || ask[1] != wantAsk[1] {
		t.Errorf("got ask list %v, want %v", ask, wantAsk)
	}

	mcpMerges := outputsByType[render.MergeJSON](plan.Outputs)
	var claudeJSON render.MergeJSON
	for _, m := range mcpMerges {
		if m.Path == claudeJSONPath {
			claudeJSON = m
		}
	}
	if claudeJSON.Path == "" {
		t.Fatalf("no MergeJSON output for %s", claudeJSONPath)
	}
	if len(claudeJSON.Managed) != 1 || claudeJSON.Managed[0] != "mcpServers" {
		t.Fatalf("got Managed %v, want exactly [mcpServers] (must never touch OAuth/cache state)", claudeJSON.Managed)
	}
	servers, ok := claudeJSON.Object["mcpServers"].(map[string]any)
	if !ok || len(servers) != 2 {
		t.Fatalf("got mcpServers %+v, want 2 entries", claudeJSON.Object["mcpServers"])
	}
	fsEntry := servers["fs"].(map[string]any)
	if fsEntry["type"] != "stdio" || fsEntry["command"] != "mcp-fs" {
		t.Errorf("got fs entry %+v", fsEntry)
	}
	if args, _ := fsEntry["args"].([]any); len(args) != 2 || args[0] != "--root" || args[1] != "/tmp" {
		t.Errorf("got fs args %+v, want [--root /tmp]", fsEntry["args"])
	}
	ghEntry := servers["gh"].(map[string]any)
	if ghEntry["type"] != "http" || ghEntry["url"] != "https://api.github.com/mcp" {
		t.Errorf("got gh entry %+v", ghEntry)
	}

	_ = outputByType[render.RebuildTree](t, plan.Outputs) // commands tree always present
}

// TestRender_PerAgentMCPVisibility confirms an agent that doesn't
// reference an MCP server in its own mcp: list gets that server's tools
// denied wholesale via disallowedTools: "mcp__<server>" — the per-server
// (not per-tool) visibility allowlist Capabilities' CapMCPToolGlobs note
// explains is the only lever available.
func TestRender_PerAgentMCPVisibility(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "sonnet"},
		Agents: []registry.Agent{
			{
				Name:   "scoped",
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: "scoped"},
				MCP:    []registry.AgentMCP{{Server: "fs"}},
			},
		},
		MCPServers: []registry.MCPServer{
			{Name: "fs", Transport: "local", Command: []registry.Value{{Literal: "mcp-fs"}}},
			{Name: "gh", Transport: "remote", URL: registry.Value{Literal: "https://x/mcp"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	scoped := agentFile(t, rebuild, "scoped.md")
	if !strings.Contains(string(scoped.Content), "disallowedTools: mcp__gh") {
		t.Errorf("got scoped.md content %q, want disallowedTools to deny mcp__gh (fs is granted, gh is not)", scoped.Content)
	}
	if strings.Contains(string(scoped.Content), "mcp__fs") {
		t.Errorf("got scoped.md content %q, must not deny mcp__fs (agent's own mcp: grants it)", scoped.Content)
	}
}

// TestRender_OpencodePersonaAgentSkipped confirms a step naming a
// standing Opencode persona (Agent.Opencode != nil) renders nothing for
// claude at all, matching codex/omp's identical treatment of this
// opencode-only construct.
func TestRender_OpencodePersonaAgentSkipped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{
				Name:     "qa",
				Role:     "delegate",
				Prompt:   registry.Prompt{Text: "unused"},
				Opencode: &registry.StepOpencode{Agent: "qa-persona"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Fatalf("got %d agent files, want 0: %+v", len(rebuild.Files), rebuild.Files)
	}
}

// TestRender_TargetsOptOut confirms an agent/server that opts out of
// claude via targets: is excluded entirely.
func TestRender_TargetsOptOut(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "sonnet"},
		Agents: []registry.Agent{
			{Name: "opencode-only", Role: "delegate", Prompt: registry.Prompt{Text: "x"}, Targets: []string{"opencode"}},
		},
		MCPServers: []registry.MCPServer{
			{Name: "opencode-only-server", Transport: "local", Command: []registry.Value{{Literal: "x"}}, Targets: []string{"opencode"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Fatalf("got %d agent files, want 0: %+v", len(rebuild.Files), rebuild.Files)
	}
	for _, m := range outputsByType[render.MergeJSON](plan.Outputs) {
		if m.Path == claudeJSONPath {
			t.Fatalf("got a %s output for a registry with no claude-targeting MCP server", claudeJSONPath)
		}
	}
}

// TestRender_BashAndExternalDirectoryDropSilently confirms bash policy
// and external_directory are never rendered (see Capabilities' shared
// category-precedence doc note), and that both omissions are reported:
// DetectGaps emits a per_agent_bash_policy skip and an
// external_directory_policy gap, matching codex's identical treatment.
func TestRender_BashAndExternalDirectoryDropSilently(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "sonnet"},
		Bash: registry.BashPolicy{
			Profiles: map[string]registry.BashProfile{"global": {Base: registry.Allow}},
		},
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Role:   "primary",
				Class:  "default",
				Prompt: registry.Prompt{Text: "lead"},
				Permissions: registry.Permissions{
					Bash:              registry.BashPermission{Profile: "lead"},
					ExternalDirectory: map[string]registry.Decision{"*": registry.Ask},
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if g, ok := gapForSubjectByCapability(plan.Gaps, render.CapPerAgentBashPolicy); !ok {
		t.Errorf("expected a per_agent_bash_policy gap, got gaps: %+v", plan.Gaps)
	} else if g.Kind != render.GapSkip {
		t.Errorf("got kind %s, want skip", g.Kind)
	}
	if _, ok := gapForSubject(plan.Gaps, "agent:lead.permissions.external_directory"); !ok {
		t.Errorf("expected an external_directory gap for agent:lead.permissions.external_directory, got gaps: %+v", plan.Gaps)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	lead := agentFile(t, rebuild, "lead.md")
	if strings.Contains(string(lead.Content), "bash") || strings.Contains(string(lead.Content), "Bash") {
		t.Errorf("lead.md must not mention bash at all: %s", lead.Content)
	}
}

func gapForSubjectByCapability(gaps []render.Gap, cap render.Capability) (render.Gap, bool) {
	for _, g := range gaps {
		if g.Capability == cap {
			return g, true
		}
	}
	return render.Gap{}, false
}

// TestRender_MCPServerToolsAllowlistGaps confirms an MCPServer.Tools
// allowlist surfaces as a mcp_tool_globs gap (declined — see
// Capabilities' doc for why the complement set is unknowable).
func TestRender_MCPServerToolsAllowlistGaps(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "fs", Transport: "local", Command: []registry.Value{{Literal: "mcp-fs"}}, Tools: []string{"read_file"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if g, ok := gapForSubject(plan.Gaps, "mcp:fs"); !ok || g.Capability != render.CapMCPToolGlobs {
		t.Errorf("expected mcp_tool_globs gap for mcp:fs, got gaps: %+v", plan.Gaps)
	}
}

// TestRender_UnresolvableLocalMCPServerSkippedWithGap and its remote
// sibling cover a resolver failure on each transport, mirroring
// codex/omp/opencode's identical regression coverage.
func TestRender_UnresolvableLocalMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "broken-local", Transport: "local", Command: []registry.Value{{From: "command", Run: []string{"/definitely/does/not/exist/agentcfg-nope"}}}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	g, ok := gapForSubject(plan.Gaps, "mcp:broken-local")
	if !ok {
		t.Fatalf("expected a gap for mcp:broken-local, got gaps: %+v", plan.Gaps)
	}
	if g.Kind != render.GapSkip || g.Capability != render.CapMCPLocalTransport {
		t.Errorf("got kind=%s capability=%s, want skip/mcp_local_transport", g.Kind, g.Capability)
	}
	for _, m := range outputsByType[render.MergeJSON](plan.Outputs) {
		if m.Path == claudeJSONPath {
			t.Fatalf("got a %s output when the only server failed to resolve", claudeJSONPath)
		}
	}
}

func TestRender_UnresolvableRemoteMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "broken-remote", Transport: "remote", URL: registry.Value{From: "file", Path: "/definitely/does/not/exist/agentcfg-nope"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	g, ok := gapForSubject(plan.Gaps, "mcp:broken-remote")
	if !ok {
		t.Fatalf("expected a gap for mcp:broken-remote, got gaps: %+v", plan.Gaps)
	}
	if g.Kind != render.GapSkip || g.Capability != render.CapMCPRemoteTransport {
		t.Errorf("got kind=%s capability=%s, want skip/mcp_remote_transport", g.Kind, g.Capability)
	}
}

// TestRenderProject_FullContentNotThin is the key regression: since
// Claude resolves a subagent name to exactly one file (no field-level
// merge across scopes — see RenderProject's own doc comment), the
// project-scope override must carry the SAME tools/disallowedTools/body
// as the user-scope file, only the model differing.
func TestRenderProject_FullContentNotThin(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{
				Name:        "build",
				Role:        "delegate",
				Class:       "default",
				Description: "builds things",
				Prompt:      registry.Prompt{Text: "You build things."},
				Permissions: registry.Permissions{Edit: "deny"},
			},
			{
				// No class: never gets a project override at all.
				Name:   "no-class",
				Role:   "delegate",
				Prompt: registry.Prompt{Text: "unaffected"},
			},
		},
	}

	classes := map[string]string{"default": "haiku"}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if rebuild.Dir != filepath.Join("/repo", ".claude", "agents") {
		t.Errorf("got Dir %q, want /repo/.claude/agents", rebuild.Dir)
	}
	if len(rebuild.Files) != 1 {
		t.Fatalf("got %d project agent files, want exactly 1 (only class-bearing agents): %+v", len(rebuild.Files), rebuild.Files)
	}

	build := agentFile(t, rebuild, "build.md")
	want := "---\n" +
		"name: build\n" +
		"description: \"builds things\"\n" +
		"model: \"haiku\"\n" +
		"disallowedTools: Edit\n" +
		"---\n" +
		"You build things."
	if string(build.Content) != want {
		t.Errorf("got build.md:\n%s\nwant:\n%s\n(must carry full content, not a thin model-only stub)", build.Content, want)
	}
}

// TestRenderProject_FileBackedPromptReadsRealFile confirms RenderProject
// resolves a file-backed prompt via real disk I/O (it has no injected
// ReadFile — see the doc comment on why), not just the inline-text path.
func TestRenderProject_FileBackedPromptReadsRealFile(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "build.md")
	if err := os.WriteFile(promptPath, []byte("Implement the plan."), 0o600); err != nil {
		t.Fatalf("writing fixture prompt file: %v", err)
	}

	reg := &registry.Registry{
		Agents: []registry.Agent{
			{
				Name:               "build",
				Role:               "delegate",
				Class:              "default",
				Prompt:             registry.Prompt{File: "build.md"},
				ResolvedPromptFile: promptPath,
			},
		},
	}

	plan, err := New().(render.ProjectScopeRenderer).RenderProject(map[string]string{"default": "haiku"}, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}
	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	build := agentFile(t, rebuild, "build.md")
	if !strings.Contains(string(build.Content), "Implement the plan.") {
		t.Errorf("got build.md content %q, want it to contain the real file's contents", build.Content)
	}
}

// TestRenderProject_DefaultModelSettingsOverride confirms a project-scope
// settings.json pins the resolved "default" class.
func TestRenderProject_DefaultModelSettingsOverride(t *testing.T) {
	reg := &registry.Registry{}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(map[string]string{"default": "opus"}, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}
	settings := outputByType[render.MergeJSON](t, plan.Outputs)
	if settings.Path != filepath.Join("/repo", ".claude", "settings.json") {
		t.Errorf("got path %q, want /repo/.claude/settings.json", settings.Path)
	}
	if settings.Object["model"] != "opus" {
		t.Errorf("got model %v, want opus", settings.Object["model"])
	}
	if len(settings.Managed) != 1 || settings.Managed[0] != "model" {
		t.Errorf("got Managed %v, want exactly [model]", settings.Managed)
	}
}
func TestRender_UsesHarnessModelClasses(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "root-default", "big": "root-big"},
		Harnesses: map[string]registry.HarnessConfig{
			"claude": {ModelClasses: map[string]string{"default": "claude-default", "big": "claude-big"}},
		},
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "big", Prompt: registry.Prompt{Text: "Lead."}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	settings := outputByType[render.MergeJSON](t, plan.Outputs)
	if settings.Object["model"] != "claude-default" {
		t.Errorf("got settings model %v, want claude-default", settings.Object["model"])
	}
	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	lead := agentFile(t, rebuild, "lead.md")
	if !strings.Contains(string(lead.Content), "model: \"claude-big\"") {
		t.Errorf("got lead.md:\n%s\nwant harness-specific big model", lead.Content)
	}
}

// TestCapabilities_OnlyDeclaresWhatIsBuilt pins the exact capability set
// so an unintended future addition/removal fails loudly.
func TestCapabilities_OnlyDeclaresWhatIsBuilt(t *testing.T) {
	want := map[render.Capability]bool{
		render.CapAgentDefinitions:           true,
		render.CapPrimaryAgent:               true,
		render.CapPrimaryAgentToolPermission: true,
		render.CapAgentSteps:                 true,
		render.CapAgentTaskPermission:        true,
		render.CapModelLiteralBinding:        true,
		render.CapMCPLocalTransport:          true,
		render.CapMCPRemoteTransport:         true,
		render.CapMCPPerToolAsk:              true,
		render.CapProjectModelPolicy:         true,
		render.CapCustomCommands:             true,
	}
	got := New().Capabilities()
	if len(got) != len(want) {
		t.Fatalf("got %d capabilities, want %d: %v", len(got), len(want), got)
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("unexpected capability declared: %s", c)
		}
	}
}

// TestRender_CommandsUseClaudeOwnSkillsDir confirms commands render under
// Claude's own skills root, not render.CommandsSkillsDir (opencode/omp's
// shared path Claude never reads).
func TestRender_CommandsUseClaudeOwnSkillsDir(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "reviews a diff", Prompt: registry.Prompt{Text: "Review it."}},
		},
	}
	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	tree := outputByType[render.RebuildTree](t, plan.Outputs)
	if tree.Dir != claudeSkillsDir {
		t.Errorf("got commands Dir %q, want %q", tree.Dir, claudeSkillsDir)
	}
	if tree.Dir == render.CommandsSkillsDir {
		t.Fatalf("claude must not share opencode/omp's CommandsSkillsDir")
	}
}
