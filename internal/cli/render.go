package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/renderers"
)

// renderOptions is shared verbatim by `render` and `apply` (apply adds
// --best-effort/--timeout on top).
type renderOptions struct {
	registry   string
	target     string
	scope      string
	contextDir string
	explain    bool
	jsonOut    bool
	strict     bool
}

func registerRenderFlags(cmd *cobra.Command, opts *renderOptions) {
	cmd.Flags().StringVar(&opts.registry, "registry", "", "registry directory (default resolution: env/XDG/~/.config)")
	cmd.Flags().StringVar(&opts.target, "target", "", "comma-separated renderer IDs (default: all registered renderers)")
	cmd.Flags().StringVar(&opts.scope, "scope", "global", "global|project|all")
	cmd.Flags().StringVar(&opts.contextDir, "context", ".", "directory to resolve project scope against (only used when scope includes project)")
	cmd.Flags().BoolVar(&opts.explain, "explain", false, "print a detailed human-readable explanation")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "print machine-readable JSON instead of text")
	cmd.Flags().BoolVar(&opts.strict, "strict", false, "exit non-zero if any gap is present across all rendered plans")
}

func newRenderCmd() *cobra.Command {
	var opts renderOptions

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Preview native harness configuration without writing anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRender(cmd.OutOrStdout(), opts)
		},
	}
	registerRenderFlags(cmd, &opts)
	return cmd
}

// runRender is a pure dry run: it builds Plans exactly like `apply` would,
// but never calls internal/apply — nothing is written, nothing is run.
// --explain and --json are format/verbosity modifiers on top of the
// baseline one-line-per-target summary that prints when neither is set;
// --json replaces the summary with a JSON document instead of layering on
// top of it (mixing formats on stdout would make the JSON unparseable).
func runRender(out io.Writer, opts renderOptions) error {
	plans, err := loadAndBuildPlans(opts.registry, opts.target, opts.scope, opts.contextDir)
	if err != nil {
		return err
	}

	switch {
	case opts.jsonOut:
		if err := printPlanJSON(out, plans); err != nil {
			return err
		}
	case opts.explain:
		printPlanExplain(out, plans)
	default:
		printPlanSummary(out, plans)
	}

	if opts.strict {
		if n := countGaps(plans); n > 0 {
			return fmt.Errorf("render: %d gap(s) found (--strict)", n)
		}
	}
	return nil
}

// loadAndBuildPlans is the shared "load registry, pick targets, render
// plans" pipeline behind both `render` and `apply`.
func loadAndBuildPlans(registryFlag, targetFlag, scopeFlag, contextDir string) ([]targetPlan, error) {
	if err := validateScope(scopeFlag); err != nil {
		return nil, err
	}

	dir := ResolveRegistryDir(registryFlag)
	reg, verrs, _, err := registry.Load(dir)
	if err != nil {
		return nil, err
	}
	if len(verrs) > 0 {
		return nil, fmt.Errorf("registry has %d validation error(s); run `agentcfg validate` for details", len(verrs))
	}

	targets, err := filterRenderers(renderers.All(), targetFlag)
	if err != nil {
		return nil, err
	}

	return buildPlans(reg, targets, scopeFlag, contextDir)
}
