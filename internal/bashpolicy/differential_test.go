package bashpolicy

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
)

// matchGlob, mostSpecificMatch, and firstMatch below are thin wrappers
// around the promoted, non-test implementations in match.go
// (MatchGlob/MostSpecificMatch/FirstMatch). Keeping the differential
// test's own oracle calling the promoted functions — rather than a second,
// independently-written copy of the matching logic — is the whole point:
// a bug in the promoted matcher must show up here too, not diverge
// silently from what `explain bash` (or any other future caller) sees.
func matchGlob(pattern, s string) bool {
	return MatchGlob(pattern, s)
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		s       string
		want    bool
	}{
		{"git status*", "git status", true},
		{"git status*", "git status --short", true},
		{"git status*", "git stat", false},
		{"rm -rf ?", "rm -rf /", true},
		{"rm -rf ?", "rm -rf /*", false},
		{"rm -rf [/~]*", "rm -rf /", true},
		{"rm -rf [/~]*", "rm -rf ~", true},
		{"rm -rf [/~]*", "rm -rf ~/Documents", true},
		{"rm -rf [/~]*", "rm -rf ./local", false},
		{`echo \*literal`, "echo *literal", true},
		{`echo \*literal`, "echo anything", false},
		{"*", "anything at all", true},
		{"*", "", true},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		// Escaped literal characters (not just \*)
		{`echo \?question`, "echo ?question", true},
		{`echo \?question`, "echo anything", false},
		{`echo \[bracket`, "echo [bracket", true},
		{`echo \[bracket`, "echo anything", false},
		{`echo \[`, "echo [", true},
		{`echo \[`, "echo anything", false},
		// Bracket negation with !
		{"[!abc]", "d", true},
		{"[!abc]", "a", false},
		{"[!abc]", "b", false},
		{"[!abc]", "c", false},
		// Bracket negation with ^
		{"[^abc]", "d", true},
		{"[^abc]", "a", false},
		// Bracket range
		{"[a-z]", "m", true},
		{"[a-z]", "a", true},
		{"[a-z]", "z", true},
		{"[a-z]", "A", false},
		{"[a-z]", "1", false},
		// Bracket range negation
		{"[!0-9]", "a", true},
		{"[!0-9]", "5", false},
		// Unterminated bracket treated as literal [
		{"[abc", "[abc", true},
		{"[abc", "[", false},
		{"[abc", "x", false},
	}
	for _, tt := range tests {
		got := matchGlob(tt.pattern, tt.s)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
		}
	}
}

// mostSpecificMatch is the differential test's "unordered" oracle: it
// delegates to the promoted MostSpecificMatch and panics (rather than
// returning ok=false) since every fixture command is guaranteed to match
// at least "*" — a false here is a test fixture bug, not a real outcome.
func mostSpecificMatch(m map[string]Decision, cmd string) Decision {
	decision, _, ok := MostSpecificMatch(m, cmd)
	if !ok {
		panic(fmt.Sprintf("mostSpecificMatch: no pattern in %v matches command %q (test fixture bug: every command must match at least \"*\")", m, cmd))
	}
	return decision
}

// firstMatch is the differential test's "ordered" oracle: it delegates to
// the promoted FirstMatch, walking rules in the order a real harness would
// (AsOrderedList's most-specific-first order).
func firstMatch(rules []Rule, cmd string) Decision {
	decision, _, _, ok := FirstMatch(rules, cmd)
	if !ok {
		panic(fmt.Sprintf("firstMatch: no rule in %v matches command %q (test fixture bug: every command must match at least \"*\")", rules, cmd))
	}
	return decision
}

// differentialFixture is one bash policy + profile to exercise against the
// full command corpus.
type differentialFixture struct {
	name    string
	policy  registry.BashPolicy
	profile string
}

// differentialFixtures returns policies with deliberately overlapping and
// nested patterns (both "git status" and "git status*" in the same list,
// "git commit*" and "git commit -am*", "git push*" and "git push --force*",
// etc.) so the corpus meaningfully exercises specificity ordering rather
// than only ever hitting one pattern per command.
func differentialFixtures() []differentialFixture {
	leadPolicy := registry.BashPolicy{
		DefaultLists: stringSlicePtr([]string{"guardrails"}),
		Lists: map[string]map[string]registry.Decision{
			"guardrails": {
				"rm -rf /*": registry.Ask,
				"rm -rf ~*": registry.Ask,
				"sudo *":    registry.Ask,
				"dd *":      registry.Ask,
			},
			"git": {
				"git status":        registry.Allow,
				"git status*":       registry.Allow,
				"git commit*":       registry.Ask,
				"git commit -am*":   registry.Allow,
				"git push*":         registry.Ask,
				"git push --force*": registry.Deny,
				"git push -f*":      registry.Deny,
			},
			"gh": {
				"gh run view*": registry.Allow,
				"gh api *":     registry.Ask,
			},
			"network": {
				"curl *": registry.Ask,
				"wget *": registry.Ask,
			},
		},
		Profiles: map[string]registry.BashProfile{
			"lead": {Base: registry.Allow, Lists: []string{"git", "gh", "network"}},
		},
	}

	lockedPolicy := registry.BashPolicy{
		Lists: map[string]map[string]registry.Decision{
			"readonly": {
				"git status":  registry.Allow,
				"git status*": registry.Allow,
				"ls *":        registry.Allow,
				"cat *":       registry.Allow,
				"pwd":         registry.Allow,
			},
		},
		Profiles: map[string]registry.BashProfile{
			"locked": {Base: registry.Deny, Lists: []string{"readonly"}, DefaultLists: boolPtr(false)},
		},
	}

	globalPolicy := registry.BashPolicy{
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Allow},
		},
	}

	return []differentialFixture{
		{name: "lead", policy: leadPolicy, profile: "lead"},
		{name: "locked", policy: lockedPolicy, profile: "locked"},
		{name: "global", policy: globalPolicy, profile: "global"},
	}
}

func readCommandCorpus(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("testdata/commands.txt")
	if err != nil {
		t.Fatalf("reading testdata/commands.txt: %v", err)
	}
	var commands []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commands = append(commands, line)
		}
	}
	if len(commands) == 0 {
		t.Fatal("testdata/commands.txt is empty")
	}
	return commands
}

// TestDifferential_OrderedMatchesUnordered is the required correctness net:
// for every fixture profile and every command in the corpus, the ordered
// first-match-wins projection (AsOrderedList) must agree with the unordered
// most-specific-match oracle. A disagreement here means AsOrderedList's
// ordering silently resolves to the wrong decision for some real harness.
func TestDifferential_OrderedMatchesUnordered(t *testing.T) {
	commands := readCommandCorpus(t)

	for _, fixture := range differentialFixtures() {
		compiled, err := Compile(fixture.policy, fixture.profile)
		if err != nil {
			t.Fatalf("Compile(%s) error = %v", fixture.name, err)
		}
		ordered := AsOrderedList(compiled)

		for _, cmd := range commands {
			gotOrdered := firstMatch(ordered, cmd)
			gotUnordered := mostSpecificMatch(compiled, cmd)
			if gotOrdered != gotUnordered {
				t.Errorf("profile %q, command %q: firstMatch(AsOrderedList(...)) = %q but mostSpecificMatch(...) = %q",
					fixture.name, cmd, gotOrdered, gotUnordered)
			}
		}
	}
}

// TestSpecificity_ExactBeatsPrefix_Regression is the specific defect the
// refined 6-tuple heuristic fixes: a naive "count non-* characters"
// scorer ties "git status" (10 literal chars) and "git status*" (also 10
// literal chars, ignoring the *), so which one "wins" is arbitrary and can
// silently resolve an exact allow to a prefix ask (or worse, a prefix
// allow to an exact deny). Both oracles must agree the exact pattern wins.
func TestSpecificity_ExactBeatsPrefix_Regression(t *testing.T) {
	policy := registry.BashPolicy{
		Lists: map[string]map[string]registry.Decision{
			"git": {
				"git status":  registry.Allow,
				"git status*": registry.Ask,
			},
		},
		Profiles: map[string]registry.BashProfile{
			"test": {Base: registry.Deny, Lists: []string{"git"}},
		},
	}

	compiled, err := Compile(policy, "test")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	const cmd = "git status"

	if got := mostSpecificMatch(compiled, cmd); got != Allow {
		t.Errorf("mostSpecificMatch(%q) = %q, want %q (exact pattern must beat prefix wildcard)", cmd, got, Allow)
	}
	if got := firstMatch(AsOrderedList(compiled), cmd); got != Allow {
		t.Errorf("firstMatch(AsOrderedList(...), %q) = %q, want %q (exact pattern must beat prefix wildcard)", cmd, got, Allow)
	}
}
