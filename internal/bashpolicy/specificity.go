package bashpolicy

import "sort"

// SortKey is a 6-tuple specificity score for a glob pattern. Comparing two
// SortKeys with Less produces a deterministic, most-specific-first total
// order — this replaces a naive "count non-* characters" heuristic that
// mis-ranks an exact literal against its own prefix-wildcard variant (e.g.
// "git status" vs "git status*") and miscounts character classes as
// several literal characters instead of one wildcard token.
type SortKey struct {
	HasWildcard        bool   // false (no wildcard at all) sorts first
	LiteralCount       int    // literal characters outside wildcard tokens; descending
	WildcardCount      int    // number of *, ?, or [...] tokens; ascending
	StartsWithWildcard bool   // false (anchored) sorts first
	Length             int    // len(pattern); descending
	Pattern            string // lexicographic ascending, final tiebreak
}

// Score computes pattern's specificity SortKey. A wildcard "token" is one
// of *, ?, or a bracket expression [...] (the whole bracketed group counts
// as one token). An escaped star \* is a literal character, not a wildcard.
func Score(pattern string) SortKey {
	runes := []rune(pattern)
	literalCount, wildcardCount := 0, 0
	startsWithWildcard := false

	for i, isFirst := 0, true; i < len(runes); isFirst = false {
		c := runes[i]
		switch {
		case c == '\\' && i+1 < len(runes):
			// Escaped character: literal, consume the backslash too.
			literalCount++
			i += 2
		case c == '*' || c == '?':
			wildcardCount++
			if isFirst {
				startsWithWildcard = true
			}
			i++
		case c == '[':
			end := i + 1
			for end < len(runes) && runes[end] != ']' {
				end++
			}
			if end < len(runes) {
				// Terminated bracket expression: one wildcard token.
				wildcardCount++
				if isFirst {
					startsWithWildcard = true
				}
				i = end + 1
			} else {
				// Unterminated bracket: treat '[' as a literal character.
				literalCount++
				i++
			}
		default:
			literalCount++
			i++
		}
	}

	return SortKey{
		HasWildcard:        wildcardCount > 0,
		LiteralCount:       literalCount,
		WildcardCount:      wildcardCount,
		StartsWithWildcard: startsWithWildcard,
		Length:             len(pattern),
		Pattern:            pattern,
	}
}

// Less reports whether a is at least as specific as b and should sort
// before it: exact literals before wildcards, more literal characters
// before fewer, fewer/narrower wildcard tokens before more, anchored
// patterns before leading-wildcard ones, longer patterns before shorter,
// and finally lexicographic order as a tiebreak that guarantees totality.
func Less(a, b SortKey) bool {
	if a.HasWildcard != b.HasWildcard {
		return !a.HasWildcard
	}
	if a.LiteralCount != b.LiteralCount {
		return a.LiteralCount > b.LiteralCount
	}
	if a.WildcardCount != b.WildcardCount {
		return a.WildcardCount < b.WildcardCount
	}
	if a.StartsWithWildcard != b.StartsWithWildcard {
		return !a.StartsWithWildcard
	}
	if a.Length != b.Length {
		return a.Length > b.Length
	}
	return a.Pattern < b.Pattern
}

// Order sorts patterns most-specific-first using Score and Less.
func Order(patterns []string) []string {
	out := make([]string, len(patterns))
	copy(out, patterns)
	sort.SliceStable(out, func(i, j int) bool {
		return Less(Score(out[i]), Score(out[j]))
	})
	return out
}
