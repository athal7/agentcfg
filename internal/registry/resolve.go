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

// resolvePromptPaths fills in ResolvedPromptFile for every agent (including
// each agent's harness_prompts entries) and every command using
// prompt.file, joining it against the registry root (never cwd, never
// home). Paths that escape the registry root are left empty; validation
// catches them.
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
		for target, hp := range reg.Agents[i].HarnessPrompts {
			if hp.Prompt.File == "" {
				continue
			}
			violates, resolved := promptFileTraversal(reg.RootDir, rootReal, hp.Prompt.File)
			if violates {
				continue
			}
			hp.ResolvedPromptFile = resolved
			reg.Agents[i].HarnessPrompts[target] = hp
		}
	}

	for i := range reg.Commands {
		if reg.Commands[i].Prompt.File != "" {
			violates, resolved := promptFileTraversal(reg.RootDir, rootReal, reg.Commands[i].Prompt.File)
			if violates {
				continue
			}
			reg.Commands[i].ResolvedPromptFile = resolved
		}
		for j := range reg.Commands[i].Steps {
			if reg.Commands[i].Steps[j].Prompt.File == "" {
				continue
			}
			violates, resolved := promptFileTraversal(reg.RootDir, rootReal, reg.Commands[i].Steps[j].Prompt.File)
			if violates {
				continue
			}
			reg.Commands[i].Steps[j].ResolvedPromptFile = resolved
		}
	}
}

// normalizeAgentRoles defaults a step's role to "delegate" when omitted.
func normalizeAgentRoles(reg *Registry) {
	for i := range reg.Agents {
		if reg.Agents[i].Role == "" {
			reg.Agents[i].Role = "delegate"
		}
	}
}
