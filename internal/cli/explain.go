package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/bashpolicy"
	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/renderers"
)

func newExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain how a registry resolves for a specific scenario",
	}
	cmd.AddCommand(newExplainBashCmd())
	return cmd
}

func newExplainBashCmd() *cobra.Command {
	var registryFlag, command, targetFlag string

	cmd := &cobra.Command{
		Use:   "bash",
		Short: "Explain how each bash-capable target resolves a command",
		RunE: func(cmd *cobra.Command, args []string) error {
			if command == "" {
				return fmt.Errorf("explain bash: --command is required")
			}
			return runExplainBash(cmd.OutOrStdout(), registryFlag, targetFlag, command)
		},
	}
	cmd.Flags().StringVar(&registryFlag, "registry", "", "registry directory (default resolution: env/XDG/~/.config)")
	cmd.Flags().StringVar(&command, "command", "", "the bash command to resolve (required)")
	cmd.Flags().StringVar(&targetFlag, "target", "", "comma-separated renderer IDs (default: all bash-capable renderers)")
	return cmd
}

// bashExplainRow is one target's resolution of a single command.
type bashExplainRow struct {
	Target   string
	Decision bashpolicy.Decision // canonical decision — what "agree"/"disagree" compares
	Display  string              // per-harness display string (e.g. omp's "prompt" for Ask)
	Pattern  string
	Note     string
}

// runExplainBash loads the registry, resolves cmd against every
// bash-capable target's global bash profile (v1 has no per-agent context
// here — see the doc comment on bashProfileFor), and reports each
// target's winning pattern/decision plus whether every target agrees.
func runExplainBash(out io.Writer, registryFlag, targetFlag, command string) error {
	dir := ResolveRegistryDir(registryFlag)
	reg, verrs, _, err := registry.Load(dir)
	if err != nil {
		return err
	}
	if len(verrs) > 0 {
		return fmt.Errorf("registry has %d validation error(s); run `agentcfg validate` for details", len(verrs))
	}

	bashCapable := filterBashCapable(renderers.All())
	targets, err := filterRenderers(bashCapable, targetFlag)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Fprintln(out, "no targets support bash policy")
		return nil
	}

	var rows []bashExplainRow
	for _, r := range targets {
		row, matched, err := resolveBashRow(reg, r, command)
		if err != nil {
			fmt.Fprintf(out, "%s  error: %v\n", r.ID(), err)
			continue
		}
		if !matched {
			fmt.Fprintf(out, "%s  no pattern matches %q\n", r.ID(), command)
			continue
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		fmt.Fprintln(out, "no target resolved a decision for this command")
		return nil
	}

	tw := newTabWriter(out)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%q\t%s\n", row.Target, row.Display, row.Pattern, row.Note)
	}
	tw.Flush()

	if bashRowsAgree(rows) {
		fmt.Fprintln(out, "✓ agree")
	} else {
		fmt.Fprintln(out, "✗ disagree")
	}
	return nil
}

// filterBashCapable keeps only renderers declaring either bash-resolution
// capability (check the exact constant names in renderer.go, not guessed):
// CapBashUnorderedMap (opencode's shape) or CapBashOrderedList (omp's).
func filterBashCapable(all []render.Renderer) []render.Renderer {
	var out []render.Renderer
	for _, r := range all {
		for _, c := range r.Capabilities() {
			if c == render.CapBashUnorderedMap || c == render.CapBashOrderedList {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// resolveBashRow compiles r's relevant bash profile and resolves command
// against it using the shape r actually declares: MostSpecificMatch for
// CapBashUnorderedMap, FirstMatch over AsOrderedList for
// CapBashOrderedList. matched is false only when nothing in the compiled
// policy matches command (shouldn't happen given the "*" fallback every
// Compile'd profile carries, but handled explicitly rather than assumed).
func resolveBashRow(reg *registry.Registry, r render.Renderer, command string) (row bashExplainRow, matched bool, err error) {
	profile := bashProfileFor(reg, r.ID())
	compiled, err := bashpolicy.Compile(reg.Bash, profile)
	if err != nil {
		return bashExplainRow{}, false, err
	}

	row = bashExplainRow{Target: r.ID()}
	if hasCapability(r, render.CapBashUnorderedMap) {
		decision, pattern, ok := bashpolicy.MostSpecificMatch(compiled, command)
		if !ok {
			return bashExplainRow{}, false, nil
		}
		row.Decision = decision
		row.Pattern = pattern
		row.Note = "(unordered map, most-specific-match)"
	} else {
		ordered := bashpolicy.AsOrderedList(compiled)
		decision, pattern, index, ok := bashpolicy.FirstMatch(ordered, command)
		if !ok {
			return bashExplainRow{}, false, nil
		}
		row.Decision = decision
		row.Pattern = pattern
		row.Note = fmt.Sprintf("(ordered list, first-match, index %d of %d)", index, len(ordered))
	}
	row.Display = displayDecision(r.ID(), row.Decision)
	return row, true, nil
}

// bashProfileFor determines which bash profile is relevant for a target:
// harnesses.<id>.bash_profile from the registry if set, else "global". v1
// doesn't have per-command agent context here (that would require knowing
// which agent is "asking"), so this only ever shows the harness-wide
// profile's resolution.
func bashProfileFor(reg *registry.Registry, targetID string) string {
	if h, ok := reg.Harnesses[targetID]; ok && h.BashProfile != "" {
		return h.BashProfile
	}
	return "global"
}

// displayDecision translates the canonical Decision into a target's own
// vocabulary for display purposes only — omp calls Ask "prompt". This
// never affects the underlying Decision used for agreement comparison.
func displayDecision(targetID string, d bashpolicy.Decision) string {
	if targetID == "omp" && d == bashpolicy.Ask {
		return "prompt"
	}
	return string(d)
}

func hasCapability(r render.Renderer, want render.Capability) bool {
	for _, c := range r.Capabilities() {
		if c == want {
			return true
		}
	}
	return false
}

// bashRowsAgree reports whether every row resolved to the same canonical
// Decision — comparing Decision (not Display), since "ask" and omp's
// "prompt" are the same underlying decision, just displayed differently.
func bashRowsAgree(rows []bashExplainRow) bool {
	if len(rows) == 0 {
		return true
	}
	first := rows[0].Decision
	for _, r := range rows[1:] {
		if r.Decision != first {
			return false
		}
	}
	return true
}
