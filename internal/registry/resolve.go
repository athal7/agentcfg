package registry

import (
	"path/filepath"
)

// promptFileTraversal reports whether promptFile (relative to rootDir)
// escapes rootDir, either via ".." segments, an absolute path, or (once
// resolved) a symlink pointing outside rootDir. It returns the joined
// (pre-symlink-resolution) path alongside the verdict, since callers that
// don't reject the path still need it for further use (existence checks,
// ResolvedPromptFile).
func promptFileTraversal(rootDir, rootReal, promptFile string) (violates bool, resolved string) {
	if filepath.IsAbs(promptFile) {
		return true, ""
	}
	resolved = filepath.Join(rootDir, promptFile)
	rel, err := filepath.Rel(rootDir, resolved)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
		return true, resolved
	}
	if realPath, err := filepath.EvalSymlinks(resolved); err == nil {
		realRel, err := filepath.Rel(rootReal, realPath)
		if err != nil || filepath.IsAbs(realRel) || realRel == ".." || len(realRel) > 2 && realRel[:3] == ".."+string(filepath.Separator) {
			return true, resolved
		}
	}
	return false, resolved
}

// resolvePromptPaths fills in Agent.ResolvedPromptFile for every agent using
// prompt.file, joining it against the registry root (never cwd, never home).
// Paths that escape the registry root are left empty; validation catches them.
func resolvePromptPaths(reg *Registry) {
	rootReal, _ := filepath.EvalSymlinks(reg.RootDir)

	for i := range reg.Agents {
		if reg.Agents[i].Prompt.File != "" {
			violates, resolved := promptFileTraversal(reg.RootDir, rootReal, reg.Agents[i].Prompt.File)
			if violates {
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
