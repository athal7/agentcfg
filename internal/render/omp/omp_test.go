package omp

import (
	"reflect"
	"sort"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

func baseBashPolicy() registry.BashPolicy {
	return registry.BashPolicy{
		Lists: map[string]map[string]registry.Decision{
			"safe": {"ls*": registry.Allow},
		},
		DefaultLists: []string{"safe"},
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Ask},
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

// TestRender_LeadAndBuildProducesFourOutputs covers the happy path: a
// primary + subagent pair, one local mcp server, file-backed prompts.
func TestRender_LeadAndBuildProducesFourOutputs(t *testing.T) {
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
	if len(plan.Outputs) != 4 {
		t.Fatalf("got %d outputs, want 4: %+v", len(plan.Outputs), plan.Outputs)
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

	cmd := outputByType[render.RunCommand](t, plan.Outputs)
	wantArgv := []string{
		"omp", "config", "set", "bash.patterns",
		`[{"decision":"allow","pattern":"ls*"},{"decision":"prompt","pattern":"*"}]`,
	}
	if !reflect.DeepEqual(cmd.Argv, wantArgv) {
		t.Errorf("got argv %v, want %v", cmd.Argv, wantArgv)
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
// leaves to DetectGaps: agent_steps, per_agent_bash_policy, mcp_tool_globs.
// Fixture (b)-equivalent from the phase 2 task spec (external_directory
// isn't included: the registry schema has no field for it — see
// gaps.go's schema note).
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
			{Name: "github", Transport: "local", Command: []registry.Value{{Literal: "gh-mcp"}}},
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
	if len(byCapability[render.CapMCPToolGlobs]) != 1 {
		t.Errorf("got %d mcp_tool_globs gaps, want 1: %+v", len(byCapability[render.CapMCPToolGlobs]), byCapability[render.CapMCPToolGlobs])
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
