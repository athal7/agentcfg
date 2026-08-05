package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveRegistryDir determines the registry directory every command
// except `init` operates on, in precedence order:
//
//  1. flagValue (the --registry flag, when non-empty)
//  2. $AGENTCFG_REGISTRY
//  3. $XDG_CONFIG_HOME/agentcfg
//  4. ~/.config/agentcfg
//
// The result always has a leading "~" expanded — registry.Load also
// expands "~" itself, but callers in this package (e.g. `init`, which
// checks for an existing agentcfg.yaml before Load ever runs) need an
// already-expanded path.
func ResolveRegistryDir(flagValue string) string {
	switch {
	case flagValue != "":
		return expandTilde(flagValue)
	case os.Getenv("AGENTCFG_REGISTRY") != "":
		return expandTilde(os.Getenv("AGENTCFG_REGISTRY"))
	case os.Getenv("XDG_CONFIG_HOME") != "":
		return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "agentcfg")
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			// Extremely unlikely (no HOME, no passwd entry). Fall back to
			// a relative path rather than erroring — Load will fail with
			// a clear "no agentcfg.yaml found" message either way.
			return filepath.Join(".config", "agentcfg")
		}
		return filepath.Join(home, ".config", "agentcfg")
	}
}

func expandTilde(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}
