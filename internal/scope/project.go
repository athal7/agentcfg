// Package scope orchestrates project-scope rendering: resolving a
// directory's git remote into a registry Context (if any matches),
// merging that context's model-class overrides over the registry's
// defaults, and asking every capable renderer to project that onto a
// directory-local config file.
package scope

import (
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
var resolveContext = contextres.Resolve

// Project resolves dir's context (if any) against reg.Contexts, merges the
// matched context's ModelClasses over a copy of the registry's own
// default model classes (partial, per-key override — a context need not
// name every class), and calls RenderProject on every renderer in
// renderers that implements render.ProjectScopeRenderer. A renderer that
// doesn't implement the interface is not an error: Project appends a
// render.Gap for it instead and moves on.
//
// "No git remote at all" and "a git remote that matches no configured
// Context" are deliberately NOT distinguished here — both simply mean
// "render project scope using the registry's own default classes,
// unmodified." This is a normal outcome, not a failure: every
// ProjectScopeRenderer is still called in both cases, with the plain
// registry defaults. Only an actual error from a renderer's RenderProject
// call propagates as a Go error — a renderer that doesn't support project
// scope at all is a Gap, not a failure.
func Project(reg *registry.Registry, renderers []render.Renderer, dir string) (*render.Plan, error) {
	classes := resolveClasses(reg, dir)

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

		sub, err := pr.RenderProject(classes, reg, dir)
		if err != nil {
			return nil, fmt.Errorf("scope: rendering project scope for %s: %w", r.ID(), err)
		}
		plan.Outputs = append(plan.Outputs, sub.Outputs...)
		plan.Gaps = append(plan.Gaps, sub.Gaps...)
	}

	return plan, nil
}

// resolveClasses determines the effective model-class map for dir: a copy
// of the registry's own defaults (reg.ModelClasses is never mutated),
// with the first matching Context's ModelClasses overlaid key-by-key on
// top. reg.Contexts is checked in order; the first match wins.
func resolveClasses(reg *registry.Registry, dir string) map[string]string {
	classes := make(map[string]string, len(reg.ModelClasses))
	for k, v := range reg.ModelClasses {
		classes[k] = v
	}

	remote, err := resolveContext(dir)
	if err != nil || remote == nil {
		return classes
	}

	for _, c := range reg.Contexts {
		if !c.Matches(remote.Host, remote.Owner) {
			continue
		}
		for k, v := range c.ModelClasses {
			classes[k] = v
		}
		break
	}

	return classes
}
