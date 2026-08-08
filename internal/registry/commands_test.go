package registry_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_CommandHappyPath(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - name: review
    description: "Reviews the current diff"
    prompt: { text: "Review the current diff for correctness." }
`
	reg, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)

	if len(reg.Commands) != 1 {
		t.Fatalf("Commands = %+v, want 1 entry", reg.Commands)
	}
	cmd := reg.Commands[0]
	if cmd.Name != "review" {
		t.Errorf("Name = %q, want %q", cmd.Name, "review")
	}
	if cmd.Description != "Reviews the current diff" {
		t.Errorf("Description = %q, want %q", cmd.Description, "Reviews the current diff")
	}
	if cmd.Prompt.Text != "Review the current diff for correctness." {
		t.Errorf("Prompt.Text = %q, want the inline text", cmd.Prompt.Text)
	}
}

func TestLoad_CommandPromptFileResolvedRelativeToRegistryRoot(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - name: review
    description: "Reviews the current diff"
    prompt: { file: prompts/review.md }
`
	files["prompts/review.md"] = "Review the current diff.\n"

	reg, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)

	want := filepath.Join(reg.RootDir, "prompts/review.md")
	if reg.Commands[0].ResolvedPromptFile != want {
		t.Errorf("ResolvedPromptFile = %q, want %q", reg.Commands[0].ResolvedPromptFile, want)
	}
}

func TestValidate_DuplicateCommandName(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - name: review
    description: "a"
    prompt: { text: "a" }
  - name: review
    description: "b"
    prompt: { text: "b" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `duplicate command name "review"`) {
		t.Errorf("errs = %v, want duplicate command name error", errs)
	}
}

func TestValidate_CommandNoName(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - description: "a"
    prompt: { text: "a" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "command has no name") {
		t.Errorf("errs = %v, want command-has-no-name error", errs)
	}
}

func TestValidate_CommandInvalidName(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"uppercase", "Review"},
		{"underscore", "review_diff"},
		{"leading_hyphen", "-review"},
		{"trailing_hyphen", "review-"},
		{"too_long", "a" + strings.Repeat("b", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := minimalFixtureFiles()
			files["agents.yaml"] += "\ncommands:\n  - name: " + tt.cmd + "\n    description: \"a\"\n    prompt: { text: \"a\" }\n"
			_, errs, _, err := loadFixture(t, files)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !anyErrorContains(errs, "has invalid name") {
				t.Errorf("errs = %v, want invalid command name error for %q", errs, tt.cmd)
			}
		})
	}
}

func TestValidate_CommandMissingDescription(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - name: review
    prompt: { text: "a" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `command "review" has no description`) {
		t.Errorf("errs = %v, want missing description error", errs)
	}
}

func TestValidate_CommandPromptRequiresExactlyOneOfFileOrText(t *testing.T) {
	tests := []struct {
		name    string
		prompt  string
		wantMsg string
	}{
		{
			name:    "neither",
			prompt:  ``,
			wantMsg: `must set exactly one of prompt or steps`,
		},
		{
			name:    "both",
			prompt:  `file: prompts/lead.md, text: "inline"`,
			wantMsg: `not both`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := minimalFixtureFiles()
			files["agents.yaml"] += "\ncommands:\n  - name: review\n    description: \"a\"\n    prompt: { " + tt.prompt + " }\n"
			_, errs, _, err := loadFixture(t, files)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !anyErrorContains(errs, tt.wantMsg) {
				t.Errorf("errs = %v, want one containing %q", errs, tt.wantMsg)
			}
		})
	}
}

func TestValidate_CommandPromptFileMustExist(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - name: review
    description: "a"
    prompt: { file: prompts/does-not-exist.md }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "referenced prompt file does not exist") {
		t.Errorf("errs = %v, want a missing-prompt-file error", errs)
	}
}

func TestValidate_CommandPromptFileTraversalRejected(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] += `
commands:
  - name: review
    description: "a"
    prompt: { file: "../outside.md" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "prompt file escapes registry root") {
		t.Errorf("errs = %v, want a prompt-file-traversal error", errs)
	}
}
