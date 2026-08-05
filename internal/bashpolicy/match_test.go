package bashpolicy

import "testing"

func TestMostSpecificMatch_ExactBeatsWildcard(t *testing.T) {
	m := map[string]Decision{
		"*":           Deny,
		"git status*": Ask,
		"git status":  Allow,
	}

	decision, pattern, ok := MostSpecificMatch(m, "git status")
	if !ok {
		t.Fatalf("MostSpecificMatch returned ok=false")
	}
	if decision != Allow {
		t.Errorf("got decision %q, want %q", decision, Allow)
	}
	if pattern != "git status" {
		t.Errorf("got winning pattern %q, want %q", pattern, "git status")
	}
}

func TestMostSpecificMatch_NoMatchReturnsFalse(t *testing.T) {
	m := map[string]Decision{"git status": Allow}

	_, _, ok := MostSpecificMatch(m, "rm -rf /")
	if ok {
		t.Errorf("MostSpecificMatch returned ok=true, want false (nothing matches)")
	}
}

func TestFirstMatch_ReturnsFirstMatchingRuleAndIndex(t *testing.T) {
	rules := []Rule{
		{Pattern: "git status", Decision: Allow},
		{Pattern: "git status*", Decision: Ask},
		{Pattern: "*", Decision: Deny},
	}

	decision, pattern, index, ok := FirstMatch(rules, "git status --short")
	if !ok {
		t.Fatalf("FirstMatch returned ok=false")
	}
	if decision != Ask || pattern != "git status*" || index != 1 {
		t.Errorf("got (%q, %q, %d), want (%q, %q, 1)", decision, pattern, index, Ask, "git status*")
	}
}

func TestFirstMatch_NoMatchReturnsFalse(t *testing.T) {
	rules := []Rule{{Pattern: "git status", Decision: Allow}}

	_, _, _, ok := FirstMatch(rules, "rm -rf /")
	if ok {
		t.Errorf("FirstMatch returned ok=true, want false (nothing matches)")
	}
}
