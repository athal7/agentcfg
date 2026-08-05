package omp

// baseTools is the fixed set of tools every omp subagent file gets,
// regardless of registry permissions — omp has no per-tool allow/deny
// surface finer than the frontmatter's comma-joined tools: list, so these
// are always granted.
var baseTools = []string{
	"read", "grep", "glob", "bash", "todo", "lsp", "web_search", "ast_grep", "inspect_image",
}
