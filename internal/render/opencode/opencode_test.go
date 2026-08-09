package opencode

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

func intPtr(n int) *int { return &n }

func baseBashPolicy() registry.BashPolicy {
	return registry.BashPolicy{
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Allow},
		},
	}
}

// TestRender_LeadAndBuildWithOneMCPServer covers the happy path: a primary
// + subagent pair, one remote mcp server the subagent references with an
// ask pattern, and a bare default bash policy. Fixture (a) from the phase 2
// task spec.
func TestRender_LeadAndBuildWithOneMCPServer(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{
			"default": "claude-opus",
			"smol":    "claude-haiku",
			"big":     "claude-opus-big",
		},
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Role:   "primary",
				Class:  "big",
				Prompt: registry.Prompt{Text: "You are lead."},
			},
			{
				Name:  "build",
				Role:  "delegate",
				Class: "default",
				Prompt: registry.Prompt{
					Text: "You build.",
				},
				Permissions: registry.Permissions{Task: "allow"},
				MCP: []registry.AgentMCP{
					{Server: "github", Ask: []string{"create_*"}},
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "github",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://api.github.com"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("got %d outputs, want 2 (config MergeJSON + commands RebuildTree): %+v", len(plan.Outputs), plan.Outputs)
	}

	out, ok := plan.Outputs[0].(render.MergeJSON)
	if !ok {
		t.Fatalf("output is %T, want render.MergeJSON", plan.Outputs[0])
	}
	if out.Path != "~/.config/opencode/opencode.json" {
		t.Errorf("got path %q, want ~/.config/opencode/opencode.json", out.Path)
	}

	// permission must never appear as a bare managed path: per merge.go,
	// a path ending mid-object is a full subtree replace, which would
	// silently drop any permission.* key Render doesn't itself emit (e.g.
	// a hand-set permission.external_directory — see #7). Each leaf
	// Render owns must be its own managed path instead.
	for _, bare := range out.Managed {
		if bare == "permission" {
			t.Fatal("Managed contains bare \"permission\" — this replaces the whole subtree and drops unmanaged keys like external_directory")
		}
	}
	wantPermissionLeaves := []string{
		"permission.bash",
		"permission.edit",
		"permission.read",
		"permission.skill",
		"permission.task",
		"permission.webfetch",
		"permission.write",
	}
	var gotPermissionLeaves []string
	for _, m := range out.Managed {
		if strings.HasPrefix(m, "permission.") {
			gotPermissionLeaves = append(gotPermissionLeaves, m)
		}
	}
	if !reflect.DeepEqual(gotPermissionLeaves, wantPermissionLeaves) {
		t.Errorf("permission managed leaves = %v, want %v", gotPermissionLeaves, wantPermissionLeaves)
	}

	want := map[string]any{
		"model":       "claude-opus",
		"small_model": "claude-haiku",
		"permission": map[string]any{
			"bash":     map[string]any{"*": "allow"},
			"read":     "allow",
			"edit":     "allow",
			"write":    "allow",
			"task":     "allow",
			"skill":    "allow",
			"webfetch": "allow",
		},
		"default_agent": "lead",
		"agent": map[string]any{
			"lead": map[string]any{
				"description": "",
				"mode":        "primary",
				"model":       "claude-opus-big",
				"prompt":      "You are lead.",
				"permission": map[string]any{
					"bash": map[string]any{"*": "allow"},
				},
			},
			"build": map[string]any{
				"description": "",
				"mode":        "subagent",
				"model":       "claude-opus",
				"prompt":      "You build.",
				"permission": map[string]any{
					"bash":            map[string]any{"*": "allow"},
					"task":            "allow",
					"github_create_*": "ask",
				},
				"tools": map[string]any{
					"github_*": true,
				},
			},
		},
		"tools": map[string]any{
			"github_*": false,
		},
		"mcp": map[string]any{
			"github": map[string]any{
				"type": "remote",
				"url":  "https://api.github.com",
			},
		},
	}

	if !reflect.DeepEqual(out.Object, want) {
		t.Errorf("Object mismatch.\ngot:  %#v\nwant: %#v", out.Object, want)
	}
}

// TestRender_UnresolvableMCPServerSkippedWithGap covers a resolver failure:
// an mcp server whose url comes from a nonexistent file is dropped from
// the plan, and a Gap records why. Fixture (b) from the phase 2 task spec
// (adapted: agent_steps/external_directory don't gap for opencode since it
// declares CapAgentSteps and the registry has no external_directory field
// at all — see gaps.go's schema note — so this fixture instead exercises
// the one gap opencode's Render actually produces on its own: an
// unresolvable mcp value).
func TestRender_UnresolvableMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{
			"default": "claude-opus",
			"smol":    "claude-haiku",
		},
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Role:   "primary",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You are lead."},
				Steps:  intPtr(12),
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken",
				Transport: "remote",
				URL: registry.Value{
					From: "file",
					Path: "/definitely/does/not/exist/agentcfg-test-fixture.txt",
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if len(plan.Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(plan.Gaps), plan.Gaps)
	}
	g := plan.Gaps[0]
	if g.Kind != render.GapSkip || g.Capability != render.CapMCPRemoteTransport {
		t.Errorf("got kind=%s capability=%s, want skip/mcp_remote_transport", g.Kind, g.Capability)
	}
	if g.Subject != "mcp:broken" {
		t.Errorf("got subject %q, want mcp:broken", g.Subject)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	mcpObj := out.Object["mcp"].(map[string]any)
	if len(mcpObj) != 0 {
		t.Errorf("got mcp %#v, want empty (broken server should be skipped)", mcpObj)
	}

	agentObj := out.Object["agent"].(map[string]any)["lead"].(map[string]any)
	if agentObj["steps"] != 12 {
		t.Errorf("got steps %v, want 12 (opencode declares agent_steps)", agentObj["steps"])
	}
}

// TestRender_UnresolvableLocalMCPServerSkippedWithGap covers a resolver
// failure for a local-transport server (a Value backed by a failing
// command), the local-transport counterpart to
// TestRender_UnresolvableMCPServerSkippedWithGap above. Regression
// coverage for the point-fix that stopped hardcoding CapMCPLocalTransport
// for every resolver failure regardless of the server's actual transport.
func TestRender_UnresolvableLocalMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{
			"default": "claude-opus",
			"smol":    "claude-haiku",
		},
		Bash: baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken-local",
				Transport: "local",
				Command:   []registry.Value{{From: "command", Run: []string{"/definitely/does/not/exist/agentcfg-nope"}}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if len(plan.Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(plan.Gaps), plan.Gaps)
	}
	g := plan.Gaps[0]
	if g.Kind != render.GapSkip || g.Capability != render.CapMCPLocalTransport {
		t.Errorf("got kind=%s capability=%s, want skip/mcp_local_transport", g.Kind, g.Capability)
	}
	if g.Subject != "mcp:broken-local" {
		t.Errorf("got subject %q, want mcp:broken-local", g.Subject)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	mcpObj := out.Object["mcp"].(map[string]any)
	if len(mcpObj) != 0 {
		t.Errorf("got mcp %#v, want empty (broken server should be skipped)", mcpObj)
	}
}

// TestRender_MixedLocalAndRemoteMCPServers covers a registry that
// registers one local-transport and one remote-transport server side by
// side, both resolvable — the two transports must coexist in one mcp
// block without either one clobbering or gapping the other.
func TestRender_MixedLocalAndRemoteMCPServers(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{
			"default": "claude-opus",
			"smol":    "claude-haiku",
		},
		Bash: baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{Name: "local-one", Transport: "local", Command: []registry.Value{{Literal: "gh-mcp"}}},
			{Name: "remote-one", Transport: "remote", URL: registry.Value{Literal: "https://api.example.com/mcp"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	mcpObj := out.Object["mcp"].(map[string]any)
	if len(mcpObj) != 2 {
		t.Fatalf("got %d mcp entries, want 2: %#v", len(mcpObj), mcpObj)
	}
	local := mcpObj["local-one"].(map[string]any)
	if local["type"] != "local" {
		t.Errorf("got local-one type %v, want local", local["type"])
	}
	if _, hasURL := local["url"]; hasURL {
		t.Errorf("got local-one %#v, want no url key", local)
	}
	wantCommand := []any{"gh-mcp"}
	if !reflect.DeepEqual(local["command"], wantCommand) {
		t.Errorf("got local-one command %#v, want %#v", local["command"], wantCommand)
	}
	remote := mcpObj["remote-one"].(map[string]any)
	if remote["type"] != "remote" {
		t.Errorf("got remote-one type %v, want remote", remote["type"])
	}
	if remote["url"] != "https://api.example.com/mcp" {
		t.Errorf("got remote-one url %#v, want https://api.example.com/mcp", remote["url"])
	}
	if _, hasCommand := remote["command"]; hasCommand {
		t.Errorf("got remote-one %#v, want no command key", remote)
	}
}

// TestRender_NoPrimaryAgentOmitsDefaultAgent covers a registry with zero
// mode:primary agents (valid per registry.Validate — only >1 is an error).
// Fixture (c) from the phase 2 task spec.
func TestRender_NoPrimaryAgentOmitsDefaultAgent(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{
			"default": "claude-opus",
			"smol":    "claude-haiku",
		},
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "build",
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You build."},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	if _, ok := out.Object["default_agent"]; ok {
		t.Errorf("got default_agent key %q, want it absent when there's no primary agent", out.Object["default_agent"])
	}
}

// TestRender_PrimaryAgentEditWriteNoGap covers issue #9's opencode side of
// the comparison: unlike omp, opencode's default_agent still gets a full
// agent.<name>.permission block (renderAgent applies to every agent
// uniformly), so a primary agent's permissions.edit/write is fully
// expressed and must never gap.
func TestRender_PrimaryAgentEditWriteNoGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Role:   "primary",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You are lead."},
				Permissions: registry.Permissions{
					Edit:  "deny",
					Write: "deny",
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0 (opencode declares primary_agent_tool_permission): %+v", len(plan.Gaps), plan.Gaps)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	leadPerm := out.Object["agent"].(map[string]any)["lead"].(map[string]any)["permission"].(map[string]any)
	if leadPerm["edit"] != "deny" || leadPerm["write"] != "deny" {
		t.Errorf("got lead permission %#v, want edit=deny write=deny", leadPerm)
	}
}

func TestCapabilities_ExcludesUndeclaredOnes(t *testing.T) {
	excluded := map[render.Capability]bool{
		render.CapBashOrderedList:   true,
		render.CapModelClassBinding: true,
		render.CapPromptAppend:      true,
	}
	for _, c := range New().Capabilities() {
		if excluded[c] {
			t.Errorf("opencode should not declare %s", c)
		}
	}
}

func TestRenderProject_PinsModelsAndClassBearingAgents(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "big"},
			{Name: "build", Role: "delegate", Class: "default"},
			{Name: "no-class", Role: "delegate"}, // Class unset — must be skipped
		},
	}
	classes := map[string]string{
		"default": "claude-opus-context-override",
		"smol":    "claude-haiku",
		"big":     "claude-opus-big",
	}

	pr, ok := New().(render.ProjectScopeRenderer)
	if !ok {
		t.Fatalf("opencode renderer does not implement render.ProjectScopeRenderer")
	}
	plan, err := pr.RenderProject(classes, reg, "/repo/checkout")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1: %+v", len(plan.Outputs), plan.Outputs)
	}

	out, ok := plan.Outputs[0].(render.MergeJSON)
	if !ok {
		t.Fatalf("output is %T, want render.MergeJSON", plan.Outputs[0])
	}
	wantPath := filepath.Join("/repo/checkout", ".opencode", "opencode.json")
	if out.Path != wantPath {
		t.Errorf("got path %q, want %q", out.Path, wantPath)
	}
	wantManaged := []string{"model", "small_model", "agent.*.model"}
	if !reflect.DeepEqual(out.Managed, wantManaged) {
		t.Errorf("got managed %v, want %v", out.Managed, wantManaged)
	}

	want := map[string]any{
		"model":       "claude-opus-context-override",
		"small_model": "claude-haiku",
		"agent": map[string]any{
			"lead":  map[string]any{"model": "claude-opus-big"},
			"build": map[string]any{"model": "claude-opus-context-override"},
		},
	}
	if !reflect.DeepEqual(out.Object, want) {
		t.Errorf("Object mismatch.\ngot:  %#v\nwant: %#v", out.Object, want)
	}
}

func TestRenderProject_UnknownClassSkipsAgentDefensively(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Class: "not-in-classes-map"},
		},
	}
	classes := map[string]string{"default": "claude-opus", "smol": "claude-haiku"}

	r := New()
	pr, ok := r.(render.ProjectScopeRenderer)
	if !ok {
		t.Fatalf("opencode renderer does not implement render.ProjectScopeRenderer")
	}
	plan, err := pr.RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	agentObj := out.Object["agent"].(map[string]any)
	if len(agentObj) != 0 {
		t.Errorf("got agent %#v, want empty (unknown class skipped)", agentObj)
	}
}

func TestCapabilities_DeclaresProjectModelPolicy(t *testing.T) {
	found := false
	for _, c := range New().Capabilities() {
		if c == render.CapProjectModelPolicy {
			found = true
		}
	}
	if !found {
		t.Errorf("opencode should declare CapProjectModelPolicy")
	}
}

func TestCapabilities_DeclaresCustomCommands(t *testing.T) {
	found := false
	for _, c := range New().Capabilities() {
		if c == render.CapCustomCommands {
			found = true
		}
	}
	if !found {
		t.Errorf("opencode should declare CapCustomCommands")
	}
}

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
		t.Fatalf("got %d gaps, want 0 (opencode declares CapCustomCommands): %+v", len(plan.Gaps), plan.Gaps)
	}

	tree, ok := plan.Outputs[1].(render.RebuildTree)
	if !ok {
		t.Fatalf("Outputs[1] is %T, want render.RebuildTree", plan.Outputs[1])
	}
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

func TestID(t *testing.T) {
	if got := New().ID(); got != "opencode" {
		t.Errorf("got ID %q, want opencode", got)
	}
}

// TestRender_HarnessExtraMergesIntoOpencodeJSON covers harnesses.opencode.
// extra fully owning opencode.json's static, non-registry-modeled keys
// (server, plugin, provider, formatter, lsp) alongside the permission
// leaves this renderer doesn't otherwise manage (grep/glob/lsp/websearch),
// without disturbing the permission leaves it does (bash).
func TestRender_HarnessExtraMergesIntoOpencodeJSON(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Harnesses: map[string]registry.HarnessConfig{
			"opencode": {
				Extra: map[string]any{
					"server":                     map[string]any{"hostname": "127.0.0.1"},
					"permission.grep":            "allow",
					"permission.glob":            "allow",
					"permission.lsp":             "allow",
					"permission.webfetch_unused": "allow",
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	server, ok := out.Object["server"].(map[string]any)
	if !ok || server["hostname"] != "127.0.0.1" {
		t.Errorf("got server %#v, want hostname 127.0.0.1", out.Object["server"])
	}

	perm := out.Object["permission"].(map[string]any)
	if perm["grep"] != "allow" || perm["glob"] != "allow" || perm["lsp"] != "allow" {
		t.Errorf("got permission %#v, want grep/glob/lsp allow from extra", perm)
	}
	if _, ok := perm["bash"]; !ok {
		t.Errorf("got permission %#v, want Render's own bash leaf to survive alongside extra's leaves", perm)
	}

	for _, want := range []string{"server", "permission.grep", "permission.glob", "permission.lsp", "permission.webfetch_unused"} {
		found := false
		for _, m := range out.Managed {
			if m == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Managed %v missing extra path %q", out.Managed, want)
		}
	}
}

// TestRender_HarnessExtraRejectsReservedTopLevelKey covers the collision
// guard: an extra key naming a top-level key Render always manages itself
// (e.g. "agent") must fail loudly rather than let the two writers race.
func TestRender_HarnessExtraRejectsReservedTopLevelKey(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Harnesses: map[string]registry.HarnessConfig{
			"opencode": {Extra: map[string]any{"agent": map[string]any{}}},
		},
	}

	_, err := New().Render(reg, render.Options{})
	if err == nil {
		t.Fatal("expected an error for an extra key colliding with a Render-managed key, got nil")
	}
}

// TestRender_HarnessExtraRejectsReservedPermissionLeaf covers the
// finer-grained collision guard on "permission.<leaf>" paths: a leaf
// Render already manages (e.g. "bash") must be rejected the same way a
// top-level collision is.
func TestRender_HarnessExtraRejectsReservedPermissionLeaf(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Harnesses: map[string]registry.HarnessConfig{
			"opencode": {Extra: map[string]any{"permission.bash": map[string]any{}}},
		},
	}

	_, err := New().Render(reg, render.Options{})
	if err == nil {
		t.Fatal("expected an error for extra permission.bash colliding with Render's own leaf, got nil")
	}
}

// TestRender_HarnessExtraRejectsNestedPermissionPath covers a validation
// bypass: "permission.read.foo" must not slip past the reserved-leaf
// check by having its leaf computed as "read.foo" (which doesn't match
// the reserved "read" entry) — nested permission paths are rejected
// outright, since setDottedPath would otherwise replace Render's
// permission.read scalar with an object.
func TestRender_HarnessExtraRejectsNestedPermissionPath(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Harnesses: map[string]registry.HarnessConfig{
			"opencode": {Extra: map[string]any{"permission.read.foo": "allow"}},
		},
	}

	_, err := New().Render(reg, render.Options{})
	if err == nil {
		t.Fatal("expected an error for a nested permission.read.foo extra key, got nil")
	}
}

// TestRender_HarnessExtraRejectsEmptyPathSegments covers malformed dotted
// keys ("", ".tools", "tools.", "tools..foo") that would otherwise let
// setDottedPath write empty-string object keys into opencode.json instead
// of failing loudly on invalid harness configuration.
func TestRender_HarnessExtraRejectsEmptyPathSegments(t *testing.T) {
	for _, key := range []string{"", ".tools", "tools.", "tools..foo"} {
		t.Run(key, func(t *testing.T) {
			reg := &registry.Registry{
				ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
				Bash:         baseBashPolicy(),
				Harnesses: map[string]registry.HarnessConfig{
					"opencode": {Extra: map[string]any{key: "x"}},
				},
			}
			if _, err := New().Render(reg, render.Options{}); err == nil {
				t.Fatalf("expected an error for extra key %q, got nil", key)
			}
		})
	}
}

// TestRender_OpencodePersonaOverridesPrimaryStep covers a workflow step
// (role: primary) that names a standing OpencodeAgent via Opencode.Agent:
// opencode must render the referenced persona keyed by the PERSONA's own
// name, using the persona's own prompt/permissions/description/model —
// never the step's own copies of those fields — while default_agent
// still resolves to the persona name so opencode's entry point actually
// exists in the agent map.
func TestRender_OpencodePersonaOverridesPrimaryStep(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{
			"default": "claude-opus",
			"smol":    "claude-haiku",
			"big":     "claude-opus-big",
		},
		Bash: baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:        "orchestrate",
				Description: "step description",
				Role:        "primary",
				Class:       "default",
				Prompt:      registry.Prompt{Text: "step prompt, must not appear"},
				Permissions: registry.Permissions{Edit: "allow", Write: "allow"},
				Opencode:    &registry.StepOpencode{Agent: "lead"},
			},
		},
		OpencodeAgents: []registry.OpencodeAgent{
			{
				Name:        "lead",
				Description: "lead persona description",
				Class:       "big",
				Prompt:      registry.Prompt{Text: "lead persona prompt"},
				Permissions: registry.Permissions{Edit: "deny", Write: "deny"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	agentObj := out.Object["agent"].(map[string]any)

	if _, ok := agentObj["orchestrate"]; ok {
		t.Errorf("agent map has key %q for the overriding step; want it absent", "orchestrate")
	}
	lead, ok := agentObj["lead"].(map[string]any)
	if !ok {
		t.Fatalf("agent map missing key %q, got %#v", "lead", agentObj)
	}

	if lead["prompt"] != "lead persona prompt" {
		t.Errorf("got prompt %v, want the lead persona's own prompt", lead["prompt"])
	}
	if lead["description"] != "lead persona description" {
		t.Errorf("got description %v, want the lead persona's own description", lead["description"])
	}
	if lead["model"] != "claude-opus-big" {
		t.Errorf("got model %v, want claude-opus-big (lead persona's class)", lead["model"])
	}
	perm := lead["permission"].(map[string]any)
	if perm["edit"] != "deny" || perm["write"] != "deny" {
		t.Errorf("got permission %#v, want edit=deny write=deny (lead persona's own permissions)", perm)
	}
	if lead["mode"] != "primary" {
		t.Errorf("got mode %v, want primary (from the step's own role)", lead["mode"])
	}

	if out.Object["default_agent"] != "lead" {
		t.Errorf("got default_agent %v, want %q", out.Object["default_agent"], "lead")
	}
}

// TestRender_OpencodePersonaSharedAcrossStepsRendersOnce covers two
// workflow steps that both reference the same OpencodeAgent persona:
// opencode must render it exactly once, and the rendered entry must
// still carry the persona's own fields rather than either step's.
func TestRender_OpencodePersonaSharedAcrossStepsRendersOnce(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:     "verify",
				Role:     "delegate",
				Class:    "default",
				Prompt:   registry.Prompt{Text: "verify step prompt"},
				Opencode: &registry.StepOpencode{Agent: "qa"},
			},
			{
				Name:     "research",
				Role:     "delegate",
				Class:    "default",
				Prompt:   registry.Prompt{Text: "research step prompt"},
				Opencode: &registry.StepOpencode{Agent: "qa"},
			},
		},
		OpencodeAgents: []registry.OpencodeAgent{
			{
				Name:        "qa",
				Description: "qa persona",
				Class:       "default",
				Prompt:      registry.Prompt{Text: "qa persona prompt"},
				Permissions: registry.Permissions{Task: "allow"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	agentObj := out.Object["agent"].(map[string]any)

	if len(agentObj) != 1 {
		t.Fatalf("got %d agent entries, want exactly 1 (deduped qa): %#v", len(agentObj), agentObj)
	}
	qa, ok := agentObj["qa"].(map[string]any)
	if !ok {
		t.Fatalf("agent map missing key %q, got %#v", "qa", agentObj)
	}
	if qa["prompt"] != "qa persona prompt" {
		t.Errorf("got prompt %v, want the qa persona's own prompt", qa["prompt"])
	}
	perm := qa["permission"].(map[string]any)
	if perm["task"] != "allow" {
		t.Errorf("got permission %#v, want task=allow (qa persona's own permissions)", perm)
	}
}

// TestRender_StepWithoutOpencodeOverrideUnchanged is a non-regression
// check: a workflow step with Opencode left nil must render exactly as
// it did before this override existed — from its own fields, keyed by
// its own name.
func TestRender_StepWithoutOpencodeOverrideUnchanged(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:        "build",
				Description: "builds things",
				Role:        "delegate",
				Class:       "default",
				Prompt:      registry.Prompt{Text: "You build."},
				Permissions: registry.Permissions{Task: "allow"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	out := plan.Outputs[0].(render.MergeJSON)
	agentObj := out.Object["agent"].(map[string]any)

	build, ok := agentObj["build"].(map[string]any)
	if !ok {
		t.Fatalf("agent map missing key %q, got %#v", "build", agentObj)
	}
	if build["prompt"] != "You build." {
		t.Errorf("got prompt %v, want %q", build["prompt"], "You build.")
	}
	if build["description"] != "builds things" {
		t.Errorf("got description %v, want %q", build["description"], "builds things")
	}
	if build["mode"] != "subagent" {
		t.Errorf("got mode %v, want subagent", build["mode"])
	}
	perm := build["permission"].(map[string]any)
	if perm["task"] != "allow" {
		t.Errorf("got permission %#v, want task=allow", perm)
	}
}
