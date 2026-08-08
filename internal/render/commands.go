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
// depend on which renderer produced it. This is load-bearing: both
// renderers' plans write the SAME shared path, applied independently
// (in whichever order `apply --target` runs them), so content can never
// vary by which renderer is asking — a renderer-conditional workflowz
// injection would silently flip depending on apply order. A multi-step
// command's body always includes the workflowz directive (see
// workflowzDirective): inert, harmless extra prose on a renderer that
// doesn't recognize it (opencode), and the deterministic-pipeline
// trigger on one that does (omp) — see CapStructuredWorkflowCommand for
// the informational-only capability that reports this per-harness
// fidelity difference without touching rendered content.
func RenderCommands(reg *registry.Registry, readFile func(string) ([]byte, error)) (RebuildTree, error) {
	dirs := make(map[string][]WriteFile, len(reg.Commands))
	for _, c := range reg.Commands {
		body, err := commandBody(c, readFile)
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

// workflowzDirective is unconditionally prepended to every structured
// (multi-step) command's rendered body: it's the prose trigger for
// omp's native `workflowz` magic keyword (see docs/vibe-mode.md's
// sibling docs/magic-keywords.md, and athal7/agentcfg#3's investigation
// comment), which contracts a deterministic multi-subagent pipeline
// over the phases that follow. Unconditional, not gated on which
// renderer is asking, because every renderer writes the exact same
// shared SKILL.md path (see RenderCommands) — the directive is inert,
// harmless extra prose on a renderer that doesn't recognize it
// (opencode) and the functional trigger on one that does (omp); see
// CapStructuredWorkflowCommand for the informational-only capability
// that reports this fidelity difference without varying content.
const workflowzDirective = "Use `workflowz` to run the following phases as a deterministic pipeline via the persistent eval kernel's `agent()`/`parallel()`/`pipeline()` helpers, each phase's output feeding the next.\n\n"

// commandBody resolves a command's full rendered instruction body: a
// flat command's prompt content unchanged, or a structured command's
// steps flattened into numbered sections (each headed "## N. <name>"),
// prefixed with workflowzDirective.
func commandBody(c registry.Command, readFile func(string) ([]byte, error)) (string, error) {
	if len(c.Steps) == 0 {
		return promptText(c.Prompt, c.ResolvedPromptFile, readFile)
	}

	var b strings.Builder
	b.WriteString(workflowzDirective)
	for i, s := range c.Steps {
		stepBody, err := promptText(s.Prompt, s.ResolvedPromptFile, readFile)
		if err != nil {
			return "", fmt.Errorf("step %q: %w", s.Name, err)
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, s.Name)
		b.WriteString(stepBody)
	}
	return b.String(), nil
}

// promptText reads a file-backed prompt's content, or returns an inline
// prompt's text as-is — the same file/text resolution every
// prompt-bearing registry entity uses (mirrors internal/render/omp's
// promptBody for agents). resolvedFile is the caller's already-resolved
// absolute path (Command.ResolvedPromptFile or CommandStep.ResolvedPromptFile),
// empty when the prompt uses inline text instead.
func promptText(p registry.Prompt, resolvedFile string, readFile func(string) ([]byte, error)) (string, error) {
	if resolvedFile != "" {
		content, err := readFile(resolvedFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt file %q: %w", resolvedFile, err)
		}
		return string(content), nil
	}
	return p.Text, nil
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
