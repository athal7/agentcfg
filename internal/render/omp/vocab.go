package omp

import (
	"fmt"
	"strings"

	"github.com/athal7/agentcfg/internal/registry"
)

// baseTools is the fixed set of tools every omp subagent file gets,
// regardless of registry permissions — omp has no per-tool allow/deny
// surface finer than the frontmatter's comma-joined tools: list, so these
// are always granted.
var baseTools = []string{
	"read", "grep", "glob", "bash", "todo", "lsp", "web_search", "ast_grep", "inspect_image",
}

// mcpServerToolIDs expands one MCP server's declared Tools allowlist into
// the exact omp tool ids it exposes under xd://. omp derives each id as
// mcp__<server, hyphens->underscores>_<raw tool name, lowercased,
// hyphens->underscores> — except context7, which omp addresses as
// "context" internally. This is the single place that encodes the
// id-derivation rule, so a subagent's frontmatter tool grants
// (renderAgentFile) and the global tools.approval allow-list
// (renderToolsApprovalCommand) can never drift from each other. Returns
// nil when the server declares no Tools allowlist.
func mcpServerToolIDs(s registry.MCPServer) []string {
	if len(s.Tools) == 0 {
		return nil
	}
	prefix := strings.ReplaceAll(s.Name, "-", "_")
	if s.Name == "context7" {
		prefix = "context"
	}
	ids := make([]string, 0, len(s.Tools))
	for _, t := range s.Tools {
		ids = append(ids, fmt.Sprintf("mcp__%s_%s", prefix, strings.ReplaceAll(strings.ToLower(t), "-", "_")))
	}
	return ids
}
