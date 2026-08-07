package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/apply"
)

// applyOptions extends renderOptions with flags specific to the apply command.
type applyOptions struct {
	renderOptions
}

// newApplyCmd creates the `apply` subcommand that renders and writes native
// harness configuration, with a --strict modifier.
func newApplyCmd() *cobra.Command {
	var opts applyOptions

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Render and write native harness configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApplyCmd(cmd, opts)
		},
	}
	registerRenderFlags(cmd, &opts.renderOptions)

	return cmd
}

// runApplyCmd dispatches to the normal apply path.
func runApplyCmd(cmd *cobra.Command, opts applyOptions) error {
	return runApplyNormal(cmd.OutOrStdout(), opts)
}

// runApplyNormal renders then applies exactly as `render` would preview,
// then writes real files/runs real commands via internal/apply. Real
// errors and non-zero exit codes on failure; --strict still means "any
// gap fails the command," same as `render`.
func runApplyNormal(out io.Writer, opts applyOptions) error {
	plans, err := loadAndBuildPlans(opts.registry, opts.target, opts.scope, opts.contextDir)
	if err != nil {
		return err
	}

	outcomes, applyErr := applyPlans(plans)

	switch {
	case opts.jsonOut:
		if err := printApplyJSON(out, outcomes); err != nil {
			return err
		}
	case opts.explain:
		printApplyExplain(out, outcomes)
	default:
		printApplySummary(out, outcomes)
	}

	if applyErr != nil {
		return applyErr
	}

	if opts.strict {
		if n := countGaps(plans); n > 0 {
			return fmt.Errorf("apply: %d gap(s) found (--strict)", n)
		}
	}
	return nil
}

// applyPlans calls internal/apply.Apply on every plan, collecting outcomes
// for reporting and joining every hard error (mirroring apply.Apply's own
// "attempt everything, join every error" contract at the multi-plan
// level).
func applyPlans(plans []targetPlan) ([]applyOutcome, error) {
	var outcomes []applyOutcome
	var errs []error
	for _, p := range plans {
		result, err := apply.Apply(p.Plan, apply.Options{})
		if err != nil {
			errs = append(errs, fmt.Errorf("applying %s (%s): %w", p.Target, p.Scope, err))
		}
		outcomes = append(outcomes, applyOutcomeFrom(p, result))
	}
	return outcomes, errors.Join(errs...)
}
