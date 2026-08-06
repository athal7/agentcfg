// Command agentcfg compiles a registry of YAML config into native
// configuration for coding-agent CLI harnesses. All command wiring lives
// in internal/cli; this is just the entrypoint.
package main

//go:generate sh -c "cd ../.. && ./scripts/gen-capabilities.sh"

import "github.com/athal7/agentcfg/internal/cli"

// main is the CLI entrypoint; it builds and executes the root command.
func main() {
	cli.Execute()
}
