package registry

import (
	"path/filepath"
)

// resolvePromptPaths fills in Agent.ResolvedPromptFile for every agent using
// prompt.file, joining it against the registry root (never cwd, never home).
// Paths that escape the registry root are left empty; validation catches them.
func resolvePromptPaths(reg *Registry) {
	for i := range reg.Agents {
		if reg.Agents[i].Prompt.File != "" {
			resolved := filepath.Join(reg.RootDir, reg.Agents[i].Prompt.File)
			rel, err := filepath.Rel(reg.RootDir, resolved)
			if err != nil || filepath.IsAbs(rel) || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
				// Traversal violation — leave ResolvedPromptFile empty.
				// validateAgents will detect and report this.
				continue
			}
			reg.Agents[i].ResolvedPromptFile = resolved
		}
	}
}

// normalizeAgentModes defaults an agent's mode to "subagent" when omitted.
func normalizeAgentModes(reg *Registry) {
	for i := range reg.Agents {
		if reg.Agents[i].Mode == "" {
			reg.Agents[i].Mode = "subagent"
		}
	}
}
