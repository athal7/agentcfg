package bashpolicy

// AsMap returns the compiled map unchanged — the "unordered" native shape
// for a harness that resolves patterns by its own specificity logic (e.g.
// opencode).
func AsMap(m map[string]Decision) map[string]Decision {
	return m
}

// AsOrderedList converts the compiled map into a deterministic
// first-match-wins slice, most-specific pattern first, using Score.
func AsOrderedList(m map[string]Decision) []Rule {
	patterns := make([]string, 0, len(m))
	for pattern := range m {
		patterns = append(patterns, pattern)
	}
	ordered := Order(patterns)

	rules := make([]Rule, len(ordered))
	for i, pattern := range ordered {
		rules[i] = Rule{Pattern: pattern, Decision: m[pattern]}
	}
	return rules
}
