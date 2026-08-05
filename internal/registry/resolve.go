package registry

import "path/filepath"

// resolvePromptPaths fills in Agent.ResolvedPromptFile for every agent using
// prompt.file, joining it against the registry root (never cwd, never home).
func resolvePromptPaths(reg *Registry) {
	for i := range reg.Agents {
		if reg.Agents[i].Prompt.File != "" {
			reg.Agents[i].ResolvedPromptFile = filepath.Join(reg.RootDir, reg.Agents[i].Prompt.File)
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
