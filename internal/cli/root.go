// Package cli wires agentcfg's cobra command tree: registry resolution,
// init/validate/render/apply/doctor/explain. It has no knowledge of a
// specific caller or environment beyond the flags/env vars each command
// documents — nothing here is tool- or host-specific.
package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// Execute runs the agentcfg CLI and exits the process with a non-zero
// code on error. This is the only entrypoint cmd/agentcfg/main.go needs.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd builds the root agentcfg cobra command and registers all subcommands.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "agentcfg",
		Short:         "Compile a harness-agnostic agent registry into native coding-agent configuration",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(newInitCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newRenderCmd())
	root.AddCommand(newApplyCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newExplainCmd())

	return root
}
