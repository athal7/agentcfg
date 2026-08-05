// Package bashpolicy compiles a named bash policy profile (from the
// registry's bash.yaml) into a flattened decision map, and provides the
// specificity-aware ordering that lets a harness resolve overlapping glob
// patterns deterministically. This is security-critical: a bug here can
// silently turn a deny/ask into an allow.
package bashpolicy

import (
	"fmt"

	"github.com/athal7/agentcfg/internal/registry"
)

// Decision is a bash command policy outcome.
type Decision string

const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
	Ask   Decision = "ask"
)

// Rule is one pattern/decision pair.
type Rule struct {
	Pattern  string
	Decision Decision
}

// Compile flattens a named profile from policy into a canonical,
// order-independent map[pattern]Decision.
//
// Precedence (first-wins): profile.Lists (in declared order) ->
// profile's applicable default_lists (in declared order, unless the
// profile sets default_lists: false) -> profile.Base as the "*" pattern
// (always present, lowest precedence, only added if not already set by a
// list).
func Compile(policy registry.BashPolicy, profileName string) (map[string]Decision, error) {
	profile, ok := policy.Profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("bash profile %q not found", profileName)
	}

	result := map[string]Decision{}
	setIfAbsent := func(pattern string, d registry.Decision) {
		if _, exists := result[pattern]; !exists {
			result[pattern] = Decision(d)
		}
	}

	applyList := func(listName string) error {
		rules, ok := policy.Lists[listName]
		if !ok {
			return fmt.Errorf("bash profile %q references unknown list %q", profileName, listName)
		}
		for pattern, d := range rules {
			setIfAbsent(pattern, d)
		}
		return nil
	}

	for _, listName := range profile.Lists {
		if err := applyList(listName); err != nil {
			return nil, err
		}
	}

	useDefaults := profile.DefaultLists == nil || *profile.DefaultLists
	if useDefaults {
		for _, listName := range policy.DefaultLists {
			if err := applyList(listName); err != nil {
				return nil, err
			}
		}
	}

	setIfAbsent("*", profile.Base)

	return result, nil
}
