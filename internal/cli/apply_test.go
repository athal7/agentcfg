package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRunApplyCmd_BestEffortFalseStrictNoError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().BoolVar(new(bool), "best-effort", false, "")
	cmd.Flags().BoolVar(new(bool), "strict", false, "")

	// Explicitly set --best-effort=false and --strict.
	if err := cmd.Flags().Set("best-effort", "false"); err != nil {
		t.Fatalf("Set best-effort: %v", err)
	}
	if err := cmd.Flags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}

	opts := applyOptions{
		bestEffort: false,
	}

	// This combination should NOT error: --best-effort=false is not
	// a conflict with --strict — the user is explicitly disabling
	// best-effort, so strict mode is the only active mode.
	err := runApplyCmd(cmd, opts)
	// Two-step assertion: first confirm we got an error (no registry
	// means apply logic will fail), then confirm it is NOT the
	// mutual-exclusion error.
	if err == nil {
		t.Fatal("expected error from apply logic (no registry), got nil")
	}
	if err.Error() == "apply: --strict and --best-effort are mutually exclusive" {
		t.Error("--best-effort=false --strict should not be rejected as mutually exclusive")
	}
}

func TestRunApplyCmd_BestEffortFalseFlagOverridesEnv(t *testing.T) {
	t.Setenv("AGENTCFG_BEST_EFFORT", "1")

	cmd := &cobra.Command{}
	cmd.Flags().BoolVar(new(bool), "best-effort", false, "")

	// Explicitly set --best-effort=false while env says true.
	if err := cmd.Flags().Set("best-effort", "false"); err != nil {
		t.Fatalf("Set best-effort: %v", err)
	}

	opts := applyOptions{
		bestEffort: false,
	}

	err := runApplyCmd(cmd, opts)
	// The explicit --best-effort=false flag must override the env var,
	// so we go through the normal apply path which will fail (no
	// registry). If best-effort were incorrectly true, we'd get nil.
	if err == nil {
		t.Fatal("expected error from normal apply path (flag --best-effort=false should override env), got nil")
	}
}

func TestRunApplyCmd_BestEffortTrueStrictErrors(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().BoolVar(new(bool), "best-effort", false, "")
	cmd.Flags().BoolVar(new(bool), "strict", false, "")

	// Explicitly set --best-effort=true and --strict.
	if err := cmd.Flags().Set("best-effort", "true"); err != nil {
		t.Fatalf("Set best-effort: %v", err)
	}
	if err := cmd.Flags().Set("strict", "true"); err != nil {
		t.Fatalf("Set strict: %v", err)
	}

	opts := applyOptions{
		bestEffort: true,
	}

	err := runApplyCmd(cmd, opts)
	if err == nil {
		t.Fatal("expected error for --best-effort=true --strict")
	}
	if err.Error() != "apply: --strict and --best-effort are mutually exclusive" {
		t.Errorf("error = %q, want 'apply: --strict and --best-effort are mutually exclusive'", err)
	}
}
