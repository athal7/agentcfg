package omp

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

func baseBashPolicy() registry.BashPolicy {
	return registry.BashPolicy{
		Lists: map[string]map[string]registry.Decision{
			"safe": {"ls*": registry.Allow},
		},
		DefaultLists: sp([]string{"safe"}),
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Ask},
		},
	}
}

func sp(s []string) *[]string { return &s }

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

// runCommandsByConfigKey indexes every RunCommand output by its `omp
// config set <key>` argument, so a test can assert on a specific command
// without depending on Render's internal ordering — Render now produces
// three (bash.patterns, tools.approvalMode, tools.approval).
func runCommandsByConfigKey(t *testing.T, outputs []render.Output) map[string][]string {
	t.Helper()
	byKey := map[string][]string{}
	for _, o := range outputs {
		cmd, ok := o.(render.RunCommand)
		if !ok || len(cmd.Argv) < 4 || cmd.Argv[0] != "omp" || cmd.Argv[1] != "config" || cmd.Argv[2] != "set" {
			continue
		}
		byKey[cmd.Argv[3]] = cmd.Argv
	}
	return byKey
}

// TestRender_LeadAndBuildProducesSixOutputs covers the happy path: a
// primary + subagent pair, one local mcp server, file-backed prompts,
// plus the two tool-approval sync commands every Render now produces.
func TestRender_LeadAndBuildProducesSixOutputs(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku", "big": "claude-opus-big"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:               "lead",
				Mode:               "primary",
				Class:              "big",
				Prompt:             registry.Prompt{File: "prompts/lead.md"},
				ResolvedPromptFile: "/registry/prompts/lead.md",
			},
			{
				Name:               "build",
				Mode:               "subagent",
				Class:              "default",
				Prompt:             registry.Prompt{File: "prompts/build.md"},
				ResolvedPromptFile: "/registry/prompts/build.md",
				Permissions: registry.Permissions{
					Task:  "allow",
					Write: "allow",
					Edit:  "allow",
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "fs",
				Transport: "local",
				Command:   []registry.Value{{Literal: "mcp-fs"}, {Literal: "--root"}, {Literal: "/tmp"}},
			},
		},
	}

	readFile := fixtureReadFile(map[string]string{
		"/registry/prompts/lead.md":  "You are the lead.",
		"/registry/prompts/build.md": "You build things.",
	})

	plan, err := New().Render(reg, render.Options{ReadFile: readFile})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Outputs) != 6 {
		t.Fatalf("got %d outputs, want 6: %+v", len(plan.Outputs), plan.Outputs)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if rebuild.Dir != "~/.omp/agent/agents" || rebuild.Glob != "*.md" {
		t.Errorf("got RebuildDir{Dir:%q,Glob:%q}, want ~/.omp/agent/agents,*.md", rebuild.Dir, rebuild.Glob)
	}
	if len(rebuild.Files) != 1 {
		t.Fatalf("got %d agent files, want 1 (primary excluded): %+v", len(rebuild.Files), rebuild.Files)
	}
	buildFile := rebuild.Files[0]
	if buildFile.Path != "build.md" {
		t.Errorf("got path %q, want build.md", buildFile.Path)
	}
	wantContent := "---\n" +
		"name: build\n" +
		"description: \n" +
		"tools: read,grep,glob,bash,todo,lsp,web_search,ast_grep,inspect_image,write,edit,ast_edit\n" +
		"spawns: \"*\"\n" +
		"model: \"@default\"\n" +
		"---\n" +
		"You build things."
	if string(buildFile.Content) != wantContent {
		t.Errorf("got content:\n%s\nwant:\n%s", buildFile.Content, wantContent)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	if appendFile.Path != "~/.omp/agent/APPEND_SYSTEM.md" {
		t.Errorf("got path %q, want ~/.omp/agent/APPEND_SYSTEM.md", appendFile.Path)
	}
	if string(appendFile.Content) != "You are the lead." {
		t.Errorf("got content %q, want %q", appendFile.Content, "You are the lead.")
	}

	cmds := runCommandsByConfigKey(t, plan.Outputs)

	wantBashArgv := []string{
		"omp", "config", "set", "bash.patterns",
		`[{"decision":"allow","pattern":"ls*"},{"decision":"prompt","pattern":"*"}]`,
	}
	if got := cmds["bash.patterns"]; !reflect.DeepEqual(got, wantBashArgv) {
		t.Errorf("got bash.patterns argv %v, want %v", got, wantBashArgv)
	}

	wantModeArgv := []string{"omp", "config", "set", "tools.approvalMode", "always-ask"}
	if got := cmds["tools.approvalMode"]; !reflect.DeepEqual(got, wantModeArgv) {
		t.Errorf("got tools.approvalMode argv %v, want %v", got, wantModeArgv)
	}

	// build grants task/write/edit, so all three (plus ast_edit) are
	// allow-listed alongside the always-present ask/eval; "fs" declares no
	// Tools, so it contributes nothing to the MCP side of the map.
	wantApprovalArgv := []string{
		"omp", "config", "set", "tools.approval",
		`{"ask":"allow","ast_edit":"allow","edit":"allow","eval":"allow","task":"allow","write":"allow"}`,
	}
	if got := cmds["tools.approval"]; !reflect.DeepEqual(got, wantApprovalArgv) {
		t.Errorf("got tools.approval argv %v, want %v", got, wantApprovalArgv)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	if mcp.Path != "~/.omp/agent/mcp.json" {
		t.Errorf("got path %q, want ~/.omp/agent/mcp.json", mcp.Path)
	}
	wantMCP := map[string]any{
		"mcpServers": map[string]any{
			"fs": map[string]any{
				"lifecycle": "lazy",
				"command":   []any{"mcp-fs", "--root", "/tmp"},
			},
		},
	}
	if !reflect.DeepEqual(mcp.Object, wantMCP) {
		t.Errorf("got mcp object %#v, want %#v", mcp.Object, wantMCP)
	}
}

// TestRender_GapsForUndeclaredCapabilities covers the gaps omp deliberately
// leaves to DetectGaps: agent_steps, per_agent_bash_policy. mcp_tool_globs
// is asserted absent instead of present — omp now declares
// CapMCPToolAllowlist, a different (exact-name, not glob) way of
// satisfying the same underlying "does something with mcp_servers[].tools"
// check. Fixture (b)-equivalent from the phase 2 task spec
// (external_directory isn't included: the registry schema has no field
// for it — see gaps.go's schema note).
func TestRender_GapsForUndeclaredCapabilities(t *testing.T) {
	steps := 7
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Mode:   "primary",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You are lead."},
			},
			{
				Name:   "build",
				Mode:   "subagent",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You build."},
				Steps:  &steps,
				Permissions: registry.Permissions{
					Bash: registry.BashPermission{Profile: "readonly"},
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "local", Command: []registry.Value{{Literal: "gh-mcp"}}, Tools: []string{"repo_read"}},
		},
	}
	reg.Bash.Profiles["readonly"] = registry.BashProfile{Base: registry.Deny}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	byCapability := map[render.Capability][]render.Gap{}
	for _, g := range plan.Gaps {
		byCapability[g.Capability] = append(byCapability[g.Capability], g)
	}

	if len(byCapability[render.CapAgentSteps]) != 1 {
		t.Errorf("got %d agent_steps gaps, want 1: %+v", len(byCapability[render.CapAgentSteps]), byCapability[render.CapAgentSteps])
	}
	if len(byCapability[render.CapPerAgentBashPolicy]) != 1 {
		t.Errorf("got %d per_agent_bash_policy gaps, want 1: %+v", len(byCapability[render.CapPerAgentBashPolicy]), byCapability[render.CapPerAgentBashPolicy])
	}
	if len(byCapability[render.CapMCPToolGlobs]) != 0 {
		t.Errorf("got %d mcp_tool_globs gaps, want 0 (CapMCPToolAllowlist covers it): %+v", len(byCapability[render.CapMCPToolGlobs]), byCapability[render.CapMCPToolGlobs])
	}
	// primary_agent must NOT gap: omp declares CapPromptAppend, the
	// documented substitute mechanism.
	if len(byCapability[render.CapPrimaryAgent]) != 0 {
		t.Errorf("got %d primary_agent gaps, want 0 (prompt_append substitutes): %+v", len(byCapability[render.CapPrimaryAgent]), byCapability[render.CapPrimaryAgent])
	}
}

// TestRender_UnresolvableMCPServerSkippedWithGap covers a resolver failure
// for a local-transport server (a Value backed by a failing command).
func TestRender_UnresolvableMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken",
				Transport: "local",
				Command:   []registry.Value{{From: "command", Run: []string{"/definitely/does/not/exist/agentcfg-nope"}}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var found bool
	for _, g := range plan.Gaps {
		if g.Capability == render.CapMCPLocalTransport && g.Subject == "mcp:broken" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a mcp_local_transport gap for mcp:broken, got %+v", plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (broken server should be skipped)", servers)
	}
}

// TestRender_AgentMCPGrantsToolsMinusAsk covers renderAgentFile's MCP
// wiring: an agent's mcp: entry grants every one of that server's Tools
// except the ones named in its own Ask list, which are excluded from the
// frontmatter entirely (not merely gated) — omp can't prompt a headless
// subagent, so visibility itself is the only lever.
func TestRender_AgentMCPGrantsToolsMinusAsk(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "scout",
				Mode:   "subagent",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You research."},
				MCP: []registry.AgentMCP{
					{Server: "context7", Ask: []string{"resolve-library-id"}},
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "context7",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://mcp.context7.com/mcp"},
				Tools:     []string{"resolve-library-id", "get-library-docs"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 1 {
		t.Fatalf("got %d agent files, want 1: %+v", len(rebuild.Files), rebuild.Files)
	}
	content := string(rebuild.Files[0].Content)
	if !strings.Contains(content, "mcp__context_get_library_docs") {
		t.Errorf("expected granted (non-asked) tool mcp__context_get_library_docs in frontmatter, got:\n%s", content)
	}
	if strings.Contains(content, "resolve_library_id") || strings.Contains(content, "resolve-library-id") {
		t.Errorf("did not expect asked tool resolve-library-id to appear in frontmatter at all, got:\n%s", content)
	}
}

// TestRenderToolsApprovalCommand_AggregatesAskAcrossAgents covers the
// harness-wide union semantics: two agents granted the same server with
// different ask lists produce ONE ask set (the union), a GapReduction
// naming the server, and mcp__context_resolve_library_id (the special-
// cased "context7" -> "context" prefix) correctly excluded from allow.
func TestRenderToolsApprovalCommand_AggregatesAskAcrossAgents(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "a", MCP: []registry.AgentMCP{{Server: "context7", Ask: []string{"resolve-library-id"}}}},
			{Name: "b", MCP: []registry.AgentMCP{{Server: "context7", Ask: []string{"get-library-docs"}}}},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "context7",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://mcp.context7.com/mcp"},
				Tools:     []string{"resolve-library-id", "get-library-docs", "search"},
			},
		},
	}

	cmd, gaps := renderToolsApprovalCommand(reg)

	var approval map[string]string
	if err := json.Unmarshal([]byte(cmd.Argv[4]), &approval); err != nil {
		t.Fatalf("tools.approval argv[4] is not valid JSON: %v (%s)", err, cmd.Argv[4])
	}
	if _, ok := approval["mcp__context_resolve_library_id"]; ok {
		t.Errorf("resolve-library-id is asked by agent a; should not be allow-listed: %v", approval)
	}
	if _, ok := approval["mcp__context_get_library_docs"]; ok {
		t.Errorf("get-library-docs is asked by agent b; should not be allow-listed: %v", approval)
	}
	if approval["mcp__context_search"] != "allow" {
		t.Errorf("search is asked by neither agent; want allow, got %v", approval)
	}

	var reductions int
	for _, g := range gaps {
		if g.Capability == render.CapMCPPerToolAsk && g.Subject == "mcp:context7" && g.Kind == render.GapReduction {
			reductions++
		}
	}
	if reductions != 1 {
		t.Errorf("got %d mcp_per_tool_ask reduction gaps for mcp:context7, want 1: %+v", reductions, gaps)
	}
}

// TestMCPToolID covers the derivation rule and its one confirmed special
// case: context7 addresses itself internally as "context", not
// "context7" — every other server observed matches the mechanical rule
// (hyphens -> underscores, lowercase, no separator inserted for
// camelCase) with no exceptions.
func TestMCPToolID(t *testing.T) {
	cases := []struct{ server, tool, want string }{
		{"context7", "resolve-library-id", "mcp__context_resolve_library_id"},
		{"firefox-devtools", "accept_dialog", "mcp__firefox_devtools_accept_dialog"},
		{"github", "get_me", "mcp__github_get_me"},
		{"runlayer-atlassian", "addCommentToJiraIssue", "mcp__runlayer_atlassian_addcommenttojiraissue"},
	}
	for _, c := range cases {
		if got := mcpToolID(c.server, c.tool); got != c.want {
			t.Errorf("mcpToolID(%q, %q) = %q, want %q", c.server, c.tool, got, c.want)
		}
	}
}

// TestRender_AgentTargetsRestrictsOutput covers agent.targets: an agent not
// targeting omp is excluded from the rebuilt agent directory, and an mcp
// server not targeting omp is excluded from mcp.json.
func TestRender_AgentTargetsRestrictsOutput(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "vscode-only", Mode: "subagent", Class: "default", Prompt: registry.Prompt{Text: "x"}, Targets: []string{"vscode"}},
			{Name: "everywhere", Mode: "subagent", Class: "default", Prompt: registry.Prompt{Text: "y"}},
		},
		MCPServers: []registry.MCPServer{
			{Name: "vscode-server", Transport: "local", Command: []registry.Value{{Literal: "x"}}, Targets: []string{"vscode"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	var names []string
	for _, f := range rebuild.Files {
		names = append(names, f.Path)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"everywhere.md"}) {
		t.Errorf("got agent files %v, want [everywhere.md]", names)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (vscode-only server excluded)", servers)
	}
}

func TestCapabilities_OnlyDeclaresWhatIsBuilt(t *testing.T) {
	want := map[render.Capability]bool{
		render.CapAgentDefinitions:   true,
		render.CapPromptAppend:       true,
		render.CapPromptFileRef:      true,
		render.CapModelClassBinding:  true,
		render.CapBashOrderedList:    true,
		render.CapGlobalBashPolicy:   true,
		render.CapMCPLocalTransport:  true,
		render.CapMCPToolAllowlist:   true,
		render.CapMCPPerToolAsk:      true,
		render.CapProjectModelPolicy: true,
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

func TestID(t *testing.T) {
	if got := New().ID(); got != "omp" {
		t.Errorf("got ID %q, want omp", got)
	}
}
