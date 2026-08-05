package bashpolicy

// MatchGlob reports whether s matches pattern, using *, ?, and [...] glob
// semantics with the whole string matched uniformly — unlike path.Match,
// there's no special-casing of a path separator. An escaped star \* matches
// a literal "*" in s.
//
// This is the one glob-matching implementation every caller in this
// package (and any external caller, e.g. the `explain bash` CLI command)
// must use. It used to live only in differential_test.go as a
// test-private oracle; it's promoted here so non-test code can match the
// exact same semantics the differential test itself checks against —
// there must never be a second, divergent copy of this logic.
func MatchGlob(pattern, s string) bool {
	return matchGlobFrom([]rune(pattern), 0, []rune(s), 0)
}

func matchGlobFrom(p []rune, pi int, s []rune, si int) bool {
	for pi < len(p) {
		switch {
		case p[pi] == '\\' && pi+1 < len(p):
			if si >= len(s) || s[si] != p[pi+1] {
				return false
			}
			pi += 2
			si++
		case p[pi] == '*':
			for pi < len(p) && p[pi] == '*' {
				pi++
			}
			if pi == len(p) {
				return true
			}
			for k := si; k <= len(s); k++ {
				if matchGlobFrom(p, pi, s, k) {
					return true
				}
			}
			return false
		case p[pi] == '?':
			if si >= len(s) {
				return false
			}
			pi++
			si++
		case p[pi] == '[':
			end := pi + 1
			negate := false
			if end < len(p) && (p[end] == '!' || p[end] == '^') {
				negate = true
				end++
			}
			start := end
			for end < len(p) && p[end] != ']' {
				end++
			}
			if end >= len(p) {
				// Unterminated bracket: treat '[' as a literal.
				if si >= len(s) || s[si] != '[' {
					return false
				}
				pi++
				si++
				continue
			}
			if si >= len(s) {
				return false
			}
			if charInClass(s[si], p[start:end]) == negate {
				return false
			}
			pi = end + 1
			si++
		default:
			if si >= len(s) || s[si] != p[pi] {
				return false
			}
			pi++
			si++
		}
	}
	return si == len(s)
}

func charInClass(c rune, class []rune) bool {
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			lo, hi := class[i], class[i+2]
			if c >= lo && c <= hi {
				return true
			}
			i += 2
			continue
		}
		if class[i] == c {
			return true
		}
	}
	return false
}

// MostSpecificMatch scans every pattern in m, keeps the ones matching cmd,
// and returns the Decision and winning pattern of whichever matching
// pattern Score/Less ranks most specific — the "unordered map" resolution
// semantics a harness like opencode uses. ok is false only if nothing in m
// matches cmd; a map produced by Compile always has a "*" fallback, so a
// real caller always gets ok=true.
func MostSpecificMatch(m map[string]Decision, cmd string) (decision Decision, pattern string, ok bool) {
	best := ""
	for p := range m {
		if !MatchGlob(p, cmd) {
			continue
		}
		if !ok || Less(Score(p), Score(best)) {
			best = p
			ok = true
		}
	}
	if !ok {
		return "", "", false
	}
	return m[best], best, true
}

// FirstMatch walks rules in order and returns the Decision, winning
// pattern, and index of the first rule matching cmd — the "ordered list"
// resolution semantics a harness like omp uses. rules is expected to
// already be specificity-ordered (e.g. via AsOrderedList). ok is false
// only if nothing in rules matches cmd.
func FirstMatch(rules []Rule, cmd string) (decision Decision, pattern string, index int, ok bool) {
	for i, r := range rules {
		if MatchGlob(r.Pattern, cmd) {
			return r.Decision, r.Pattern, i, true
		}
	}
	return "", "", 0, false
}
