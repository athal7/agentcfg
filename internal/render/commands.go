package render

import (
	"fmt"
	"strings"

	"github.com/athal7/agentcfg/internal/registry"
)

// CommandsSkillsDir is the shared Agent Skills root both opencode and omp
// natively discover at user scope: opencode's global skill search path
// includes "~/.agents/skills/*/SKILL.md" directly; omp's own "agents"
// skill provider — its docs call ".agent[s]/skills" "the canonical
// OMP-native location" — reads the identical path. Because both harnesses
// resolve the exact same location, rendering a command needs no
// per-harness translation: internal/render/opencode and
// internal/render/omp both call RenderCommands and get back the exact
// same RebuildTree (see docs/schema.md's commands: section for the
// confirmed discovery parity this relies on).
const CommandsSkillsDir = "~/.agents/skills"

// RenderCommands builds the RebuildTree that (re)writes every registry
// command as an Agent Skills SKILL.md file under CommandsSkillsDir,
// pruning any previously-rendered command no longer present in the
// registry. Called identically from every renderer that declares
// CapCustomCommands — currently both internal/render/opencode and
// internal/render/omp — so a command's rendered content and path never
// depend on which renderer produced it.
func RenderCommands(reg *registry.Registry, readFile func(string) ([]byte, error)) (RebuildTree, error) {
	dirs := make(map[string][]WriteFile, len(reg.Commands))
	for _, c := range reg.Commands {
		body, err := commandPromptBody(c, readFile)
		if err != nil {
			return RebuildTree{}, fmt.Errorf("command %q: %w", c.Name, err)
		}
		dirs[c.Name] = []WriteFile{{
			Path:    "SKILL.md",
			Mode:    0600,
			Content: []byte(renderSkillFile(c, body)),
		}}
	}
	return RebuildTree{Dir: CommandsSkillsDir, Dirs: dirs}, nil
}

// commandPromptBody reads a file-backed command prompt's content, or
// returns an inline prompt's text as-is — the same file/text resolution
// every prompt-bearing registry entity uses (mirrors
// internal/render/omp's promptBody for agents).
func commandPromptBody(c registry.Command, readFile func(string) ([]byte, error)) (string, error) {
	if c.ResolvedPromptFile != "" {
		content, err := readFile(c.ResolvedPromptFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt file %q: %w", c.ResolvedPromptFile, err)
		}
		return string(content), nil
	}
	return c.Prompt.Text, nil
}

// renderSkillFile builds one SKILL.md's full content: YAML frontmatter
// with exactly the two fields the Agent Skills spec requires (name,
// description — name must match the containing directory, which is
// c.Name, satisfied by RenderCommands using c.Name as the Dirs key too),
// followed by "---" and the raw prompt body as the skill's markdown
// instructions.
func renderSkillFile(c registry.Command, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", c.Name)
	fmt.Fprintf(&b, "description: %s\n", c.Description)
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}
