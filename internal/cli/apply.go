package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/apply"
)

const defaultBestEffortTimeout = 5 * time.Second

type applyOptions struct {
	renderOptions
	bestEffort bool
	timeout    time.Duration
}

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
	cmd.Flags().BoolVar(&opts.bestEffort, "best-effort", false, "swallow every error and always exit 0 (also via AGENTCFG_BEST_EFFORT=1)")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", defaultBestEffortTimeout, "abandon and exit 0 after this long (only meaningful with --best-effort)")

	return cmd
}

// runApplyCmd resolves the --strict/--best-effort interaction, then
// dispatches to the best-effort or normal apply path.
//
// Mutual exclusion rule: --strict and --best-effort are contradictory (one
// says "fail loudly on any gap", the other says "never fail, ever") and
// explicitly setting both via flags is an error. AGENTCFG_BEST_EFFORT=1 is
// different: it's an ambient default (e.g. set in a shell profile for
// every invocation), not a one-off explicit choice, so an explicit
// --strict flag on a single invocation overrides it rather than erroring —
// the more specific, more recently-stated intent (the flag on this
// command line) wins over the broader ambient one (the env var).
func runApplyCmd(cmd *cobra.Command, opts applyOptions) error {
	bestEffortFromFlag := cmd.Flags().Changed("best-effort")
	strictFromFlag := cmd.Flags().Changed("strict")
	bestEffortFromEnv := os.Getenv("AGENTCFG_BEST_EFFORT") == "1"

	bestEffort := opts.bestEffort || bestEffortFromEnv

	switch {
	case bestEffortFromFlag && strictFromFlag:
		return fmt.Errorf("apply: --strict and --best-effort are mutually exclusive")
	case !bestEffortFromFlag && bestEffortFromEnv && strictFromFlag:
		bestEffort = false
	}

	if bestEffort {
		runApplyBestEffort(opts)
		return nil
	}
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

// runApplyBestEffort is the silent contract: NOTHING is ever written to
// stdout, the process always behaves as a success (the caller always
// treats this as exit 0 — see runApplyCmd), and every failure mode —
// registry load failure, no git repo, no origin remote, an unparseable
// remote, no matching context, an apply error, or a panic anywhere in the
// work — is swallowed. Diagnostics go to stderr ONLY when
// AGENTCFG_DEBUG=1. The work runs in a goroutine so --timeout can actually
// abandon it: on timeout we stop waiting and return, we don't (can't,
// without deeper plumbing) forcibly kill in-flight I/O — "abandon" per the
// spec means the CLI stops waiting and exits, accepting that a detached
// write may or may not still complete before the process exits.
func runApplyBestEffort(opts applyOptions) {
	defer func() {
		if r := recover(); r != nil {
			debugf("agentcfg apply: panic recovered: %v", r)
		}
	}()

	timeout := opts.timeout
	if timeout <= 0 {
		timeout = defaultBestEffortTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				debugf("agentcfg apply: panic recovered: %v", r)
			}
		}()

		plans, err := loadAndBuildPlans(opts.registry, opts.target, opts.scope, opts.contextDir)
		if err != nil {
			debugf("agentcfg apply: %v", err)
			return
		}
		if _, err := applyPlans(plans); err != nil {
			debugf("agentcfg apply: %v", err)
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
		debugf("agentcfg apply: timed out after %s, abandoning", timeout)
	}
}

// debugf writes a diagnostic line to stderr only when AGENTCFG_DEBUG=1 is
// set — the one sanctioned leak in --best-effort's otherwise-silent
// contract, and it's stderr-only, never stdout.
func debugf(format string, args ...any) {
	if os.Getenv("AGENTCFG_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}
