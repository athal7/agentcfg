package renderers

import "testing"

func TestAll_RegistersEveryBuiltinRenderer(t *testing.T) {
	all := All()

	if len(all) != 3 {
		t.Fatalf("got %d renderers, want 3: %v", len(all), all)
	}

	seen := map[string]bool{}
	for _, r := range all {
		id := r.ID()
		if seen[id] {
			t.Errorf("renderer id %q registered more than once", id)
		}
		seen[id] = true
	}
	for _, want := range []string{"opencode", "omp", "codex"} {
		if !seen[want] {
			t.Errorf("expected renderer id %q to be registered", want)
		}
	}
}
