package omp

import "strings"

// baseTools is the fixed set of tools every omp subagent file gets,
// regardless of registry permissions — omp has no per-tool allow/deny
// surface finer than the frontmatter's comma-joined tools: list, so these
// are always granted.
var baseTools = []string{
	"read", "grep", "glob", "bash", "todo", "lsp", "web_search", "ast_grep", "inspect_image",
}

// builtinAlwaysAllow are omp built-in tools kept auto-approved in
// tools.approval whenever always-ask mode is active, independent of any
// per-agent registry permission (unlike task/write/edit/ast_edit below,
// which are conditionally allow-listed only when some agent's
// permissions actually grant them). `ask` gating itself would be
// circular — it's the mechanism that solicits human input, and can't
// resolve its own approval prompt headlessly. `eval` has no
// bash.patterns-style pattern gate, so leaving it ask-by-default would
// block ordinary benign computation on every single call. Verified
// empirically against a live omp session: every other built-in
// (read/grep/glob/lsp/web_search/todo/hub) auto-approves under
// always-ask regardless of any tools.approval entry — tier "read" or
// approval-exempt — so they need no entry here at all.
var builtinAlwaysAllow = []string{"ask", "eval"}

// mcpToolID derives the exact omp tool id for one MCP server's tool, per
// the empirically-confirmed derivation rule: mcp__<server, hyphens ->
// underscores>_<tool, lowercased, hyphens -> underscores>. context7 is a
// confirmed special case — omp addresses it internally as "context", not
// "context7", for reasons undocumented upstream. Every other server
// observed while deriving this rule (firefox-devtools, github, and seven
// runlayer-* servers) matched the mechanical rule exactly, including
// camelCase raw tool names (simply lowercased, no separator inserted).
func mcpToolID(server, tool string) string {
	prefix := strings.ReplaceAll(server, "-", "_")
	if server == "context7" {
		prefix = "context"
	}
	name := strings.ReplaceAll(strings.ToLower(tool), "-", "_")
	return "mcp__" + prefix + "_" + name
}
