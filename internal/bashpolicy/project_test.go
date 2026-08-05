package bashpolicy

import "testing"

func TestAsMap_ReturnsUnchanged(t *testing.T) {
	in := map[string]Decision{
		"git status*": Allow,
		"*":           Deny,
	}

	got := AsMap(in)

	if len(got) != len(in) {
		t.Fatalf("AsMap() = %v, want %v", got, in)
	}
	for k, v := range in {
		if got[k] != v {
			t.Errorf("AsMap()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestAsOrderedList_MostSpecificFirst(t *testing.T) {
	compiled := map[string]Decision{
		"*":           Allow,
		"git status*": Allow,
		"git status":  Deny,
		"git commit*": Ask,
	}

	got := AsOrderedList(compiled)

	if len(got) != len(compiled) {
		t.Fatalf("AsOrderedList() has %d rules, want %d", len(got), len(compiled))
	}

	// "git status" (exact) must come before "git status*" (prefix) —
	// the specific defect the refined heuristic fixes.
	exactIdx, prefixIdx := -1, -1
	for i, r := range got {
		if r.Pattern == "git status" {
			exactIdx = i
		}
		if r.Pattern == "git status*" {
			prefixIdx = i
		}
	}
	if exactIdx == -1 || prefixIdx == -1 {
		t.Fatalf("AsOrderedList() missing expected patterns: %v", got)
	}
	if exactIdx >= prefixIdx {
		t.Errorf("AsOrderedList() ordered %q at %d, %q at %d; want exact before prefix", "git status", exactIdx, "git status*", prefixIdx)
	}

	// "*" is the least specific pattern and must sort last.
	if got[len(got)-1].Pattern != "*" {
		t.Errorf("AsOrderedList() last rule = %+v, want pattern \"*\" last", got[len(got)-1])
	}

	// Every rule's Decision must match the input map (projection doesn't
	// change decisions, only order).
	for _, r := range got {
		if r.Decision != compiled[r.Pattern] {
			t.Errorf("AsOrderedList() rule %+v has wrong decision, map has %q", r, compiled[r.Pattern])
		}
	}
}

func TestAsOrderedList_Deterministic(t *testing.T) {
	compiled := map[string]Decision{
		"a*": Allow,
		"b*": Deny,
		"c*": Ask,
	}

	first := AsOrderedList(compiled)
	for i := 0; i < 10; i++ {
		again := AsOrderedList(compiled)
		if len(again) != len(first) {
			t.Fatalf("AsOrderedList() length changed across calls")
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("AsOrderedList() is not deterministic: call 1 = %v, call %d = %v", first, i+2, again)
			}
		}
	}
}
