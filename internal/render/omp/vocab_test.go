package omp

import "testing"

// TestCreateMCPToolName exercises createMCPToolName against boundary cases
// the prior hand-rolled implementation (hyphen-only replacement, a single
// hard-coded "context7" special case, no redundant-prefix strip) got
// wrong, alongside the already-covered context7 case for continuity. Each
// case is a real instance of a rule this repo used to special-case or omit
// entirely — see vocab.go's doc comments and docs/decisions/0003.
func TestCreateMCPToolName(t *testing.T) {
	cases := []struct {
		name       string
		server     string
		tool       string
		want       string
		regression string
	}{
		{
			name:   "context7 digit-stripping",
			server: "context7",
			tool:   "query-docs",
			want:   "mcp__context_query_docs",
			regression: "the old code special-cased exactly this server name; " +
				"this is now an instance of the general digit-stripping rule, not a carve-out",
		},
		{
			name:   "redundant server-name prefix is stripped",
			server: "puppeteer",
			tool:   "puppeteer_screenshot",
			want:   "mcp__puppeteer_screenshot",
			regression: "the old code never implemented this strip and would have produced " +
				"mcp__puppeteer_puppeteer_screenshot — a tool id the real omp session never registers",
		},
		{
			name:   "digit in a non-context7 server name",
			server: "gpt4-tools",
			tool:   "run",
			want:   "mcp__gpt_tools_run",
			regression: "the old code only replaced literal hyphens, leaving the digit in place " +
				"(mcp__gpt4_tools_run) — a mismatch against omp's own sanitizer, which strips it",
		},
		{
			name:   "hyphenated server, ordinary tool",
			server: "runlayer-slack",
			tool:   "slack_read_channel",
			want:   "mcp__runlayer_slack_slack_read_channel",
			regression: "no redundant prefix here (the tool name is missing the \"runlayer_\" part), " +
				"so this stays double-prefixed on both old and new code — a control case proving " +
				"the fix doesn't over-strip",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := createMCPToolName(c.server, c.tool); got != c.want {
				t.Errorf("createMCPToolName(%q, %q) = %q, want %q (%s)", c.server, c.tool, got, c.want, c.regression)
			}
		})
	}
}
