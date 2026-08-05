package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/scope"
)

// targetPlan is one rendered Plan, tagged with which scope produced it and
// which target it's for. "global" plans are one-per-renderer (each
// renderer's own Render call); "project" is a single aggregate plan
// (scope.Project takes every targeted renderer at once and combines their
// RenderProject outputs/gaps into one Plan) — there's no per-renderer
// breakdown available for project scope, so Target is "project" for that
// one entry.
type targetPlan struct {
	Scope  string // "global" | "project"
	Target string // renderer ID for scope=global; "project" for scope=project
	Plan   *render.Plan
}

// filterRenderers narrows all down to the comma-separated IDs named in
// targetFlag. An empty targetFlag means "every renderer" (all is returned
// unchanged). An unknown ID is an error naming every unmatched ID, not a
// silent no-op.
func filterRenderers(all []render.Renderer, targetFlag string) ([]render.Renderer, error) {
	if strings.TrimSpace(targetFlag) == "" {
		return all, nil
	}

	wanted := map[string]bool{}
	for _, name := range strings.Split(targetFlag, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			wanted[name] = true
		}
	}

	var out []render.Renderer
	for _, r := range all {
		if wanted[r.ID()] {
			out = append(out, r)
			delete(wanted, r.ID())
		}
	}

	if len(wanted) > 0 {
		var unknown []string
		for name := range wanted {
			unknown = append(unknown, name)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown target(s): %s", strings.Join(unknown, ", "))
	}

	return out, nil
}

// validateScope reports an error for anything other than the three
// recognized --scope values.
func validateScope(s string) error {
	switch s {
	case "global", "project", "all":
		return nil
	default:
		return fmt.Errorf("invalid --scope %q (must be global, project, or all)", s)
	}
}

// buildPlans renders targets against reg for the requested scope(s).
// scope=all runs both global and project and returns both sets of plans,
// in that order, for combined reporting — they write to disjoint paths
// (user-scope vs. project-local config files) so there's no conflict in
// running both.
func buildPlans(reg *registry.Registry, targets []render.Renderer, scopeFlag, contextDir string) ([]targetPlan, error) {
	var plans []targetPlan

	if scopeFlag == "global" || scopeFlag == "all" {
		for _, r := range targets {
			p, err := r.Render(reg, render.Options{RegistryRoot: reg.RootDir})
			if err != nil {
				return nil, fmt.Errorf("rendering %s (global): %w", r.ID(), err)
			}
			plans = append(plans, targetPlan{Scope: "global", Target: r.ID(), Plan: p})
		}
	}

	if scopeFlag == "project" || scopeFlag == "all" {
		p, err := scope.Project(reg, targets, contextDir)
		if err != nil {
			return nil, fmt.Errorf("rendering project scope: %w", err)
		}
		plans = append(plans, targetPlan{Scope: "project", Target: "project", Plan: p})
	}

	return plans, nil
}

// countGaps totals every Gap across every plan — the shared basis for
// --strict on both render and apply.
func countGaps(plans []targetPlan) int {
	n := 0
	for _, p := range plans {
		n += len(p.Plan.Gaps)
	}
	return n
}
