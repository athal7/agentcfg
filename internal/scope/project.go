// Package scope orchestrates project-scope rendering: resolving a
// directory's git remote into a registry Context (if any matches),
// merging that context's model-class overrides over the registry's
// defaults, and asking every capable renderer to project that onto a
// directory-local config file.
package scope

import (
	"context"
	"fmt"

	"github.com/athal7/agentcfg/internal/contextres"
	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

// resolveContext is a seam over contextres.Resolve so tests can substitute
// a fake git-remote resolution without shelling out to git for every case
// (the "context matches"/"no context matches" cases don't need a real
// repo; only the end-to-end wiring test does). Production code always
// uses the real contextres.Resolve — this var exists purely for this
// package's own tests, which reassign it and restore it via t.Cleanup.
var resolveContext = func(ctx context.Context, dir string) (*contextres.RemoteInfo, error) {
	return contextres.Resolve(ctx, dir)
}

// Project resolves dir's context (if any) against reg.Contexts, merges the
// matched context's ModelClasses over each renderer's effective base
// (root model_classes + harness-level model_classes overrides), and calls
// RenderProject on every renderer in renderers that implements
// render.ProjectScopeRenderer. Resolution order per renderer:
//
//  1. Root model_classes (global base)
//  2. HarnessConfig.ModelClasses for that renderer's harness (harness override)
//  3. First matching Context's ModelClasses (project-specific, most specific)
//
// A renderer that doesn't implement the interface is not an error: Project
// appends a render.Gap for it instead and moves on.
//
// "No git remote at all" and "a git remote that matches no configured
// Context" are deliberately NOT distinguished here — both simply mean
// "render project scope using the registry and harness defaults,
// unmodified." This is a normal outcome, not a failure.
func Project(reg *registry.Registry, renderers []render.Renderer, dir string) (*render.Plan, error) {
	contextDelta := resolveContextDelta(reg, dir)

	plan := &render.Plan{}
	for _, r := range renderers {
		pr, ok := r.(render.ProjectScopeRenderer)
		if !ok {
			plan.Gaps = append(plan.Gaps, render.Gap{
				Kind:       render.GapSkip,
				Capability: render.CapProjectModelPolicy,
				Subject:    fmt.Sprintf("target:%s", r.ID()),
				Detail:     fmt.Sprintf("%s has no project scope", r.ID()),
			})
			continue
		}

		classes := classesForRenderer(reg, r.ID(), contextDelta)
		sub, err := pr.RenderProject(classes, reg, dir)
		if err != nil {
			return nil, fmt.Errorf("scope: rendering project scope for %s: %w", r.ID(), err)
		}
		plan.Outputs = append(plan.Outputs, sub.Outputs...)
		plan.Gaps = append(plan.Gaps, sub.Gaps...)
	}

	return plan, nil
}

// classesForRenderer builds the effective class map for one renderer:
// root model_classes, then harness overrides, then context delta.
func classesForRenderer(reg *registry.Registry, harness string, contextDelta map[string]string) map[string]string {
	classes := reg.EffectiveModelClasses(harness)
	for k, v := range contextDelta {
		classes[k] = v
	}
	return classes
}

// resolveContextDelta returns only the matching Context's ModelClasses (the
// per-project delta), or nil when no context matches or the remote is
// unresolvable. Callers overlay this onto their own effective base.
func resolveContextDelta(reg *registry.Registry, dir string) map[string]string {
	remote, err := resolveContext(context.Background(), dir)
	if err != nil || remote == nil {
		return nil
	}
	for _, c := range reg.Contexts {
		if !c.Matches(remote.Host, remote.Owner) {
			continue
		}
		return c.ModelClasses
	}
	return nil
}
