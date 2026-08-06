package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/athal7/agentcfg/internal/apply"
	"github.com/athal7/agentcfg/internal/render"
)

// printPlanSummary is the baseline `render` output with no --explain/--json
// modifier: one line per target naming how many outputs are planned and
// how many gaps were found.
func printPlanSummary(w io.Writer, plans []targetPlan) {
	if len(plans) == 0 {
		fmt.Fprintln(w, "no targets selected")
		return
	}
	for _, p := range plans {
		fmt.Fprintf(w, "%s (%s): %d output(s) planned, %d gap(s)\n", p.Target, p.Scope, len(p.Plan.Outputs), len(p.Plan.Gaps))
	}
}

// printPlanExplain is --explain's verbose form: every output's Describe()
// and every gap's full detail, per target.
func printPlanExplain(w io.Writer, plans []targetPlan) {
	if len(plans) == 0 {
		fmt.Fprintln(w, "no targets selected")
		return
	}
	for _, p := range plans {
		fmt.Fprintf(w, "== %s (%s) ==\n", p.Target, p.Scope)
		if len(p.Plan.Outputs) == 0 {
			fmt.Fprintln(w, "  (no outputs)")
		}
		for _, o := range p.Plan.Outputs {
			fmt.Fprintf(w, "  - %s\n", o.Describe())
		}
		if len(p.Plan.Gaps) == 0 {
			fmt.Fprintln(w, "  (no gaps)")
		}
		for _, g := range p.Plan.Gaps {
			fmt.Fprintf(w, "  ! [%s] %s %s\n", g.Kind, g.Capability, g.Subject)
			fmt.Fprintf(w, "      %s\n", g.Detail)
		}
	}
}

// outputTypeName returns a stable, harness-agnostic string for the JSON
// "type" field. Using a switch instead of fmt.Sprintf("%T") so the JSON
// output doesn't depend on Go's internal type names (which would change
// if a struct is renamed or moved).
func outputTypeName(o render.Output) string {
	switch o.(type) {
	case render.WriteFile:
		return "write_file"
	case render.MergeJSON:
		return "merge_json"
	case render.MergeYAML:
		return "merge_yaml"
	case render.MergeTOML:
		return "merge_toml"
	case render.RebuildDir:
		return "rebuild_dir"
	case render.RunCommand:
		return "run_command"
	default:
		return "unknown"
	}
}

// jsonOutput is one Output's --json representation: a stable type name,
// its Path (or Dir, for RebuildDir) where the type has one, and its
// Describe() string. RunCommand has no path at all — Path is simply
// omitted for it.
type jsonOutput struct {
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"`
	Describe string `json:"describe"`
}

// toJSONOutput converts a render.Output into its --json representation,
// mapping the concrete type to a stable JSON "type" field and extracting
// the path where applicable.
func toJSONOutput(o render.Output) jsonOutput {
	jo := jsonOutput{Type: outputTypeName(o), Describe: o.Describe()}
	switch v := o.(type) {
	case render.WriteFile:
		jo.Path = v.Path
	case render.MergeJSON:
		jo.Path = v.Path
	case render.MergeYAML:
		jo.Path = v.Path
	case render.MergeTOML:
		jo.Path = v.Path
	case render.RebuildDir:
		jo.Path = v.Dir
	}
	return jo
}

// jsonGap is one Gap's --json representation.
type jsonGap struct {
	Kind       string `json:"kind"`
	Capability string `json:"capability"`
	Subject    string `json:"subject"`
	Detail     string `json:"detail"`
}

// toJSONGap converts a render.Gap into its --json representation.
func toJSONGap(g render.Gap) jsonGap {
	return jsonGap{
		Kind:       string(g.Kind),
		Capability: string(g.Capability),
		Subject:    g.Subject,
		Detail:     g.Detail,
	}
}

// jsonPlanTarget is one target's --json entry. The task spec describes a
// single plan's JSON shape as {outputs, gaps}; since render/apply can
// report on multiple targets and scopes at once (--target a,b, --scope
// all), this wraps that shape in a "targets" array, one entry per
// targetPlan, each still exposing top-level "outputs"/"gaps" keys matching
// the spec's shape.
type jsonPlanTarget struct {
	Target  string       `json:"target"`
	Scope   string       `json:"scope"`
	Outputs []jsonOutput `json:"outputs"`
	Gaps    []jsonGap    `json:"gaps"`
}

// jsonPlanReport is the top-level JSON shape for render's --json output.
type jsonPlanReport struct {
	Targets []jsonPlanTarget `json:"targets"`
}

// printPlanJSON encodes plans as the jsonPlanReport JSON shape to w.
func printPlanJSON(w io.Writer, plans []targetPlan) error {
	report := jsonPlanReport{Targets: []jsonPlanTarget{}}
	for _, p := range plans {
		jt := jsonPlanTarget{Target: p.Target, Scope: p.Scope, Outputs: []jsonOutput{}, Gaps: []jsonGap{}}
		for _, o := range p.Plan.Outputs {
			jt.Outputs = append(jt.Outputs, toJSONOutput(o))
		}
		for _, g := range p.Plan.Gaps {
			jt.Gaps = append(jt.Gaps, toJSONGap(g))
		}
		report.Targets = append(report.Targets, jt)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// applyOutcome is one target's actual apply result, alongside the Gaps its
// Plan carried (apply's --strict checks the same gap count render's does).
type applyOutcome struct {
	Scope   string
	Target  string
	Applied []string
	Skipped []string
	Gaps    []render.Gap
}

// applyOutcomeFrom builds an applyOutcome from a targetPlan and its apply.Result.
func applyOutcomeFrom(p targetPlan, result apply.Result) applyOutcome {
	return applyOutcome{
		Scope:   p.Scope,
		Target:  p.Target,
		Applied: result.Applied,
		Skipped: result.Skipped,
		Gaps:    p.Plan.Gaps,
	}
}

// printApplySummary writes a one-line-per-target summary (applied/skipped/gap counts) to w.
func printApplySummary(w io.Writer, outcomes []applyOutcome) {
	if len(outcomes) == 0 {
		fmt.Fprintln(w, "no targets selected")
		return
	}
	for _, o := range outcomes {
		fmt.Fprintf(w, "%s (%s): %d applied, %d skipped, %d gap(s)\n", o.Target, o.Scope, len(o.Applied), len(o.Skipped), len(o.Gaps))
	}
}

// printApplyExplain writes a detailed per-target breakdown (each applied/skipped output, each gap) to w.
func printApplyExplain(w io.Writer, outcomes []applyOutcome) {
	if len(outcomes) == 0 {
		fmt.Fprintln(w, "no targets selected")
		return
	}
	for _, o := range outcomes {
		fmt.Fprintf(w, "== %s (%s) ==\n", o.Target, o.Scope)
		for _, a := range o.Applied {
			fmt.Fprintf(w, "  + %s\n", a)
		}
		for _, s := range o.Skipped {
			fmt.Fprintf(w, "  - %s\n", s)
		}
		for _, g := range o.Gaps {
			fmt.Fprintf(w, "  ! [%s] %s %s\n", g.Kind, g.Capability, g.Subject)
			fmt.Fprintf(w, "      %s\n", g.Detail)
		}
	}
}

// jsonApplyTarget is the JSON shape for one target's apply outcome.
type jsonApplyTarget struct {
	Target  string    `json:"target"`
	Scope   string    `json:"scope"`
	Applied []string  `json:"applied"`
	Skipped []string  `json:"skipped"`
	Gaps    []jsonGap `json:"gaps"`
}

// jsonApplyReport is the top-level JSON shape for apply's --json output.
type jsonApplyReport struct {
	Targets []jsonApplyTarget `json:"targets"`
}

// printApplyJSON encodes outcomes as the jsonApplyReport JSON shape to w.
func printApplyJSON(w io.Writer, outcomes []applyOutcome) error {
	report := jsonApplyReport{Targets: []jsonApplyTarget{}}
	for _, o := range outcomes {
		applied := o.Applied
		if applied == nil {
			applied = []string{}
		}
		skipped := o.Skipped
		if skipped == nil {
			skipped = []string{}
		}
		jt := jsonApplyTarget{Target: o.Target, Scope: o.Scope, Applied: applied, Skipped: skipped, Gaps: []jsonGap{}}
		for _, g := range o.Gaps {
			jt.Gaps = append(jt.Gaps, toJSONGap(g))
		}
		report.Targets = append(report.Targets, jt)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// newTabWriter is the shared plain-text table writer used by `doctor` and
// `explain bash`.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}
