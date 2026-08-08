package render

import (
	"fmt"
	"strings"

	"github.com/athal7/agentcfg/internal/registry"
)

// DetectGaps walks the registry and reports every use of a capability the
// renderer didn't declare. Renderers call this INSIDE their Render() to get
// the common gaps, then append their own bespoke gaps (e.g. reductions
// specific to how they degrade a feature) on top. Never hand-roll these
// checks inside a renderer — route through here so registry-walking logic
// isn't duplicated across renderers.
func DetectGaps(reg *registry.Registry, declared []Capability) []Gap {
	has := make(map[Capability]bool, len(declared))
	for _, c := range declared {
		has[c] = true
	}

	var gaps []Gap
	gaps = append(gaps, detectAgentStepsGaps(reg, has)...)
	gaps = append(gaps, detectPerAgentBashPolicyGap(reg, has)...)
	gaps = append(gaps, detectPrimaryAgentGap(reg, has)...)
	gaps = append(gaps, detectExternalDirectoryGaps(reg, has)...)
	gaps = append(gaps, detectMCPToolGlobsGaps(reg, has)...)
	gaps = append(gaps, detectAgentTaskPermissionGaps(reg, has)...)
	gaps = append(gaps, detectMCPPerToolAskGaps(reg, has)...)
	gaps = append(gaps, detectCustomCommandsGap(reg, has)...)
	return gaps
}

// detectAgentStepsGaps reports a GapSkip for every agent that sets steps:
// when the renderer doesn't declare CapAgentSteps.
func detectAgentStepsGaps(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapAgentSteps] {
		return nil
	}
	var gaps []Gap
	for _, a := range reg.Agents {
		if a.Steps == nil {
			continue
		}
		gaps = append(gaps, Gap{
			Kind:       GapSkip,
			Capability: CapAgentSteps,
			Subject:    fmt.Sprintf("agent:%s.steps", a.Name),
			Detail: fmt.Sprintf(
				"agent %q sets steps: %d; this harness has no step-budget mechanism, so the step limit is dropped.",
				a.Name, *a.Steps,
			),
		})
	}
	return gaps
}

// isPerAgentBashProfile reports whether an agent's bash permission names a
// profile other than "global" — the harness-wide default profile name
// every renderer compiles for its baseline policy.
func isPerAgentBashProfile(b registry.BashPermission) bool {
	return b.Profile != "" && b.Profile != "global"
}

// detectPerAgentBashPolicyGap reports a GapSkip when the renderer doesn't
// declare CapPerAgentBashPolicy and one or more agents name a per-agent
// bash profile other than "global".
func detectPerAgentBashPolicyGap(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapPerAgentBashPolicy] {
		return nil
	}
	var names []string
	for _, a := range reg.Agents {
		if isPerAgentBashProfile(a.Permissions.Bash) {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Gap{{
		Kind:       GapSkip,
		Capability: CapPerAgentBashPolicy,
		Subject:    strings.Join(names, ", "),
		Detail:     "this harness has no per-agent bash scoping; only the global bash profile is applied, harness-wide, so per-agent profile overrides are dropped.",
	}}
}

// detectPrimaryAgentGap reports a GapReduction when the renderer doesn't
// declare CapPrimaryAgent (or CapPromptAppend as a substitute) and the
// registry has a mode:primary agent.
func detectPrimaryAgentGap(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapPrimaryAgent] {
		return nil
	}
	if sub, ok := SubstituteOf(CapPrimaryAgent); ok && has[sub] {
		return nil
	}
	for _, a := range reg.Agents {
		if a.Mode == "primary" {
			return []Gap{{
				Kind:       GapReduction,
				Capability: CapPrimaryAgent,
				Subject:    fmt.Sprintf("agent:%s", a.Name),
				Detail:     "no default-agent key; primary agent's prompt appended as a whole-session system-prompt file instead.",
			}}
		}
	}
	return nil
}

// detectExternalDirectoryGaps reports a GapSkip for every agent that sets
// permissions.external_directory when the renderer doesn't declare
// CapExternalDirectory.
func detectExternalDirectoryGaps(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapExternalDirectory] {
		return nil
	}
	var gaps []Gap
	for _, a := range reg.Agents {
		if len(a.Permissions.ExternalDirectory) == 0 {
			continue
		}
		gaps = append(gaps, Gap{
			Kind:       GapSkip,
			Capability: CapExternalDirectory,
			Subject:    fmt.Sprintf("agent:%s.permissions.external_directory", a.Name),
			Detail: fmt.Sprintf(
				"agent %q sets permissions.external_directory; this harness has no external-directory access policy, so it was dropped.",
				a.Name,
			),
		})
	}
	return gaps
}

// detectMCPToolGlobsGaps reports a GapSkip for every MCP server that sets
// tools: when the renderer doesn't declare CapMCPToolGlobs.
func detectMCPToolGlobsGaps(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapMCPToolGlobs] {
		return nil
	}
	var gaps []Gap
	for _, s := range reg.MCPServers {
		if len(s.Tools) == 0 {
			continue
		}
		gaps = append(gaps, Gap{
			Kind:       GapSkip,
			Capability: CapMCPToolGlobs,
			Subject:    fmt.Sprintf("mcp:%s", s.Name),
			Detail: fmt.Sprintf(
				"mcp server %q has no tool allowlist support in this harness; all of its tools are exposed without glob-based filtering.",
				s.Name,
			),
		})
	}
	return gaps
}

// detectAgentTaskPermissionGaps reports a GapSkip for every agent that sets
// permissions.task when the renderer doesn't declare CapAgentTaskPermission.
func detectAgentTaskPermissionGaps(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapAgentTaskPermission] {
		return nil
	}
	var gaps []Gap
	for _, a := range reg.Agents {
		if a.Permissions.Task == "" {
			continue
		}
		gaps = append(gaps, Gap{
			Kind:       GapSkip,
			Capability: CapAgentTaskPermission,
			Subject:    fmt.Sprintf("agent:%s.permissions.task", a.Name),
			Detail: fmt.Sprintf(
				"agent %q sets permissions.task=%q; this harness has no task-dispatch permission control, so subagent dispatch is always allowed.",
				a.Name, a.Permissions.Task,
			),
		})
	}
	return gaps
}

// detectMCPPerToolAskGaps reports a GapSkip for every agent MCP entry that
// sets per-tool ask patterns when the renderer doesn't declare
// CapMCPPerToolAsk.
func detectMCPPerToolAskGaps(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapMCPPerToolAsk] {
		return nil
	}
	var gaps []Gap
	for _, a := range reg.Agents {
		for _, m := range a.MCP {
			if len(m.Ask) == 0 {
				continue
			}
			gaps = append(gaps, Gap{
				Kind:       GapSkip,
				Capability: CapMCPPerToolAsk,
				Subject:    fmt.Sprintf("agent:%s.mcp:%s", a.Name, m.Server),
				Detail: fmt.Sprintf(
					"agent %q's mcp server %q sets per-tool ask patterns %v; this harness has no per-tool ask-listing, so tools are either fully allowed or fully blocked at the server level.",
					a.Name, m.Server, m.Ask,
				),
			})
		}
	}
	return gaps
}

// detectCustomCommandsGap reports a GapSkip for every registry command
// when the renderer doesn't declare CapCustomCommands. Unlike
// detectPrimaryAgentGap, there's no registered substitute pair to check
// (render.SubstituteOf) — every current renderer that supports custom
// commands does so via the identical Agent Skills mechanism, so a
// renderer lacking CapCustomCommands has no alternative expression of a
// command at all; every one is simply dropped.
func detectCustomCommandsGap(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapCustomCommands] {
		return nil
	}
	var gaps []Gap
	for _, c := range reg.Commands {
		gaps = append(gaps, Gap{
			Kind:       GapSkip,
			Capability: CapCustomCommands,
			Subject:    fmt.Sprintf("command:%s", c.Name),
			Detail: fmt.Sprintf(
				"command %q has no Agent Skills (SKILL.md) support in this harness, so it was dropped entirely.",
				c.Name,
			),
		})
	}
	return gaps
}
