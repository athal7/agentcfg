package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/renderers"
)

// capabilityGroup gives a user-facing heading to closely related
// capabilities in Markdown output.
type capabilityGroup struct {
	Title        string
	Capabilities []render.Capability
}

// capabilityGroups keeps the Markdown capability matrix navigable while
// preserving a complete, ordered view for the plain-text doctor output.
var capabilityGroups = []capabilityGroup{
	{
		Title: "Agents",
		Capabilities: []render.Capability{
			render.CapAgentDefinitions,
			render.CapPrimaryAgent,
			render.CapComposeIntoPrimary,
			render.CapPromptAppend,
			render.CapPromptFileRef,
			render.CapAgentSteps,
		},
	},
	{
		Title: "Permissions",
		Capabilities: []render.Capability{
			render.CapPrimaryAgentToolPermission,
			render.CapAgentTaskPermission,
			render.CapExternalDirectory,
		},
	},
	{
		Title: "Model bindings",
		Capabilities: []render.Capability{
			render.CapModelLiteralBinding,
			render.CapModelClassBinding,
		},
	},
	{
		Title: "Bash policies",
		Capabilities: []render.Capability{
			render.CapBashUnorderedMap,
			render.CapBashOrderedList,
			render.CapBashInteriorGlob,
			render.CapPerAgentBashPolicy,
			render.CapGlobalBashPolicy,
		},
	},
	{
		Title: "MCP servers",
		Capabilities: []render.Capability{
			render.CapMCPLocalTransport,
			render.CapMCPRemoteTransport,
			render.CapMCPToolGlobs,
			render.CapMCPPerToolAsk,
		},
	},
	{
		Title: "Project policy",
		Capabilities: []render.Capability{
			render.CapProjectModelPolicy,
		},
	},
	{
		Title: "Commands",
		Capabilities: []render.Capability{
			render.CapCustomCommands,
			render.CapStructuredWorkflowCommand,
		},
	},
}

// allCapabilities is every render.Capability constant defined in
// internal/render/renderer.go. It is flattened from capabilityGroups so the
// plain-text and Markdown doctor outputs report the same capabilities. Go has
// no runtime enum introspection; TestAllCapabilities_MatchesRendererGoConstCount
// fails if a new constant is not assigned to a group.
var allCapabilities = flattenCapabilityGroups(capabilityGroups)

func flattenCapabilityGroups(groups []capabilityGroup) []render.Capability {
	var count int
	for _, group := range groups {
		count += len(group.Capabilities)
	}

	caps := make([]render.Capability, 0, count)
	for _, group := range groups {
		caps = append(caps, group.Capabilities...)
	}
	return caps
}

// newDoctorCmd builds the doctor command.
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

	printRegistryGaps(out, targets, reg, markdown)
}

// printCapabilityMatrix writes a table of which capabilities each target
// renderer supports. A capability with no direct declaration but a
// declared render.SubstituteOf counterpart renders "≈" instead of "✗":
// the underlying registry feature is still fully expressed, just via a
// different harness-native mechanism (e.g. omp's prompt_append instead of
// opencode's primary_agent default-agent key) — not a real gap.
func printCapabilityMatrix(w io.Writer, targets []render.Renderer, caps []render.Capability, markdown bool) {
	declared := make([]map[render.Capability]bool, len(targets))
	for i, r := range targets {
		m := map[render.Capability]bool{}
		for _, c := range r.Capabilities() {
			m[c] = true
		}
		declared[i] = m
	}

	type substitution struct {
		target string
		cap    render.Capability
		via    render.Capability
	}
	var substituted []substitution

	mark := func(i int, c render.Capability) string {
		if declared[i][c] {
			return "✓"
		}
		if via, ok := render.SubstituteOf(c); ok && declared[i][via] {
			substituted = append(substituted, substitution{targets[i].ID(), c, via})
			return "≈"
		}
		return "✗"
	}

	if markdown {
		for _, group := range capabilityGroups {
			fmt.Fprintf(w, "## %s\n\n", group.Title)
			printMarkdownCapabilityTable(w, targets, group.Capabilities, mark)
			fmt.Fprintln(w)
		}
	} else {
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

	if len(substituted) == 0 {
		return
	}

	if !markdown {
		fmt.Fprintln(w)
	}
	if markdown {
		fmt.Fprintln(w, "## Equivalent capabilities")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "An `≈` indicates the same registry feature uses a different native mechanism.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| harness | capability | expressed via |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, s := range substituted {
			fmt.Fprintf(w, "| %s | %s | %s |\n", s.target, s.cap, s.via)
		}
		return
	}

	fmt.Fprintln(w, "≈ = same underlying feature, expressed via a different harness-native mechanism (not a gap):")
	for _, s := range substituted {
		fmt.Fprintf(w, "%s  %s — via %s\n", s.target, s.cap, s.via)
	}
}

func printMarkdownCapabilityTable(w io.Writer, targets []render.Renderer, caps []render.Capability, mark func(int, render.Capability) string) {
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
}

// printRegistryGaps writes each target's rendering gaps against the given registry.
func printRegistryGaps(w io.Writer, targets []render.Renderer, reg *registry.Registry, markdown bool) {
	if markdown {
		fmt.Fprintln(w, "## Registry gaps")
		for _, r := range targets {
			gaps := render.DetectGaps(reg, r.Capabilities())
			fmt.Fprintf(w, "\n### %s\n\n", r.ID())
			if len(gaps) == 0 {
				fmt.Fprintln(w, "No gaps.")
				continue
			}
			for _, g := range gaps {
				fmt.Fprintf(w, "- **%s `%s` for `%s`**. %s\n", markdownGapKind(g.Kind), g.Capability, g.Subject, g.Detail)
			}
		}
		return
	}

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

func markdownGapKind(kind render.GapKind) string {
	switch kind {
	case render.GapSkip:
		return "Skipped"
	case render.GapReduction:
		return "Reduced"
	default:
		return string(kind)
	}
}
