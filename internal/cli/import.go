package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/athal7/agentcfg/internal/importer"
)

// newImportCmd builds the import command that imports existing harness configs.
func newImportCmd() *cobra.Command {
	var (
		fromFlag     string
		registryFlag string
		forceFlag    bool
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import existing harness configurations into an agentcfg registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd.OutOrStdout(), fromFlag, registryFlag, forceFlag)
		},
	}

	cmd.Flags().StringVar(&fromFlag, "from", "", "comma-separated list of harnesses to import from (opencode, omp, codex, claude; default: all)")
	cmd.Flags().StringVar(&registryFlag, "registry", "", "registry directory to import into (default resolution: env/XDG/~/.config)")
	cmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "overwrite existing registry files")

	return cmd
}

func runImport(out io.Writer, fromFlag, registryFlag string, forceFlag bool) error {
	dir := ResolveRegistryDir(registryFlag)

	var targets []importer.ImportTarget
	if strings.TrimSpace(fromFlag) == "" {
		targets = []importer.ImportTarget{
			importer.TargetOpencode,
			importer.TargetOMP,
			importer.TargetCodex,
			importer.TargetClaude,
		}
	} else {
		for _, raw := range strings.Split(fromFlag, ",") {
			t := importer.ImportTarget(strings.TrimSpace(strings.ToLower(raw)))
			switch t {
			case importer.TargetOpencode, importer.TargetOMP, importer.TargetCodex, importer.TargetClaude:
				targets = append(targets, t)
			case "":
			default:
				return fmt.Errorf("import: unknown harness %q (supported: opencode, omp, codex, claude)", raw)
			}
		}
	}

	res, err := importer.ImportHarnesses(targets, importer.Options{})
	if err != nil {
		return fmt.Errorf("importing harnesses: %w", err)
	}

	if !forceFlag {
		for name := range res.Files {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("import: %s already exists; use --force to overwrite an existing registry", path)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("import: checking %s: %w", path, err)
			}
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("import: creating directory %s: %w", dir, err)
	}

	if err := writeImportFiles(dir, res.Files); err != nil {
		return err
	}

	fmt.Fprintf(out, "imported configuration into agentcfg registry at %s\n", dir)
	return nil
}

func writeImportFiles(dir string, files map[string]string) error {
	order := []string{"models.yaml", "bash.yaml", "workflow.yaml", "mcp.yaml", "agentcfg.yaml"}
	staged := make(map[string]string, len(files))
	for _, name := range order {
		content, ok := files[name]
		if !ok {
			continue
		}
		file, err := os.CreateTemp(dir, "."+name+".*")
		if err != nil {
			return fmt.Errorf("import: staging %s: %w", name, err)
		}
		tempPath := file.Name()
		if _, err := file.WriteString(content); err != nil {
			file.Close()
			os.Remove(tempPath)
			return fmt.Errorf("import: staging %s: %w", name, err)
		}
		if err := file.Chmod(0o644); err != nil {
			file.Close()
			os.Remove(tempPath)
			return fmt.Errorf("import: staging %s: %w", name, err)
		}
		if err := file.Close(); err != nil {
			os.Remove(tempPath)
			return fmt.Errorf("import: staging %s: %w", name, err)
		}
		staged[name] = tempPath
	}
	for _, name := range order {
		tempPath, ok := staged[name]
		if !ok {
			continue
		}
		if err := os.Rename(tempPath, filepath.Join(dir, name)); err != nil {
			for _, path := range staged {
				os.Remove(path)
			}
			return fmt.Errorf("import: writing %s: %w", name, err)
		}
	}
	return nil
}
