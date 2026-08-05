package scope

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"

	"github.com/athal7/agentcfg/internal/contextres"
	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

// withResolveContext substitutes the package-level resolveContext seam for
// the duration of a test, restoring the original afterward. Tests that
// don't need a real git repo use this instead of shelling out to git.
func withResolveContext(t *testing.T, fn func(dir string) (*contextres.RemoteInfo, error)) {
	t.Helper()
	orig := resolveContext
	resolveContext = fn
	t.Cleanup(func() { resolveContext = orig })
}

// fakeRenderer is a render.Renderer that does NOT implement
// render.ProjectScopeRenderer.
type fakeRenderer struct {
	id string
}

func (f fakeRenderer) ID() string                        { return f.id }
func (f fakeRenderer) Capabilities() []render.Capability { return nil }
func (f fakeRenderer) Render(*registry.Registry, render.Options) (*render.Plan, error) {
	return &render.Plan{}, nil
}

// fakeOutput is a minimal render.Output for asserting aggregation order.
type fakeOutput struct{ tag string }

func (f fakeOutput) Describe() string { return "fake:" + f.tag }

// fakeProjectRenderer is a render.Renderer that DOES implement
// render.ProjectScopeRenderer. It records the classes map it was called
// with (so tests can assert what Project computed) and can be told to
// fail.
type fakeProjectRenderer struct {
	fakeRenderer
	captured *map[string]string
	err      error
}

func (f fakeProjectRenderer) RenderProject(classes map[string]string, _ *registry.Registry, _ string) (*render.Plan, error) {
	if f.captured != nil {
		*f.captured = classes
	}
	if f.err != nil {
		return nil, f.err
	}
	return &render.Plan{
		Outputs: []render.Output{fakeOutput{tag: f.id}},
	}, nil
}

func TestProject_ContextMatchOverridesSomeClasses(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return &contextres.RemoteInfo{Host: "github.com", Owner: "athal7"}, nil
	})

	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "a", "smol": "b", "big": "c"},
		Contexts: []registry.Context{
			{
				Match:        registry.ContextMatch{GitRemoteOwner: "athal7"},
				ModelClasses: map[string]string{"default": "override-a"},
			},
		},
	}

	var captured map[string]string
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}, captured: &captured},
	}

	plan, err := Project(reg, renderers, "/repo")
	if err != nil {
		t.Fatalf("Project returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	want := map[string]string{"default": "override-a", "smol": "b", "big": "c"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("got classes %v, want %v", captured, want)
	}

	// Original registry map must not have been mutated.
	if reg.ModelClasses["default"] != "a" {
		t.Errorf("reg.ModelClasses was mutated: %v", reg.ModelClasses)
	}
}

func TestProject_NoContextMatchUsesRegistryDefaults(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return &contextres.RemoteInfo{Host: "github.com", Owner: "someone-else"}, nil
	})

	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "a", "smol": "b"},
		Contexts: []registry.Context{
			{
				Match:        registry.ContextMatch{GitRemoteOwner: "athal7"},
				ModelClasses: map[string]string{"default": "override-a"},
			},
		},
	}

	var captured map[string]string
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}, captured: &captured},
	}

	plan, err := Project(reg, renderers, "/repo")
	if err != nil {
		t.Fatalf("Project returned error: %v", err)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1 (renderer must still be called): %+v", len(plan.Outputs), plan.Outputs)
	}

	want := map[string]string{"default": "a", "smol": "b"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("got classes %v, want registry defaults unmodified: %v", captured, want)
	}
}

func TestProject_NoGitRemoteAtAllUsesRegistryDefaults(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return nil, nil // contextres.Resolve's "not a repo / no origin" outcome
	})

	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "a"},
	}

	var captured map[string]string
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}, captured: &captured},
	}

	plan, err := Project(reg, renderers, "/repo")
	if err != nil {
		t.Fatalf("Project returned error: %v", err)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1 (renderer must still be called)", len(plan.Outputs))
	}
	want := map[string]string{"default": "a"}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("got classes %v, want %v", captured, want)
	}
}

func TestProject_FirstMatchingContextWins(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return &contextres.RemoteInfo{Host: "github.com", Owner: "athal7"}, nil
	})

	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "a"},
		Contexts: []registry.Context{
			{
				Match:        registry.ContextMatch{GitRemoteOwner: "athal7"},
				ModelClasses: map[string]string{"default": "first-match"},
			},
			{
				Match:        registry.ContextMatch{GitRemoteHost: "github.com"},
				ModelClasses: map[string]string{"default": "second-match"},
			},
		},
	}

	var captured map[string]string
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}, captured: &captured},
	}

	if _, err := Project(reg, renderers, "/repo"); err != nil {
		t.Fatalf("Project returned error: %v", err)
	}
	if captured["default"] != "first-match" {
		t.Errorf("got default class %q, want first-match (first Contexts entry wins)", captured["default"])
	}
}

func TestProject_RendererWithoutProjectScopeProducesSkipGap(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return nil, nil
	})

	reg := &registry.Registry{ModelClasses: map[string]string{"default": "a"}}
	renderers := []render.Renderer{
		fakeRenderer{id: "nonproject"},
	}

	plan, err := Project(reg, renderers, "/repo")
	if err != nil {
		t.Fatalf("Project returned error: %v", err)
	}
	if len(plan.Outputs) != 0 {
		t.Fatalf("got %d outputs, want 0", len(plan.Outputs))
	}
	if len(plan.Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1: %+v", len(plan.Gaps), plan.Gaps)
	}

	g := plan.Gaps[0]
	if g.Kind != render.GapSkip {
		t.Errorf("got kind %s, want skip", g.Kind)
	}
	if g.Capability != render.CapProjectModelPolicy {
		t.Errorf("got capability %s, want project_model_policy", g.Capability)
	}
	if g.Subject != "target:nonproject" {
		t.Errorf("got subject %q, want target:nonproject", g.Subject)
	}
	if g.Detail == "" {
		t.Errorf("got empty detail, want an explanation")
	}
}

func TestProject_MixedRenderersAggregateInOrder(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return nil, nil
	})

	reg := &registry.Registry{ModelClasses: map[string]string{"default": "a"}}
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}},
		fakeRenderer{id: "nonproject"},
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "omp"}},
	}

	plan, err := Project(reg, renderers, "/repo")
	if err != nil {
		t.Fatalf("Project returned error: %v", err)
	}

	if len(plan.Outputs) != 2 {
		t.Fatalf("got %d outputs, want 2: %+v", len(plan.Outputs), plan.Outputs)
	}
	if plan.Outputs[0].Describe() != "fake:opencode" || plan.Outputs[1].Describe() != "fake:omp" {
		t.Errorf("outputs out of order: %v", plan.Outputs)
	}
	if len(plan.Gaps) != 1 || plan.Gaps[0].Subject != "target:nonproject" {
		t.Fatalf("got gaps %+v, want exactly one skip gap for nonproject", plan.Gaps)
	}
}

func TestProject_RenderProjectErrorPropagates(t *testing.T) {
	withResolveContext(t, func(dir string) (*contextres.RemoteInfo, error) {
		return nil, nil
	})

	wantErr := errors.New("boom")
	reg := &registry.Registry{ModelClasses: map[string]string{"default": "a"}}
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}, err: wantErr},
	}

	plan, err := Project(reg, renderers, "/repo")
	if err == nil {
		t.Fatalf("expected an error, got nil (plan: %+v)", plan)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("got error %v, want it to wrap %v", err, wantErr)
	}
}

// TestProject_EndToEndWithRealGitRepo proves the real contextres.Resolve
// wiring works, not just the injected/mocked resolveContext path used by
// every other test in this file.
func TestProject_EndToEndWithRealGitRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-q", "-m", "init")
	runGit(t, dir, "remote", "add", "origin", "git@github.com:athal7/agentcfg.git")

	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "a"},
		Contexts: []registry.Context{
			{
				Match:        registry.ContextMatch{GitRemoteOwner: "athal7"},
				ModelClasses: map[string]string{"default": "real-repo-override"},
			},
		},
	}

	var captured map[string]string
	renderers := []render.Renderer{
		fakeProjectRenderer{fakeRenderer: fakeRenderer{id: "opencode"}, captured: &captured},
	}

	// Deliberately NOT overriding resolveContext: this exercises the real
	// contextres.Resolve against the temp repo created above.
	plan, err := Project(reg, renderers, dir)
	if err != nil {
		t.Fatalf("Project returned error: %v", err)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1", len(plan.Outputs))
	}
	if captured["default"] != "real-repo-override" {
		t.Errorf("got default class %q, want real-repo-override (real git wiring didn't resolve the context)", captured["default"])
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
