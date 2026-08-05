package bashpolicy

import "testing"

func TestScore_ExactLiteralBeatsPrefixWildcard(t *testing.T) {
	// This is the specific defect the refined heuristic fixes: a naive
	// "count non-* chars" scorer treats "git status" and "git status*"
	// as tied (both have 10 literal chars), so ordering between them is
	// arbitrary. The refined heuristic must always rank the exact literal
	// ahead of the prefix-wildcard pattern.
	exact := Score("git status")
	prefix := Score("git status*")

	if !Less(exact, prefix) {
		t.Fatalf("Score(%q) = %+v should sort before Score(%q) = %+v (exact literal must beat wildcard prefix)",
			"git status", exact, "git status*", prefix)
	}
}

func TestScore_HasWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"git status", false},
		{"git status*", true},
		{"git ?tatus", true},
		{"rm -rf [/~]*", true},
		{`\*literal`, false},
	}
	for _, tt := range tests {
		got := Score(tt.pattern).HasWildcard
		if got != tt.want {
			t.Errorf("Score(%q).HasWildcard = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}

func TestScore_CharacterClassCountsAsOneWildcardToken(t *testing.T) {
	// A naive heuristic that counts non-'*' characters would score
	// "rm -rf [/~]*" as having "rf [/~]" worth of literal characters
	// (miscounting the bracket contents as literals). The refined
	// heuristic must treat the whole [...] group as a single wildcard
	// token contributing 0 to LiteralCount and 1 to WildcardCount.
	got := Score("rm -rf [/~]*")

	if got.WildcardCount != 2 {
		t.Errorf("WildcardCount = %d, want 2 (the [/~] class and the trailing *)", got.WildcardCount)
	}
	// "rm -rf " is 7 literal characters; "[/~]" and "*" are wildcard
	// tokens contributing 0 literal characters between them.
	if got.LiteralCount != 7 {
		t.Errorf("LiteralCount = %d, want 7", got.LiteralCount)
	}
}

func TestScore_EscapedStarIsLiteralNotWildcard(t *testing.T) {
	got := Score(`\*foo`)

	if got.HasWildcard {
		t.Errorf("HasWildcard = true, want false for escaped star")
	}
	if got.WildcardCount != 0 {
		t.Errorf("WildcardCount = %d, want 0", got.WildcardCount)
	}
	// After un-escaping, "*foo" is 4 literal characters.
	if got.LiteralCount != 4 {
		t.Errorf("LiteralCount = %d, want 4", got.LiteralCount)
	}
}

func TestScore_StartsWithWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"*.log", true},
		{"app*", false},
		{"app.log", false},
		{"?pp.log", true},
		{"[ab]pp.log", true},
		{`\*.log`, false}, // escaped star is literal, not a leading wildcard
	}
	for _, tt := range tests {
		got := Score(tt.pattern).StartsWithWildcard
		if got != tt.want {
			t.Errorf("Score(%q).StartsWithWildcard = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}

func TestOrder_RealisticPatternSet(t *testing.T) {
	// A concrete ordering proof across a handful of realistic patterns
	// together, not just pairwise: exact literals sort first (by literal
	// character count), then wildcard patterns sort by literal character
	// count, and "log.*" vs "*.log" — identical literal/wildcard counts,
	// differing only in whether the wildcard is anchored at the start —
	// proves the anchoring tiebreak fires when everything else ties.
	patterns := []string{
		"*.log",
		"app*",
		"app.log",
		"git status*",
		"git status",
		"log.*",
		"rm -rf [/~]*",
	}

	got := Order(patterns)

	want := []string{
		"git status",   // exact, 10 literal chars
		"app.log",      // exact, 7 literal chars
		"git status*",  // wildcard, 10 literal chars
		"rm -rf [/~]*", // wildcard, 7 literal chars (bracket group is 1 token)
		"log.*",        // wildcard, 4 literal chars, anchored (not leading-wildcard)
		"*.log",        // wildcard, 4 literal chars, leading wildcard
		"app*",         // wildcard, 3 literal chars, least specific
	}

	if len(got) != len(want) {
		t.Fatalf("Order() returned %d patterns, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Order() = %v, want %v (mismatch at index %d)", got, want, i)
		}
	}
}
