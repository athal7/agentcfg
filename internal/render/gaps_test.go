package render

import (
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
)

func intPtr(n int) *int { return &n }

func TestDetectGaps_AgentStepsDropped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Steps: intPtr(5)},
		},
	}

	gaps := DetectGaps(reg, nil)

	if len(gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(gaps), gaps)
	}
	g := gaps[0]
	if g.Kind != GapSkip || g.Capability != CapAgentSteps {
		t.Errorf("got kind=%s capability=%s, want skip/agent_steps", g.Kind, g.Capability)
	}
	if g.Subject != "agent:build.steps" {
		t.Errorf("got subject %q, want agent:build.steps", g.Subject)
	}
}

func TestDetectGaps_AgentStepsSuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Steps: intPtr(5)},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapAgentSteps})

	if len(gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(gaps), gaps)
	}
}

func TestDetectGaps_NoStepsNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, nil)

	if len(gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(gaps), gaps)
	}
}

func TestDetectGaps_PerAgentBashProfileDropped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Permissions: registry.Permissions{
				Bash: registry.BashPermission{Profile: "readonly"},
			}},
			{Name: "build", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapPrimaryAgent})

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapPerAgentBashPolicy {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a per_agent_bash_policy gap, got %+v", gaps)
	}
	if found.Kind != GapSkip {
		t.Errorf("got kind %s, want skip", found.Kind)
	}
	if found.Subject != "lead" {
		t.Errorf("got subject %q, want %q", found.Subject, "lead")
	}
}

func TestDetectGaps_GlobalBashProfileIsNotPerAgent(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Permissions: registry.Permissions{
				Bash: registry.BashPermission{Profile: "global"},
			}},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapPrimaryAgent})

	for _, g := range gaps {
		if g.Capability == CapPerAgentBashPolicy {
			t.Fatalf("did not expect per_agent_bash_policy gap for profile:global, got %+v", g)
		}
	}
}

func TestDetectGaps_PerAgentBashPolicySuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Permissions: registry.Permissions{
				Bash: registry.BashPermission{Profile: "readonly"},
			}},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapPrimaryAgent, CapPerAgentBashPolicy})

	for _, g := range gaps {
		if g.Capability == CapPerAgentBashPolicy {
			t.Fatalf("did not expect per_agent_bash_policy gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_PrimaryAgentReductionWhenNeitherCapDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{Name: "build", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapPrimaryAgent {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a primary_agent gap, got %+v", gaps)
	}
	if found.Kind != GapReduction {
		t.Errorf("got kind %s, want reduction", found.Kind)
	}
	if found.Subject != "agent:lead" {
		t.Errorf("got subject %q, want agent:lead", found.Subject)
	}
}

func TestDetectGaps_PrimaryAgentSuppressedByPromptAppend(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapPromptAppend})

	for _, g := range gaps {
		if g.Capability == CapPrimaryAgent {
			t.Fatalf("expected prompt_append to suppress the primary_agent gap, got %+v", g)
		}
	}
}

func TestDetectGaps_PrimaryAgentSuppressedWhenDeclaredDirectly(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapPrimaryAgent})

	for _, g := range gaps {
		if g.Capability == CapPrimaryAgent {
			t.Fatalf("did not expect primary_agent gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoPrimaryAgentNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapPrimaryAgent {
			t.Fatalf("did not expect primary_agent gap when no primary agent exists, got %+v", g)
		}
	}
}

func TestDetectGaps_PrimaryAgentToolPermissionDropped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Permissions: registry.Permissions{
				Edit: "deny", Write: "deny",
			}},
			{Name: "build", Role: "delegate", Permissions: registry.Permissions{
				Edit: "allow", Write: "allow",
			}},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapPrimaryAgentToolPermission {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a primary_agent_tool_permission gap, got %+v", gaps)
	}
	if found.Kind != GapSkip {
		t.Errorf("got kind %s, want skip", found.Kind)
	}
	if found.Subject != "agent:lead.permissions" {
		t.Errorf("got subject %q, want agent:lead.permissions", found.Subject)
	}
}

func TestDetectGaps_PrimaryAgentToolPermissionIgnoresSubagentEditWrite(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{Name: "build", Role: "delegate", Permissions: registry.Permissions{
				Edit: "allow", Write: "allow",
			}},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapPrimaryAgentToolPermission {
			t.Fatalf("did not expect primary_agent_tool_permission gap from a subagent's edit/write, got %+v", g)
		}
	}
}

func TestDetectGaps_PrimaryAgentToolPermissionSuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Permissions: registry.Permissions{
				Edit: "deny", Write: "deny",
			}},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapPrimaryAgentToolPermission})

	for _, g := range gaps {
		if g.Capability == CapPrimaryAgentToolPermission {
			t.Fatalf("did not expect primary_agent_tool_permission gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoPrimaryAgentEditWriteNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapPrimaryAgentToolPermission {
			t.Fatalf("did not expect primary_agent_tool_permission gap when primary agent's edit/write are unset, got %+v", g)
		}
	}
}

func TestDetectGaps_ExternalDirectoryDropped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Permissions: registry.Permissions{
				ExternalDirectory: map[string]registry.Decision{"*": registry.Ask},
			}},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapExternalDirectory {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected an external_directory_policy gap, got %+v", gaps)
	}
	if found.Kind != GapSkip {
		t.Errorf("got kind %s, want skip", found.Kind)
	}
	if found.Subject != "agent:build.permissions.external_directory" {
		t.Errorf("got subject %q, want agent:build.permissions.external_directory", found.Subject)
	}
}

func TestDetectGaps_ExternalDirectorySuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Permissions: registry.Permissions{
				ExternalDirectory: map[string]registry.Decision{"*": registry.Ask},
			}},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapExternalDirectory})

	for _, g := range gaps {
		if g.Capability == CapExternalDirectory {
			t.Fatalf("did not expect external_directory_policy gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoExternalDirectoryNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapExternalDirectory {
			t.Fatalf("did not expect external_directory_policy gap when unset, got %+v", g)
		}
	}
}

func TestDetectGaps_MCPToolGlobsPerServer(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "remote", Tools: []string{"repo_read", "repo_create"}},
			{Name: "linear", Transport: "remote", Tools: []string{"issue_search"}},
		},
	}

	gaps := DetectGaps(reg, nil)

	var subjects []string
	for _, g := range gaps {
		if g.Capability == CapMCPToolGlobs {
			subjects = append(subjects, g.Subject)
		}
	}
	if len(subjects) != 2 {
		t.Fatalf("got %d mcp_tool_globs gaps, want 2: %v", len(subjects), subjects)
	}
	if subjects[0] != "mcp:github" || subjects[1] != "mcp:linear" {
		t.Errorf("got subjects %v, want [mcp:github mcp:linear]", subjects)
	}
}

func TestDetectGaps_MCPToolGlobsSuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "remote"},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapMCPToolGlobs})

	for _, g := range gaps {
		if g.Capability == CapMCPToolGlobs {
			t.Fatalf("did not expect mcp_tool_globs gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_MCPToolGlobsGapWhenServerHasToolsAllowlist(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "slack", Transport: "remote", Tools: []string{"slack_search", "slack_send_message"}},
			{Name: "github", Transport: "remote"},
		},
	}

	gaps := DetectGaps(reg, nil)

	var subjects []string
	for _, g := range gaps {
		if g.Capability == CapMCPToolGlobs {
			subjects = append(subjects, g.Subject)
		}
	}
	if len(subjects) != 1 {
		t.Fatalf("got %d mcp_tool_globs gaps, want 1 (only the server with an allowlist): %v", len(subjects), subjects)
	}
	if subjects[0] != "mcp:slack" {
		t.Errorf("got subject %q, want mcp:slack", subjects[0])
	}
}

func TestDetectGaps_MCPToolGlobsNoGapWhenNoServerHasToolsAllowlist(t *testing.T) {
	reg := &registry.Registry{
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "remote"},
			{Name: "linear", Transport: "remote"},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapMCPToolGlobs {
			t.Fatalf("did not expect mcp_tool_globs gap when no server has a tools allowlist, got %+v", g)
		}
	}
}

func TestDetectGaps_NoMCPServersNoGap(t *testing.T) {
	reg := &registry.Registry{}

	gaps := DetectGaps(reg, nil)

	if len(gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(gaps), gaps)
	}
}

func TestDetectGaps_AgentTaskPermissionDropped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Permissions: registry.Permissions{Task: "deny"}},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapAgentTaskPermission {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected an agent_task_permission gap, got %+v", gaps)
	}
	if found.Kind != GapSkip {
		t.Errorf("got kind %s, want skip", found.Kind)
	}
	if found.Subject != "agent:build.permissions.task" {
		t.Errorf("got subject %q, want agent:build.permissions.task", found.Subject)
	}
}

func TestDetectGaps_AgentTaskPermissionSuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", Permissions: registry.Permissions{Task: "deny"}},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapAgentTaskPermission})

	for _, g := range gaps {
		if g.Capability == CapAgentTaskPermission {
			t.Fatalf("did not expect agent_task_permission gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoAgentTaskPermissionNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapAgentTaskPermission {
			t.Fatalf("did not expect agent_task_permission gap when unset, got %+v", g)
		}
	}
}

func TestDetectGaps_MCPPerToolAskDropped(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", MCP: []registry.AgentMCP{
				{Server: "github", Ask: []string{"create_*"}},
			}},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapMCPPerToolAsk {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected an mcp_per_tool_ask gap, got %+v", gaps)
	}
	if found.Kind != GapSkip {
		t.Errorf("got kind %s, want skip", found.Kind)
	}
	if found.Subject != "agent:build.mcp:github" {
		t.Errorf("got subject %q, want agent:build.mcp:github", found.Subject)
	}
}

func TestDetectGaps_MCPPerToolAskSuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", MCP: []registry.AgentMCP{
				{Server: "github", Ask: []string{"create_*"}},
			}},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapMCPPerToolAsk})

	for _, g := range gaps {
		if g.Capability == CapMCPPerToolAsk {
			t.Fatalf("did not expect mcp_per_tool_ask gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoMCPPerToolAskNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", MCP: []registry.AgentMCP{
				{Server: "github"},
			}},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapMCPPerToolAsk {
			t.Fatalf("did not expect mcp_per_tool_ask gap when no ask patterns set, got %+v", g)
		}
	}
}


func TestDetectGaps_MCPToolGlobsAndPerToolAskCombineIndependently(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "build", Role: "delegate", MCP: []registry.AgentMCP{
				{Server: "github", Ask: []string{"create_*"}},
			}},
		},
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "remote", Tools: []string{"repo_read", "create_issue"}},
		},
	}

	gaps := DetectGaps(reg, nil)

	var toolGlobs, perToolAsk []Gap
	for _, g := range gaps {
		switch g.Capability {
		case CapMCPToolGlobs:
			toolGlobs = append(toolGlobs, g)
		case CapMCPPerToolAsk:
			perToolAsk = append(perToolAsk, g)
		}
	}
	if len(toolGlobs) != 1 {
		t.Fatalf("got %d mcp_tool_globs gaps, want 1: %+v", len(toolGlobs), toolGlobs)
	}
	if toolGlobs[0].Subject != "mcp:github" {
		t.Errorf("got mcp_tool_globs subject %q, want mcp:github", toolGlobs[0].Subject)
	}
	if len(perToolAsk) != 1 {
		t.Fatalf("got %d mcp_per_tool_ask gaps, want 1: %+v", len(perToolAsk), perToolAsk)
	}
	if perToolAsk[0].Subject != "agent:build.mcp:github" {
		t.Errorf("got mcp_per_tool_ask subject %q, want agent:build.mcp:github", perToolAsk[0].Subject)
	}

	// Declaring one of the two must not suppress the other: they're
	// independent capabilities even though they share a server. Check
	// both directions.
	gapsToolGlobsDeclared := DetectGaps(reg, []Capability{CapMCPToolGlobs})
	var stillAsk bool
	for _, g := range gapsToolGlobsDeclared {
		if g.Capability == CapMCPToolGlobs {
			t.Fatalf("did not expect mcp_tool_globs gap when declared, got %+v", g)
		}
		if g.Capability == CapMCPPerToolAsk {
			stillAsk = true
		}
	}
	if !stillAsk {
		t.Fatalf("expected mcp_per_tool_ask gap to survive declaring only mcp_tool_globs, got %+v", gapsToolGlobsDeclared)
	}

	gapsAskDeclared := DetectGaps(reg, []Capability{CapMCPPerToolAsk})
	var stillToolGlobs bool
	for _, g := range gapsAskDeclared {
		if g.Capability == CapMCPPerToolAsk {
			t.Fatalf("did not expect mcp_per_tool_ask gap when declared, got %+v", g)
		}
		if g.Capability == CapMCPToolGlobs {
			stillToolGlobs = true
		}
	}
	if !stillToolGlobs {
		t.Fatalf("expected mcp_tool_globs gap to survive declaring only mcp_per_tool_ask, got %+v", gapsAskDeclared)
	}
}

func TestDetectGaps_ComposeIntoPrimaryReductionWhenUndeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{Name: "plan", Role: "advisory"},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found *Gap
	for i := range gaps {
		if gaps[i].Capability == CapComposeIntoPrimary {
			found = &gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a compose_into_primary gap, got %+v", gaps)
	}
	if found.Kind != GapReduction {
		t.Errorf("got kind %s, want reduction (content survives as a standalone agent)", found.Kind)
	}
	if found.Subject != "agent:plan" {
		t.Errorf("got subject %q, want agent:plan", found.Subject)
	}
}

func TestDetectGaps_ComposeIntoPrimarySuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{Name: "plan", Role: "advisory"},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapComposeIntoPrimary})

	for _, g := range gaps {
		if g.Capability == CapComposeIntoPrimary {
			t.Fatalf("did not expect compose_into_primary gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoComposeIntoPrimaryNoGap(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{Name: "plan", Role: "delegate"},
		},
	}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapComposeIntoPrimary {
			t.Fatalf("did not expect compose_into_primary gap when unset, got %+v", g)
		}
	}
}

// TestDetectGaps_ComposedAwaySuppressesPerAgentDispatchGaps covers the
// CodeRabbit finding on PR #23: an agent composed into the primary on a
// renderer that declares CapComposeIntoPrimary is never rendered as a
// standalone, independently dispatchable agent there, so per-agent gaps
// that specifically describe standalone-dispatch behavior (steps,
// per-agent bash profile, external_directory, task permission,
// per-tool MCP ask) must not fire for it — they'd misleadingly imply the
// agent is dispatchable when it isn't.
func TestDetectGaps_ComposedAwaySuppressesPerAgentDispatchGaps(t *testing.T) {
	steps := 5
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{
				Name:  "plan",
				Role:  "advisory",
				Steps: &steps,
				Permissions: registry.Permissions{
					Task:              "deny",
					Bash:              registry.BashPermission{Profile: "readonly"},
					ExternalDirectory: map[string]registry.Decision{"*": registry.Ask},
				},
				MCP: []registry.AgentMCP{{Server: "github", Ask: []string{"create_*"}}},
			},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapComposeIntoPrimary})

	suppressed := []Capability{
		CapAgentSteps,
		CapPerAgentBashPolicy,
		CapExternalDirectory,
		CapAgentTaskPermission,
		CapMCPPerToolAsk,
	}
	for _, cap := range suppressed {
		for _, g := range gaps {
			if g.Capability == cap {
				t.Errorf("did not expect a %s gap for a composed-away agent, got %+v", cap, g)
			}
		}
	}
}

// TestDetectGaps_AdvisoryWithoutPrimaryDoesNotSuppressGaps covers a
// registry with no role: primary agent: an advisory agent has nothing to
// compose into, so it falls back to a normal standalone file (see
// omp_test.go's TestRender_AdvisoryWithoutPrimaryFallsBackToStandaloneFile)
// and every per-agent dispatch gap that would otherwise be suppressed for
// a genuinely composed-away agent must still fire.
func TestDetectGaps_AdvisoryWithoutPrimaryDoesNotSuppressGaps(t *testing.T) {
	steps := 5
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{
				Name:  "plan",
				Role:  "advisory",
				Steps: &steps,
				Permissions: registry.Permissions{
					Task: "deny",
				},
			},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapComposeIntoPrimary})

	var found bool
	for _, g := range gaps {
		if g.Capability == CapAgentSteps && g.Subject == "agent:plan.steps" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an agent_steps gap for orphaned advisory agent %q, got %+v", "plan", gaps)
	}
}

// TestDetectGaps_ComposeFlagSetButRendererDoesNotSupportItStillGaps
// covers the opencode-shaped side: when the renderer does NOT declare
// CapComposeIntoPrimary, the agent renders as a normal standalone agent
// there (per docs/schema.md), so its per-agent gaps must fire exactly as
// they would with the flag unset.
func TestDetectGaps_ComposeFlagSetButRendererDoesNotSupportItStillGaps(t *testing.T) {
	reg := &registry.Registry{
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary"},
			{
				Name:        "plan",
				Role:        "advisory",
				Permissions: registry.Permissions{Task: "deny"},
			},
		},
	}

	gaps := DetectGaps(reg, nil)

	var found bool
	for _, g := range gaps {
		if g.Capability == CapAgentTaskPermission && g.Subject == "agent:plan.permissions.task" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an agent_task_permission gap for agent:plan on a renderer without CapComposeIntoPrimary, got %+v", gaps)
	}
}

func TestDetectGaps_CustomCommandsDropped(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "Reviews a diff"},
		},
	}

	gaps := DetectGaps(reg, nil)

	if len(gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(gaps), gaps)
	}
	g := gaps[0]
	if g.Kind != GapSkip || g.Capability != CapCustomCommands {
		t.Errorf("got kind=%s capability=%s, want skip/custom_commands", g.Kind, g.Capability)
	}
	if g.Subject != "command:review" {
		t.Errorf("got subject %q, want command:review", g.Subject)
	}
}

func TestDetectGaps_CustomCommandsSuppressedWhenDeclared(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "Reviews a diff"},
		},
	}

	gaps := DetectGaps(reg, []Capability{CapCustomCommands})

	for _, g := range gaps {
		if g.Capability == CapCustomCommands {
			t.Fatalf("did not expect custom_commands gap when declared, got %+v", g)
		}
	}
}

func TestDetectGaps_NoCommandsNoGap(t *testing.T) {
	reg := &registry.Registry{}

	gaps := DetectGaps(reg, nil)

	for _, g := range gaps {
		if g.Capability == CapCustomCommands {
			t.Fatalf("did not expect custom_commands gap when no commands declared, got %+v", g)
		}
	}
}
