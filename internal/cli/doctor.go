package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/renderers"
)

// allCapabilities is every render.Capability constant defined in
// internal/render/renderer.go. Go has no runtime enum introspection, so
// this list is maintained by hand — keep it in sync whenever a Cap*
// constant is added there. TestAllCapabilities_MatchesRendererGoConstCount
// is a tripwire that fails loudly (naming the exact count mismatch) if
// this list falls out of sync, rather than doctor silently under-reporting
// a new capability forever.
var allCapabilities = []render.Capability{
	render.CapAgentDefinitions,
	render.CapPrimaryAgent,
	render.CapPromptAppend,
	render.CapPromptFileRef,
	render.CapAgentSteps,
	render.CapAgentTaskPermission,
	render.CapModelLiteralBinding,
	render.CapModelClassBinding,
	render.CapModelAliasOnly,
	render.CapBashUnorderedMap,
	render.CapBashOrderedList,
	render.CapBashBucketedLists,
	render.CapBashCoarseMode,
	render.CapBashInteriorGlob,
	render.CapPerAgentBashPolicy,
	render.CapGlobalBashPolicy,
	render.CapExternalDirectory,
	render.CapMCPLocalTransport,
	render.CapMCPToolGlobs,
	render.CapMCPPerToolAsk,
	render.CapProjectModelPolicy,
}

func newDoctorCmd() *cobra.Command {
	var registryFlag string
	var markdown bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Print the renderer capability matrix and concrete registry gaps",
		RunE: func(cmd *cobra.Command, args []string) error {
			runDoctor(cmd.OutOrStdout(), registryFlag, markdown)
			return nil
		},
	}
	cmd.Flags().StringVar(&registryFlag, "registry", "", "registry directory (default resolution: env/XDG/~/.config)")
	cmd.Flags().BoolVar(&markdown, "markdown", false, "print the capability matrix as a markdown table")
	return cmd
}

// runDoctor never returns an error: it's diagnostic, not enforcement
// (--strict on render/apply is the enforcement mechanism). A registry that
// fails to load or has validation errors still gets the capability matrix
// printed; the registry-gap section just explains why it couldn't run.
func runDoctor(out io.Writer, registryFlag string, markdown bool) {
	targets := renderers.All()
	printCapabilityMatrix(out, targets, allCapabilities, markdown)
	fmt.Fprintln(out)

	dir := ResolveRegistryDir(registryFlag)
	reg, verrs, _, err := registry.Load(dir)
	if err != nil {
		fmt.Fprintf(out, "registry gaps: could not load registry at %s: %v\n", dir, err)
		return
	}
	if len(verrs) > 0 {
		fmt.Fprintf(out, "registry gaps: registry has %d validation error(s), skipping gap analysis:\n", len(verrs))
		for _, e := range verrs {
			fmt.Fprintf(out, "  - %s\n", e.Message)
		}
		return
	}

	printRegistryGaps(out, targets, reg)
}

func printCapabilityMatrix(w io.Writer, targets []render.Renderer, caps []render.Capability, markdown bool) {
	declared := make([]map[render.Capability]bool, len(targets))
	for i, r := range targets {
		m := map[render.Capability]bool{}
		for _, c := range r.Capabilities() {
			m[c] = true
		}
		declared[i] = m
	}

	mark := func(i int, c render.Capability) string {
		if declared[i][c] {
			return "✓"
		}
		return "✗"
	}

	if markdown {
		fmt.Fprint(w, "| capability |")
		for _, r := range targets {
			fmt.Fprintf(w, " %s |", r.ID())
		}
		fmt.Fprintln(w)
		fmt.Fprint(w, "|---|")
		for range targets {
			fmt.Fprint(w, "---|")
		}
		fmt.Fprintln(w)
		for _, c := range caps {
			fmt.Fprintf(w, "| %s |", c)
			for i := range targets {
				fmt.Fprintf(w, " %s |", mark(i, c))
			}
			fmt.Fprintln(w)
		}
		return
	}

	tw := newTabWriter(w)
	fmt.Fprint(tw, "CAPABILITY")
	for _, r := range targets {
		fmt.Fprintf(tw, "\t%s", r.ID())
	}
	fmt.Fprintln(tw)
	for _, c := range caps {
		fmt.Fprintf(tw, "%s", c)
		for i := range targets {
			fmt.Fprintf(tw, "\t%s", mark(i, c))
		}
		fmt.Fprintln(tw)
	}
	tw.Flush()
}

func printRegistryGaps(w io.Writer, targets []render.Renderer, reg *registry.Registry) {
	for _, r := range targets {
		gaps := render.DetectGaps(reg, r.Capabilities())
		if len(gaps) == 0 {
			fmt.Fprintf(w, "%s: no gaps\n", r.ID())
			continue
		}
		for _, g := range gaps {
			fmt.Fprintf(w, "%s  %s  %s  %s\n", r.ID(), g.Kind, g.Capability, g.Subject)
			fmt.Fprintf(w, "    %s\n", g.Detail)
		}
	}
}
