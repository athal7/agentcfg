package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExplainBash_ErrorTargetPrintedAndAgreeUnaffected(t *testing.T) {
	dir := t.TempDir()

	// bash.yaml: "global" profile is valid, "broken" references a list
	// that doesn't exist in policy.Lists — Compile will error.
	bashYAML := `bash:
  profiles:
    global:
      base: allow
    broken:
      base: deny
      lists:
        - nonexistent_list
`
	if err := os.WriteFile(filepath.Join(dir, "bash.yaml"), []byte(bashYAML), 0644); err != nil {
		t.Fatalf("write bash.yaml: %v", err)
	}

	// agentcfg.yaml: two harnesses — one uses "global" (succeeds),
	// one uses "broken" (fails to compile).
	agentcfgYAML := `imports:
  - bash.yaml
harnesses:
  opencode:
    bash_profile: global
  omp:
    bash_profile: broken
`
	if err := os.WriteFile(filepath.Join(dir, "agentcfg.yaml"), []byte(agentcfgYAML), 0644); err != nil {
		t.Fatalf("write agentcfg.yaml: %v", err)
	}

	var buf strings.Builder
	err := runExplainBash(&buf, dir, "", "ls -la")
	if err != nil {
		t.Fatalf("runExplainBash returned error: %v", err)
	}

	output := buf.String()

	// 1. The successful target's row must appear in the tabular output.
	if !strings.Contains(output, "opencode") {
		t.Error("output missing 'opencode' row (successful target should be shown)")
	}

	// 2. The error line for the failing target must be present.
	if !strings.Contains(output, "! omp: target excluded (resolution error)") {
		t.Errorf("output missing error line for omp:\n%s", output)
	}

	// 3. The agree/disagree conclusion must NOT be misleading.
	// With only one successful row (opencode), bashRowsAgree returns true
	// (single-element slice = trivially agree). The output should say
	// "agree" — which is correct because the single resolved target
	// trivially agrees with itself. The key assertion is that the error
	// line is present so the user knows omp was excluded.
	if !strings.Contains(output, "agree") {
		t.Errorf("output missing 'agree' conclusion:\n%s", output)
	}
}

func TestRunExplainBash_AllTargetsError(t *testing.T) {
	dir := t.TempDir()

	// Both harnesses reference non-existent profiles.
	bashYAML := `bash:
  profiles:
    global:
      base: allow
`
	if err := os.WriteFile(filepath.Join(dir, "bash.yaml"), []byte(bashYAML), 0644); err != nil {
		t.Fatalf("write bash.yaml: %v", err)
	}

	agentcfgYAML := `imports:
  - bash.yaml
harnesses:
  opencode:
    bash_profile: nonexistent
  omp:
    bash_profile: also_missing
`
	if err := os.WriteFile(filepath.Join(dir, "agentcfg.yaml"), []byte(agentcfgYAML), 0644); err != nil {
		t.Fatalf("write agentcfg.yaml: %v", err)
	}

	var buf strings.Builder
	err := runExplainBash(&buf, dir, "", "ls -la")
	if err != nil {
		t.Fatalf("runExplainBash returned error: %v", err)
	}

	output := buf.String()

	// No tabular rows should appear (no target resolved) — check for the
	// row-note markers rather than the target names, since the error
	// lines below legitimately mention both target names.
	if strings.Contains(output, "(unordered map") || strings.Contains(output, "(ordered list") {
		t.Errorf("output should not contain target rows when all fail:\n%s", output)
	}

	// Both error lines must be present.
	if !strings.Contains(output, "! opencode: target excluded (resolution error)") {
		t.Errorf("output missing error line for opencode:\n%s", output)
	}
	if !strings.Contains(output, "! omp: target excluded (resolution error)") {
		t.Errorf("output missing error line for omp:\n%s", output)
	}

	// No agree/disagree line should appear (no rows to compare).
	if strings.Contains(output, "agree") || strings.Contains(output, "disagree") {
		t.Errorf("output should not contain agree/disagree when no rows resolved:\n%s", output)
	}
}

func TestRunExplainBash_MultipleTargetsDisagreeWithOneError(t *testing.T) {
	// v1 only ships two bash-capable renderers (opencode, omp), so a
	// three-target "two disagree, one errors" scenario isn't reachable —
	// the error-exclusion behavior is already covered by
	// TestRunExplainBash_ErrorTargetPrintedAndAgreeUnaffected (partial
	// failure) and TestRunExplainBash_AllTargetsError (total failure).
	// This test instead confirms the disagree path itself is unaffected
	// by the error-handling changes: both real targets resolve
	// successfully to different decisions.
	dir := t.TempDir()

	// global: allow, strict: deny. opencode uses global, omp uses strict.
	bashYAML := `bash:
  profiles:
    global:
      base: allow
    strict:
      base: deny
`
	if err := os.WriteFile(filepath.Join(dir, "bash.yaml"), []byte(bashYAML), 0644); err != nil {
		t.Fatalf("write bash.yaml: %v", err)
	}

	agentcfgYAML := `imports:
  - bash.yaml
harnesses:
  opencode:
    bash_profile: global
  omp:
    bash_profile: strict
`
	if err := os.WriteFile(filepath.Join(dir, "agentcfg.yaml"), []byte(agentcfgYAML), 0644); err != nil {
		t.Fatalf("write agentcfg.yaml: %v", err)
	}

	var buf strings.Builder
	err := runExplainBash(&buf, dir, "", "ls -la")
	if err != nil {
		t.Fatalf("runExplainBash returned error: %v", err)
	}

	output := buf.String()

	// opencode and omp should both appear (they resolved).
	if !strings.Contains(output, "opencode") {
		t.Error("output missing 'opencode' row")
	}
	if !strings.Contains(output, "omp") {
		t.Error("output missing 'omp' row")
	}

	// No error lines should appear — both targets resolved cleanly.
	if strings.Contains(output, "target excluded") {
		t.Errorf("output should not contain any excluded-target line:\n%s", output)
	}

	// opencode=allow, omp=deny → disagree.
	if !strings.Contains(output, "disagree") {
		t.Errorf("output should say 'disagree' (allow vs deny):\n%s", output)
	}
}
