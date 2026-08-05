package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/registry"
)

func newValidateCmd() *cobra.Command {
	var registryFlag string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a registry and report errors and warnings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd.OutOrStdout(), registryFlag)
		},
	}
	cmd.Flags().StringVar(&registryFlag, "registry", "", "registry directory (default resolution: env/XDG/~/.config)")
	return cmd
}

// runValidate loads the registry and prints every warning then every
// error. Warnings never fail the command; any error does. Note:
// registry.ValidationError/ValidationWarning carry only a Message string —
// the loader doesn't track source file or line number for a given
// problem, so there's nothing more precise to print here.
func runValidate(out io.Writer, registryFlag string) error {
	dir := ResolveRegistryDir(registryFlag)
	_, verrs, vwarns, err := registry.Load(dir)
	if err != nil {
		return err
	}

	for _, w := range vwarns {
		fmt.Fprintf(out, "warning: %s\n", w.Message)
	}
	for _, e := range verrs {
		fmt.Fprintf(out, "error: %s\n", e.Message)
	}

	if len(verrs) > 0 {
		return fmt.Errorf("validate: %d error(s), %d warning(s)", len(verrs), len(vwarns))
	}

	fmt.Fprintf(out, "%s: OK (%d warning(s))\n", dir, len(vwarns))
	return nil
}
