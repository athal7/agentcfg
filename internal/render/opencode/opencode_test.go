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
				Mode:   "primary",
				Class:  "big",
				Prompt: registry.Prompt{Text: "You are lead."},
			},
			{
				Name:  "build",
				Mode:  "subagent",
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
	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1: %+v", len(plan.Outputs), plan.Outputs)
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
				Mode:   "primary",
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
				Mode:   "subagent",
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
			{Name: "lead", Mode: "primary", Class: "big"},
			{Name: "build", Mode: "subagent", Class: "default"},
			{Name: "no-class", Mode: "subagent"}, // Class unset — must be skipped
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
			{Name: "build", Mode: "subagent", Class: "not-in-classes-map"},
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

func TestID(t *testing.T) {
	if got := New().ID(); got != "opencode" {
		t.Errorf("got ID %q, want opencode", got)
	}
}
