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
	// We expect an error from the actual apply logic (no registry),
	// NOT from the mutual-exclusion check. If the mutual-exclusion
	// check fires, the error message will contain "mutually exclusive".
	if err != nil && err.Error() == "apply: --strict and --best-effort are mutually exclusive" {
		t.Error("--best-effort=false --strict should not be rejected as mutually exclusive")
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
