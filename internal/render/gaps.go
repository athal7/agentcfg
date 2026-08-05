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
	return gaps
}

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

func detectPrimaryAgentGap(reg *registry.Registry, has map[Capability]bool) []Gap {
	// CapPromptAppend is the documented substitute mechanism for
	// CapPrimaryAgent: a harness that appends the primary agent's prompt
	// as a whole-session system prompt has already expressed "there is a
	// primary agent", just not via a default-agent key.
	if has[CapPrimaryAgent] || has[CapPromptAppend] {
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

func detectMCPToolGlobsGaps(reg *registry.Registry, has map[Capability]bool) []Gap {
	if has[CapMCPToolGlobs] {
		return nil
	}
	var gaps []Gap
	for _, s := range reg.MCPServers {
		if len(s.Tools) > 0 {
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
