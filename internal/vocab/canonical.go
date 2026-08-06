// Package vocab defines the harness-agnostic canonical tool/permission
// vocabulary. Each renderer maps these canonical names to its own harness's
// native vocabulary; agentcfg's registry schema and compiler only ever deal
// in these canonical names.
package vocab

// Canonical is a harness-agnostic tool or permission name.
type Canonical string

// Canonical tool and permission names used across all renderers.
const (
	Read              Canonical = "read"
	Write             Canonical = "write"
	Edit              Canonical = "edit"
	Grep              Canonical = "grep"
	Glob              Canonical = "glob"
	Bash              Canonical = "bash"
	LSP               Canonical = "lsp"
	WebSearch         Canonical = "web_search"
	WebFetch          Canonical = "web_fetch"
	Task              Canonical = "task"
	Skill             Canonical = "skill"
	ExternalDirectory Canonical = "external_directory"
	Todo              Canonical = "todo"
	ASTSearch         Canonical = "ast_search"
	ImageInspect      Canonical = "image_inspect"
)
