package omp

import (
	"fmt"
	"regexp"
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

// mcpToolNameSanitizeRe matches any run of characters outside [a-z_] — the
// exact character class omp's real sanitizer collapses to a single "_".
// Notably this includes digits: "context7" sanitizes to "context_" and
// then trims to "context", which is why omp addresses the context7 server
// as "context" — not a special case, a direct consequence of this rule.
var mcpToolNameSanitizeRe = regexp.MustCompile(`[^a-z_]+`)

// sanitizeMCPToolNamePart ports omp's sanitizeMCPToolNamePart
// (packages/coding-agent/src/mcp/tool-bridge.ts, omp v17.2.11) verbatim:
// lowercase, collapse every run of non-[a-z_] characters (digits included)
// to a single "_", collapse repeated "_", trim leading/trailing "_", and
// fall back to a placeholder when nothing survives.
func sanitizeMCPToolNamePart(value, fallback string) string {
	sanitized := mcpToolNameSanitizeRe.ReplaceAllString(strings.ToLower(value), "_")
	for strings.Contains(sanitized, "__") {
		sanitized = strings.ReplaceAll(sanitized, "__", "_")
	}
	sanitized = strings.Trim(sanitized, "_")
	if sanitized == "" {
		return fallback
	}
	return sanitized
}

// createMCPToolName ports omp's createMCPToolName verbatim, including the
// redundant-server-name-prefix strip (a server "puppeteer" with tool
// "puppeteer_screenshot" yields "mcp__puppeteer_screenshot", not
// "mcp__puppeteer_puppeteer_screenshot"). Every prior special-case this
// repo carried (the context7 -> context comment) is now the general rule,
// not a carve-out — see mcpToolNameSanitizeRe's doc comment.
func createMCPToolName(serverName, toolName string) string {
	sanitizedServer := sanitizeMCPToolNamePart(serverName, "server")
	sanitizedTool := sanitizeMCPToolNamePart(toolName, "tool")

	prefixWithUnderscore := sanitizedServer + "_"
	normalizedTool := strings.TrimPrefix(sanitizedTool, prefixWithUnderscore)

	return fmt.Sprintf("mcp__%s_%s", sanitizedServer, normalizedTool)
}

// mcpServerToolIDs expands one MCP server's declared Tools allowlist into
// the exact omp tool ids it exposes under xd://, via createMCPToolName —
// the single place that encodes the id-derivation rule, so a subagent's
// frontmatter tool grants (renderAgentFile) and the global tools.approval
// allow-list (renderToolsApprovalCommand) can never drift from each other,
// and can never drift from omp's own derivation either (see
// docs/decisions/0003 for why a hand-simplified reimplementation of a
// harness's own wire/naming rules is the recurring failure mode this
// function used to be an instance of: it special-cased context7 instead of
// implementing the general digit-stripping rule, and never implemented the
// redundant-prefix strip at all). Returns nil when the server declares no
// Tools allowlist.
func mcpServerToolIDs(s registry.MCPServer) []string {
	if len(s.Tools) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.Tools))
	for _, t := range s.Tools {
		ids = append(ids, createMCPToolName(s.Name, t))
	}
	return ids
}
