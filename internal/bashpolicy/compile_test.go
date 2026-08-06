package bashpolicy

import (
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
)

func boolPtr(b bool) *bool { return &b }

func stringSlicePtr(s []string) *[]string { return &s }

func TestCompile_PrecedenceExample(t *testing.T) {
	policy := registry.BashPolicy{
		DefaultLists: stringSlicePtr([]string{"guardrails", "git"}),
		Lists: map[string]map[string]registry.Decision{
			"guardrails": {"rm -rf /*": registry.Ask},
			"git": {
				"git commit*": registry.Ask,
				"git status*": registry.Allow,
			},
			"lead": {"gh run view*": registry.Allow},
		},
		Profiles: map[string]registry.BashProfile{
			"lead": {Base: registry.Allow, Lists: []string{"lead"}},
		},
	}

	got, err := Compile(policy, "lead")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	want := map[string]Decision{
		"gh run view*": Allow,
		"rm -rf /*":    Ask,
		"git commit*":  Ask,
		"git status*":  Allow,
		"*":            Allow,
	}

	if len(got) != len(want) {
		t.Fatalf("Compile() = %v, want %v", got, want)
	}
	for pattern, decision := range want {
		if got[pattern] != decision {
			t.Errorf("Compile()[%q] = %q, want %q", pattern, got[pattern], decision)
		}
	}
}

func TestCompile_ProfileListOverridesDefaultListForSamePattern(t *testing.T) {
	policy := registry.BashPolicy{
		DefaultLists: stringSlicePtr([]string{"git"}),
		Lists: map[string]map[string]registry.Decision{
			"git":      {"git commit*": registry.Ask},
			"override": {"git commit*": registry.Allow},
		},
		Profiles: map[string]registry.BashProfile{
			"custom": {Base: registry.Deny, Lists: []string{"override"}},
		},
	}

	got, err := Compile(policy, "custom")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if got["git commit*"] != Allow {
		t.Errorf(`Compile()["git commit*"] = %q, want %q (profile list must win over default list)`, got["git commit*"], Allow)
	}
}

func TestCompile_DefaultListsFalseSuppressesDefaultChain(t *testing.T) {
	policy := registry.BashPolicy{
		DefaultLists: stringSlicePtr([]string{"guardrails", "git"}),
		Lists: map[string]map[string]registry.Decision{
			"guardrails": {"rm -rf /*": registry.Ask},
			"git":        {"git commit*": registry.Ask},
			"lead":       {"gh run view*": registry.Allow},
		},
		Profiles: map[string]registry.BashProfile{
			"locked": {Base: registry.Deny, Lists: []string{"lead"}, DefaultLists: boolPtr(false)},
		},
	}

	got, err := Compile(policy, "locked")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	want := map[string]Decision{
		"gh run view*": Allow,
		"*":            Deny,
	}
	if len(got) != len(want) {
		t.Fatalf("Compile() = %v, want %v (default_lists: false must suppress guardrails/git)", got, want)
	}
	for pattern, decision := range want {
		if got[pattern] != decision {
			t.Errorf("Compile()[%q] = %q, want %q", pattern, got[pattern], decision)
		}
	}
}

func TestCompile_UnknownProfileErrors(t *testing.T) {
	policy := registry.BashPolicy{
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Allow},
		},
	}

	_, err := Compile(policy, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
}

func TestCompile_UnknownListReferencedByProfileErrors(t *testing.T) {
	policy := registry.BashPolicy{
		Profiles: map[string]registry.BashProfile{
			"broken": {Base: registry.Allow, Lists: []string{"nonexistent"}},
		},
	}

	_, err := Compile(policy, "broken")
	if err == nil {
		t.Fatal("expected error for profile referencing unknown list, got nil")
	}
}
