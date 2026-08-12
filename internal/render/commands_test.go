package render

import (
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
)

func fixtureReadFile(files map[string]string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		body, ok := files[path]
		if !ok {
			return nil, &pathNotFoundError{path}
		}
		return []byte(body), nil
	}
}

type pathNotFoundError struct{ path string }

func (e *pathNotFoundError) Error() string { return "no fixture for path: " + e.path }

func TestRenderCommands_InlineText(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "Reviews a diff", Prompt: registry.Prompt{Text: "Review the diff."}},
		},
	}

	tree, err := RenderCommands(CommandsSkillsDir, reg, fixtureReadFile(nil))
	if err != nil {
		t.Fatalf("RenderCommands() error = %v", err)
	}

	if tree.Dir != CommandsSkillsDir {
		t.Errorf("Dir = %q, want %q", tree.Dir, CommandsSkillsDir)
	}
	files, ok := tree.Dirs["review"]
	if !ok || len(files) != 1 {
		t.Fatalf("Dirs[review] = %+v, want exactly one SKILL.md entry", tree.Dirs["review"])
	}
	f := files[0]
	if f.Path != "SKILL.md" {
		t.Errorf("Path = %q, want SKILL.md", f.Path)
	}
	want := "---\nname: review\ndescription: \"Reviews a diff\"\n---\nReview the diff."
	if string(f.Content) != want {
		t.Errorf("Content = %q, want %q", f.Content, want)
	}
}

func TestRenderCommands_FileBackedPrompt(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{
				Name:               "review",
				Description:        "Reviews a diff",
				Prompt:             registry.Prompt{File: "prompts/review.md"},
				ResolvedPromptFile: "/registry/prompts/review.md",
			},
		},
	}

	tree, err := RenderCommands(CommandsSkillsDir, reg, fixtureReadFile(map[string]string{
		"/registry/prompts/review.md": "Review the diff for correctness.",
	}))
	if err != nil {
		t.Fatalf("RenderCommands() error = %v", err)
	}

	want := "---\nname: review\ndescription: \"Reviews a diff\"\n---\nReview the diff for correctness."
	if string(tree.Dirs["review"][0].Content) != want {
		t.Errorf("Content = %q, want %q", tree.Dirs["review"][0].Content, want)
	}
}

func TestRenderCommands_ReadFileErrorPropagates(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "d", Prompt: registry.Prompt{File: "missing.md"}, ResolvedPromptFile: "/missing.md"},
		},
	}

	_, err := RenderCommands(CommandsSkillsDir, reg, fixtureReadFile(nil))
	if err == nil {
		t.Fatal("expected an error for an unreadable prompt file, got nil")
	}
}

func TestRenderCommands_MultipleCommandsEachOwnDirectory(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "d1", Prompt: registry.Prompt{Text: "a"}},
			{Name: "ship", Description: "d2", Prompt: registry.Prompt{Text: "b"}},
		},
	}

	tree, err := RenderCommands(CommandsSkillsDir, reg, fixtureReadFile(nil))
	if err != nil {
		t.Fatalf("RenderCommands() error = %v", err)
	}
	if len(tree.Dirs) != 2 {
		t.Fatalf("Dirs = %+v, want 2 entries", tree.Dirs)
	}
	if string(tree.Dirs["review"][0].Content) == string(tree.Dirs["ship"][0].Content) {
		t.Error("expected review and ship to render distinct SKILL.md content")
	}
}

// TestRenderCommands_MultiStepAlwaysIncludesWorkflowzDirective covers the
// shared-path invariant RenderCommands documents: since every renderer
// writes the identical SKILL.md content to the same Agent Skills path
// (see CommandsSkillsDir), a multi-step command's content can never vary
// by which renderer produced it — the workflowz directive is always
// present, regardless of whether the eventual reader is omp (where it's
// functional) or opencode (where it's inert prose).
func TestRenderCommands_MultiStepAlwaysIncludesWorkflowzDirective(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{
				Name:        "ship",
				Description: "Plan, build, and ship a change",
				Steps: []registry.CommandStep{
					{Name: "plan", Prompt: registry.Prompt{Text: "Design an approach."}},
					{Name: "build", Prompt: registry.Prompt{Text: "Implement it."}},
				},
			},
		},
	}

	tree, err := RenderCommands(CommandsSkillsDir, reg, fixtureReadFile(nil))
	if err != nil {
		t.Fatalf("RenderCommands() error = %v", err)
	}

	want := "---\nname: ship\ndescription: \"Plan, build, and ship a change\"\n---\n" +
		"Use `workflowz` to run the following phases as a deterministic pipeline via the persistent eval kernel's `agent()`/`parallel()`/`pipeline()` helpers, each phase's output feeding the next.\n\n" +
		"## 1. plan\n\nDesign an approach.\n\n## 2. build\n\nImplement it."
	got := string(tree.Dirs["ship"][0].Content)
	if got != want {
		t.Errorf("Content = %q, want %q", got, want)
	}
}

func TestRenderCommands_FlatCommandNeverGetsWorkflowzDirective(t *testing.T) {
	reg := &registry.Registry{
		Commands: []registry.Command{
			{Name: "review", Description: "Reviews a diff", Prompt: registry.Prompt{Text: "Review the diff."}},
		},
	}

	tree, err := RenderCommands(CommandsSkillsDir, reg, fixtureReadFile(nil))
	if err != nil {
		t.Fatalf("RenderCommands() error = %v", err)
	}

	got := string(tree.Dirs["review"][0].Content)
	if strings.Contains(got, "workflowz") {
		t.Errorf("Content = %q, did not want workflowz directive on a flat (non-stepped) command", got)
	}
}

func TestRenderCommands_NoCommandsStillReturnsTree(t *testing.T) {
	tree, err := RenderCommands(CommandsSkillsDir, &registry.Registry{}, fixtureReadFile(nil))
	if err != nil {
		t.Fatalf("RenderCommands() error = %v", err)
	}
	if tree.Dir != CommandsSkillsDir {
		t.Errorf("Dir = %q, want %q", tree.Dir, CommandsSkillsDir)
	}
	if len(tree.Dirs) != 0 {
		t.Errorf("Dirs = %+v, want empty (so a since-removed command is pruned on apply)", tree.Dirs)
	}
}
