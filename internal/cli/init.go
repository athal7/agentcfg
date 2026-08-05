package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const starterAgentcfgYAML = `version: 1
imports:
  - models.yaml
  - bash.yaml
  - agents.yaml
harnesses:
  opencode:
    out: ~/.config/opencode/opencode.json
  omp:
    agents_dir: ~/.omp/agent/agents
`

const starterModelsYAML = `model_classes:
  default: anthropic/claude-sonnet-4-5
  smol: anthropic/claude-haiku-4-5
`

// starterBashYAML defines the "global" profile every renderer compiles
// against unconditionally (bashpolicy.Compile(reg.Bash, "global") is
// hardcoded in both internal/render/opencode and internal/render/omp).
// Without this file, a freshly-init'd registry fails render/apply/doctor/
// explain with "bash profile \"global\" not found" — init must always
// produce a loadable-and-renderable registry, not just a
// schema-valid one.
const starterBashYAML = `bash:
  profiles:
    global:
      base: allow
`

const starterAgentsYAML = `agents:
  - name: lead
    mode: primary
    class: default
    prompt:
      text: "You are a helpful assistant."
`

func newInitCmd() *cobra.Command {
	var registryFlag string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new agentcfg registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.OutOrStdout(), registryFlag)
		},
	}
	cmd.Flags().StringVar(&registryFlag, "registry", "", "registry directory to scaffold (default resolution: env/XDG/~/.config)")
	return cmd
}

// runInit scaffolds a minimal, valid starter registry at the resolved
// directory. It refuses to run if agentcfg.yaml already exists there —
// init is meant to bootstrap a fresh registry, never to clobber one.
func runInit(out io.Writer, registryFlag string) error {
	dir := ResolveRegistryDir(registryFlag)
	entryPath := filepath.Join(dir, "agentcfg.yaml")

	if _, err := os.Stat(entryPath); err == nil {
		return fmt.Errorf("init: %s already exists; refusing to overwrite an existing registry", entryPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("init: checking %s: %w", entryPath, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("init: creating %s: %w", dir, err)
	}

	files := map[string]string{
		"models.yaml":   starterModelsYAML,
		"bash.yaml":     starterBashYAML,
		"agents.yaml":   starterAgentsYAML,
		"agentcfg.yaml": starterAgentcfgYAML,
	}
	// Write agentcfg.yaml last: a partially-written scaffold (e.g. disk
	// full mid-write) should never look like a complete, loadable
	// registry — Load only ever looks for agentcfg.yaml first.
	order := []string{"models.yaml", "bash.yaml", "agents.yaml", "agentcfg.yaml"}
	for _, name := range order {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o644); err != nil {
			return fmt.Errorf("init: writing %s: %w", name, err)
		}
	}

	fmt.Fprintf(out, "initialized agentcfg registry at %s\n", dir)
	return nil
}
