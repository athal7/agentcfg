package omp

import (
	"encoding/json"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/bashpolicy"
	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

func baseBashPolicy() registry.BashPolicy {
	return registry.BashPolicy{
		Lists: map[string]map[string]registry.Decision{
			"safe": {"ls*": registry.Allow},
		},
		DefaultLists: sp([]string{"safe"}),
		Profiles: map[string]registry.BashProfile{
			"global": {Base: registry.Ask},
		},
	}
}

// homeSecretTokenPath returns the expanded absolute path for
// "~/secret-token", mirroring renderHeaderValue's own expandHome so the
// header lazy-resolution test can assert home-relative expansion without
// hardcoding the test machine's home directory.
func homeSecretTokenPath(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current: %v", err)
	}
	return filepath.Join(u.HomeDir, "secret-token")
}

func sp(s []string) *[]string { return &s }

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

func outputByType[T render.Output](t *testing.T, outputs []render.Output) T {
	t.Helper()
	for _, o := range outputs {
		if v, ok := o.(T); ok {
			return v
		}
	}
	t.Fatalf("no output of type %T found among %d outputs", *new(T), len(outputs))
	panic("unreachable")
}

// TestRender_LeadAndBuildProducesFourOutputs covers the happy path: a
// primary + subagent pair, one local mcp server, file-backed prompts.
func TestRender_LeadAndBuildProducesFourOutputs(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku", "big": "claude-opus-big"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:               "lead",
				Role:               "primary",
				Class:              "big",
				Prompt:             registry.Prompt{File: "prompts/lead.md"},
				ResolvedPromptFile: "/registry/prompts/lead.md",
			},
			{
				Name:               "build",
				Role:               "delegate",
				Class:              "default",
				Prompt:             registry.Prompt{File: "prompts/build.md"},
				ResolvedPromptFile: "/registry/prompts/build.md",
				Permissions: registry.Permissions{
					Task:  "allow",
					Write: "allow",
					Edit:  "allow",
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "fs",
				Transport: "local",
				Command:   []registry.Value{{Literal: "mcp-fs"}, {Literal: "--root"}, {Literal: "/tmp"}},
			},
		},
	}

	readFile := fixtureReadFile(map[string]string{
		"/registry/prompts/lead.md":  "You are the lead.",
		"/registry/prompts/build.md": "You build things.",
	})

	plan, err := New().Render(reg, render.Options{ReadFile: readFile})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Outputs) != 5 {
		t.Fatalf("got %d outputs, want 5 (was 4 before the commands RebuildTree): %+v", len(plan.Outputs), plan.Outputs)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if rebuild.Dir != "~/.omp/agent/agents" || rebuild.Glob != "*.md" {
		t.Errorf("got RebuildDir{Dir:%q,Glob:%q}, want ~/.omp/agent/agents,*.md", rebuild.Dir, rebuild.Glob)
	}
	if len(rebuild.Files) != 1 {
		t.Fatalf("got %d agent files, want 1 (primary excluded): %+v", len(rebuild.Files), rebuild.Files)
	}
	buildFile := rebuild.Files[0]
	if buildFile.Path != "build.md" {
		t.Errorf("got path %q, want build.md", buildFile.Path)
	}
	wantContent := "---\n" +
		"name: build\n" +
		"description: \n" +
		"tools: read,grep,glob,bash,todo,lsp,web_search,ast_grep,inspect_image,write,edit,ast_edit\n" +
		"spawns: \"*\"\n" +
		"model: \"@default\"\n" +
		"---\n" +
		"You build things."
	if string(buildFile.Content) != wantContent {
		t.Errorf("got content:\n%s\nwant:\n%s", buildFile.Content, wantContent)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	if appendFile.Path != "~/.omp/agent/APPEND_SYSTEM.md" {
		t.Errorf("got path %q, want ~/.omp/agent/APPEND_SYSTEM.md", appendFile.Path)
	}
	if string(appendFile.Content) != "You are the lead." {
		t.Errorf("got content %q, want %q", appendFile.Content, "You are the lead.")
	}

	// Structural check only (argv shape, element count) — this test's job
	// is confirming Render() produces one RunCommand of the right kind
	// among its five outputs, not re-verifying the payload's wire
	// semantics. A byte-exact JSON string here used to be this repo's
	// only check on that payload and passed for the entire lifetime of a
	// real {pattern,decision}-vs-{match,approval} field-name bug (see
	// ADR-0003): a hand-typed golden string can drift in lockstep with a
	// renderer bug written by the same author in the same sitting.
	// TestRender_BashPatternsCommand_MatchesOmpApprovalContract owns the
	// "does this payload mean the right thing to a real omp" question via
	// a decode-and-resolve check against the documented wire contract.
	cmd := outputByType[render.RunCommand](t, plan.Outputs)
	wantArgvPrefix := []string{"omp", "config", "set", "bash.patterns"}
	if len(cmd.Argv) != 5 || !reflect.DeepEqual(cmd.Argv[:4], wantArgvPrefix) {
		t.Errorf("got argv %v, want prefix %v plus one JSON payload element", cmd.Argv, wantArgvPrefix)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	if mcp.Path != "~/.omp/agent/mcp.json" {
		t.Errorf("got path %q, want ~/.omp/agent/mcp.json", mcp.Path)
	}
	wantMCP := map[string]any{
		"mcpServers": map[string]any{
			"fs": map[string]any{
				"lifecycle": "lazy",
				"command":   "mcp-fs",
				"args":      []string{"--root", "/tmp"},
			},
		},
	}
	if !reflect.DeepEqual(mcp.Object, wantMCP) {
		t.Errorf("got mcp object %#v, want %#v", mcp.Object, wantMCP)
	}
}

// TestRender_BashPatternsCommand_MatchesOmpApprovalContract decodes the
// rendered bash.patterns payload using the exact wire shape omp's own
// BashTool.approval() consumes (packages/coding-agent/src/tools/bash.ts's
// getBashApprovalPatternRules(): {match, approval} objects — also
// documented in omp's own tools/bash.md) and resolves representative
// commands against it. A golden-string assertion on the renderer's Argv
// output (as TestRender_LeadAndBuildProducesFourOutputs's wantArgv does)
// passes even if the field names silently drift from what the real
// consumer expects, since it only checks the renderer agrees with itself.
// This test instead exercises the actual downstream behavior the renderer
// exists to produce: given this rendered config, does a guardrail-listed
// command actually resolve to "prompt", and does an unmatched command fall
// through to the profile's base decision? The {pattern, decision} bug this
// test guards against decoded every rule's Match/Approval to "", so no
// rule (and not even the intended catch-all) would ever fire, silently
// turning every configured guardrail into a no-op — both assertions below
// would fail under that regression.
func TestRender_BashPatternsCommand_MatchesOmpApprovalContract(t *testing.T) {
	reg := &registry.Registry{
		Bash: registry.BashPolicy{
			Lists: map[string]map[string]registry.Decision{
				"guardrails": {"git push*": registry.Ask, "sudo *": registry.Ask},
			},
			DefaultLists: sp([]string{"guardrails"}),
			Profiles: map[string]registry.BashProfile{
				"global": {Base: registry.Allow, Lists: []string{"guardrails"}},
			},
		},
	}

	cmd, err := renderBashPatternsCommand(reg)
	if err != nil {
		t.Fatalf("renderBashPatternsCommand returned error: %v", err)
	}
	if len(cmd.Argv) != 5 {
		t.Fatalf("got argv %v, want 5 elements", cmd.Argv)
	}

	var wire []struct {
		Match    string `json:"match"`
		Approval string `json:"approval"`
	}
	if err := json.Unmarshal([]byte(cmd.Argv[4]), &wire); err != nil {
		t.Fatalf("decoding rendered bash.patterns as omp's {match,approval} wire shape: %v", err)
	}

	resolve := func(command string) string {
		for _, rule := range wire {
			if bashpolicy.MatchGlob(rule.Match, command) {
				return rule.Approval
			}
		}
		return "" // no rule matched at all — omp falls through to bare tier "exec"
	}

	if got := resolve("git push origin main"); got != "prompt" {
		t.Errorf("resolve(%q) = %q, want %q (guardrail must still gate a destructive command)",
			"git push origin main", got, "prompt")
	}
	if got := resolve("git status"); got != "allow" {
		t.Errorf("resolve(%q) = %q, want %q (unmatched command falls through to the profile's base decision)",
			"git status", got, "allow")
	}
}

// TestRender_GapsForUndeclaredCapabilities covers the gaps omp deliberately
// leaves to DetectGaps: agent_steps, per_agent_bash_policy. Fixture
// (b)-equivalent from the phase 2 task spec (external_directory isn't
// included: the registry schema has no field for it — see gaps.go's
// schema note).
func TestRender_GapsForUndeclaredCapabilities(t *testing.T) {
	steps := 7
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Role:   "primary",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You are lead."},
			},
			{
				Name:   "build",
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You build."},
				Steps:  &steps,
				Permissions: registry.Permissions{
					Bash: registry.BashPermission{Profile: "readonly"},
				},
			},
		},
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "local", Command: []registry.Value{{Literal: "gh-mcp"}}, Tools: []string{"repo_read"}},
		},
	}
	reg.Bash.Profiles["readonly"] = registry.BashProfile{Base: registry.Deny}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	byCapability := map[render.Capability][]render.Gap{}
	for _, g := range plan.Gaps {
		byCapability[g.Capability] = append(byCapability[g.Capability], g)
	}

	if len(byCapability[render.CapAgentSteps]) != 1 {
		t.Errorf("got %d agent_steps gaps, want 1: %+v", len(byCapability[render.CapAgentSteps]), byCapability[render.CapAgentSteps])
	}
	if len(byCapability[render.CapPerAgentBashPolicy]) != 1 {
		t.Errorf("got %d per_agent_bash_policy gaps, want 1: %+v", len(byCapability[render.CapPerAgentBashPolicy]), byCapability[render.CapPerAgentBashPolicy])
	}
	// mcp_tool_globs must NOT gap: omp declares CapMCPToolGlobs and
	// actually consumes MCPServer.Tools (subagent frontmatter grants,
	// tools.approval) — the github server's Tools allowlist above is
	// fully expressed, not dropped.
	if len(byCapability[render.CapMCPToolGlobs]) != 0 {
		t.Errorf("got %d mcp_tool_globs gaps, want 0 (omp supports tool globs): %+v", len(byCapability[render.CapMCPToolGlobs]), byCapability[render.CapMCPToolGlobs])
	}
	// primary_agent must NOT gap: omp declares CapPromptAppend, the
	// documented substitute mechanism.
	if len(byCapability[render.CapPrimaryAgent]) != 0 {
		t.Errorf("got %d primary_agent gaps, want 0 (prompt_append substitutes): %+v", len(byCapability[render.CapPrimaryAgent]), byCapability[render.CapPrimaryAgent])
	}
}

// TestRender_PrimaryAgentEditWriteGap covers issue #9: a primary agent
// setting permissions.edit/write (e.g. to force delegation of file changes
// to subagents, as opencode's agent.permission block does) has nowhere to
// go in omp's rendered output — the primary agent gets only a
// system-prompt append, never a per-agent tools: list — so the decision
// must surface as a primary_agent_tool_permission gap instead of being
// silently dropped.
func TestRender_PrimaryAgentEditWriteGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "lead",
				Role:   "primary",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You are lead."},
				Permissions: registry.Permissions{
					Edit:  "deny",
					Write: "deny",
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var found *render.Gap
	for i := range plan.Gaps {
		if plan.Gaps[i].Capability == render.CapPrimaryAgentToolPermission {
			found = &plan.Gaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a primary_agent_tool_permission gap, got %+v", plan.Gaps)
	}
	if found.Kind != render.GapSkip {
		t.Errorf("got kind %s, want skip", found.Kind)
	}
	if found.Subject != "agent:lead.permissions" {
		t.Errorf("got subject %q, want agent:lead.permissions", found.Subject)
	}
}

// TestRender_UnresolvableMCPServerSkippedWithGap covers a resolver failure
// for a local-transport server (a Value backed by a failing command).
func TestRender_UnresolvableMCPServerSkippedWithGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken",
				Transport: "local",
				Command:   []registry.Value{{From: "command", Run: []string{"/definitely/does/not/exist/agentcfg-nope"}}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var found bool
	for _, g := range plan.Gaps {
		if g.Capability == render.CapMCPLocalTransport && g.Subject == "mcp:broken" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a mcp_local_transport gap for mcp:broken, got %+v", plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (broken server should be skipped)", servers)
	}
}

// TestRender_RemoteTransportFailureGap covers a resolver failure for a
// remote-transport server (a Value backed by an unreadable file), the
// remote-transport counterpart to TestRender_UnresolvableMCPServerSkippedWithGap
// above. Regression coverage for the point-fix that stopped hardcoding
// CapMCPLocalTransport for every resolver failure regardless of the
// server's actual transport.
func TestRender_RemoteTransportFailureGap(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken-remote",
				Transport: "remote",
				URL:       registry.Value{From: "file", Path: "/definitely/does/not/exist/agentcfg-test-fixture.txt"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var found bool
	for _, g := range plan.Gaps {
		if g.Capability == render.CapMCPRemoteTransport && g.Subject == "mcp:broken-remote" {
			found = true
		}
		if g.Capability == render.CapMCPLocalTransport {
			t.Errorf("got mcp_local_transport gap for a remote-transport failure, want mcp_remote_transport: %+v", g)
		}
	}
	if !found {
		t.Fatalf("expected a mcp_remote_transport gap for mcp:broken-remote, got %+v", plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (broken server should be skipped)", servers)
	}
}

// TestRender_TransportFailureDoesNotAlsoGapToolGlobs covers a server
// that fails transport resolution (a CapMCPRemoteTransport failure) while
// also declaring a Tools allowlist: since omp declares CapMCPToolGlobs
// and genuinely consumes MCPServer.Tools, the allowlist itself is never
// a gap here regardless of the unrelated resolve failure — only the
// transport gap should surface for this server.
func TestRender_TransportFailureDoesNotAlsoGapToolGlobs(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken-slack",
				Transport: "remote",
				URL:       registry.Value{From: "file", Path: "/definitely/does/not/exist/agentcfg-test-fixture.txt"},
				Tools:     []string{"slack_search", "slack_send_message"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var haveTransport, haveToolGlobs bool
	for _, g := range plan.Gaps {
		if g.Subject != "mcp:broken-slack" {
			continue
		}
		switch g.Capability {
		case render.CapMCPRemoteTransport:
			haveTransport = true
		case render.CapMCPToolGlobs:
			haveToolGlobs = true
		}
	}
	if !haveTransport {
		t.Errorf("expected a mcp_remote_transport gap for mcp:broken-slack, got %+v", plan.Gaps)
	}
	if haveToolGlobs {
		t.Errorf("expected no mcp_tool_globs gap for mcp:broken-slack (omp supports tool globs), got %+v", plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (broken server should be skipped)", servers)
	}
}

// TestRender_MixedLocalAndRemoteMCPServers covers a registry that
// registers one local-transport and one remote-transport server side by
// side, both resolvable — the two transports must coexist in one
// mcpServers map without either one clobbering or gapping the other.
func TestRender_MixedLocalAndRemoteMCPServers(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{Name: "local-one", Transport: "local", Command: []registry.Value{{Literal: "gh-mcp"}}},
			{Name: "remote-one", Transport: "remote", URL: registry.Value{Literal: "https://api.example.com/mcp"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("got %d mcpServers, want 2: %#v", len(servers), servers)
	}
	local := servers["local-one"].(map[string]any)
	if local["command"] == nil {
		t.Errorf("got local-one %#v, want a command key", local)
	}
	if _, hasURL := local["url"]; hasURL {
		t.Errorf("got local-one %#v, want no url key", local)
	}
	remote := servers["remote-one"].(map[string]any)
	if remote["url"] != "https://api.example.com/mcp" {
		t.Errorf("got remote-one url %#v, want https://api.example.com/mcp", remote["url"])
	}
	if _, hasCommand := remote["command"]; hasCommand {
		t.Errorf("got remote-one %#v, want no command key", remote)
	}
}

// TestRender_MCPRemoteTransportRendersHeadersLazily covers the remote
// transport bug: headers must render using omp's own `!<command>`
// pre-connect resolution (docs/mcp-config.md "Pre-connect env/header
// resolution") for any source that can go stale between reconnects,
// instead of being silently dropped or baked in as a stale snapshot.
// Also covers the "type": "http" field a remote entry needs — omp
// defaults an untyped entry to stdio, which then fails validation for
// lacking "command".
func TestRender_MCPRemoteTransportRendersHeadersLazily(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "gh",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://api.githubcopilot.com/mcp/"},
				Headers: map[string]registry.Value{
					"X-Literal":     {Literal: "static-value"},
					"X-Env-Bare":    {From: "env", Name: "MY_TOKEN"},
					"Authorization": {From: "command", Run: []string{"gh", "auth", "token"}, Format: "Bearer {}"},
					"X-Cmd-Bare":    {From: "command", Run: []string{"gh", "auth", "token"}},
					"X-Cmd-Quoted":  {From: "command", Run: []string{"op", "read", "op://vault/github token"}},
					"X-Env-Fmt":     {From: "env", Name: "GITHUB_TOKEN", Format: "Bearer {}"},
					"X-File-Bare":   {From: "file", Path: "/etc/secret"},
					"X-File-Fmt":    {From: "file", Path: "/etc/secret", Format: "Bearer {}"},
					"X-File-Home":   {From: "file", Path: "~/secret-token"},
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	gh := servers["gh"].(map[string]any)
	if gh["type"] != "http" {
		t.Errorf(`got type %#v, want "http"`, gh["type"])
	}
	if gh["url"] != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("got url %#v, want https://api.githubcopilot.com/mcp/", gh["url"])
	}
	headers, ok := gh["headers"].(map[string]any)
	if !ok {
		t.Fatalf("got headers %#v, want a map", gh["headers"])
	}
	want := map[string]any{
		"X-Literal":     "static-value",
		"X-Env-Bare":    "MY_TOKEN",
		"Authorization": `!printf 'Bearer %s' "$(gh auth token)"`,
		"X-Cmd-Bare":    "!gh auth token",
		"X-Cmd-Quoted":  `!op read 'op://vault/github token'`,
		"X-Env-Fmt":     `!printf 'Bearer %s' "$GITHUB_TOKEN"`,
		"X-File-Bare":   "!cat -- '/etc/secret'",
		"X-File-Fmt":    `!printf 'Bearer %s' "$(cat -- '/etc/secret')"`,
		"X-File-Home":   "!cat -- " + shellQuote(homeSecretTokenPath(t)),
	}
	if !reflect.DeepEqual(headers, want) {
		t.Errorf("got headers %#v, want %#v", headers, want)
	}
}

// TestRender_MCPLocalTransportSplitsCommandAndArgs covers the local
// transport bug: omp's mcp-schema.json types "command" as a bare
// executable string with a separate "args" array, not a single argv
// array — bundling the whole resolved command into "command" produces
// an entry omp cannot launch.
func TestRender_MCPLocalTransportSplitsCommandAndArgs(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "filesystem",
				Transport: "local",
				Command: []registry.Value{
					{Literal: "npx"},
					{Literal: "-y"},
					{Literal: "@modelcontextprotocol/server-filesystem"},
					{Literal: "/tmp"},
				},
			},
			{
				Name:      "no-args",
				Transport: "local",
				Command:   []registry.Value{{Literal: "mcp-server-solo"}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0: %+v", len(plan.Gaps), plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)

	fs := servers["filesystem"].(map[string]any)
	if fs["command"] != "npx" {
		t.Errorf("got command %#v, want %q", fs["command"], "npx")
	}
	wantArgs := []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}
	if !reflect.DeepEqual(fs["args"], wantArgs) {
		t.Errorf("got args %#v, want %#v", fs["args"], wantArgs)
	}
	if _, hasURL := fs["url"]; hasURL {
		t.Errorf("got filesystem %#v, want no url key", fs)
	}

	noArgs := servers["no-args"].(map[string]any)
	if noArgs["command"] != "mcp-server-solo" {
		t.Errorf("got command %#v, want %q", noArgs["command"], "mcp-server-solo")
	}
	if _, hasArgs := noArgs["args"]; hasArgs {
		t.Errorf("got no-args %#v, want no args key (single-element command)", noArgs)
	}
}

// TestRender_MCPLocalTransportEmptyExecutableGapsServer covers a
// nonempty command list that resolves its first element to an empty
// string (e.g. an unset env var or a blank literal) — omp's schema
// requires a nonempty "command", so the server must be skipped with a
// gap instead of rendering "command": "".
func TestRender_MCPLocalTransportEmptyExecutableGapsServer(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "blank-executable",
				Transport: "local",
				Command:   []registry.Value{{Literal: ""}, {Literal: "--root"}, {Literal: "/tmp"}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var haveGap bool
	for _, g := range plan.Gaps {
		if g.Subject == "mcp:blank-executable" && g.Capability == render.CapMCPLocalTransport {
			haveGap = true
		}
	}
	if !haveGap {
		t.Errorf("expected a mcp_local_transport gap for mcp:blank-executable, got %+v", plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (blank-executable server should be skipped)", servers)
	}
}

// TestRender_MCPHeaderResolveFailureGapsServer covers a header whose
// source fails to render: env/file/command headers are now rendered
// lazily (as an omp `!<command>` directive, never read/run at agentcfg
// render time), so the only render-time failure left is a malformed
// declaration such as an empty command argv — the server must still be
// skipped with a gap, like an unresolvable url, rather than rendering
// with a missing or partially-resolved headers map.
func TestRender_MCPHeaderResolveFailureGapsServer(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:      "broken-headers",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://api.example.com/mcp"},
				Headers: map[string]registry.Value{
					"Authorization": {From: "command", Run: []string{}},
				},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	var haveGap bool
	for _, g := range plan.Gaps {
		if g.Subject == "mcp:broken-headers" && g.Capability == render.CapMCPRemoteTransport {
			haveGap = true
		}
	}
	if !haveGap {
		t.Errorf("expected a mcp_remote_transport gap for mcp:broken-headers, got %+v", plan.Gaps)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (broken server should be skipped)", servers)
	}
}

// TestRender_AgentTargetsRestrictsOutput covers agent.targets: an agent not
// targeting omp is excluded from the rebuilt agent directory, and an mcp
// server not targeting omp is excluded from mcp.json.
func TestRender_AgentTargetsRestrictsOutput(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "vscode-only", Role: "delegate", Class: "default", Prompt: registry.Prompt{Text: "x"}, Targets: []string{"vscode"}},
			{Name: "everywhere", Role: "delegate", Class: "default", Prompt: registry.Prompt{Text: "y"}},
		},
		MCPServers: []registry.MCPServer{
			{Name: "vscode-server", Transport: "local", Command: []registry.Value{{Literal: "x"}}, Targets: []string{"vscode"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	var names []string
	for _, f := range rebuild.Files {
		names = append(names, f.Path)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"everywhere.md"}) {
		t.Errorf("got agent files %v, want [everywhere.md]", names)
	}

	mcp := outputByType[render.MergeJSON](t, plan.Outputs)
	servers := mcp.Object["mcpServers"].(map[string]any)
	if len(servers) != 0 {
		t.Errorf("got mcpServers %#v, want empty (vscode-only server excluded)", servers)
	}
}

// TestRender_ComposeIntoPrimarySplicesIntoAppendSystem covers the core
// compose_into_primary behavior: the composing agent gets no standalone
// file, and its prompt is appended as a labeled section after the
// primary's own prompt in APPEND_SYSTEM.md.
func TestRender_ComposeIntoPrimarySplicesIntoAppendSystem(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "default", Prompt: registry.Prompt{Text: "You are the lead."}},
			{
				Name:        "plan",
				Role:        "advisory",
				Class:       "default",
				Description: "Architects before implementing",
				Prompt:      registry.Prompt{Text: "Research before coding."},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Errorf("got %d standalone agent files, want 0 (composing agent must not get one): %+v", len(rebuild.Files), rebuild.Files)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	want := "You are the lead.\n\n## plan: Architects before implementing\n\nResearch before coding."
	if string(appendFile.Content) != want {
		t.Errorf("got APPEND_SYSTEM.md content:\n%s\nwant:\n%s", appendFile.Content, want)
	}

	for _, g := range plan.Gaps {
		if g.Capability == render.CapComposeIntoPrimary {
			t.Errorf("did not expect a compose_into_primary gap when omp declares the capability, got %+v", g)
		}
	}
}

// TestRender_ComposeIntoPrimaryMultipleAgentsKeepDeclarationOrder covers
// more than one composed agent: sections must appear in registry
// declaration order, not alphabetical or any other order.
func TestRender_ComposeIntoPrimaryMultipleAgentsKeepDeclarationOrder(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "default", Prompt: registry.Prompt{Text: "Lead prompt."}},
			{Name: "zebra", Role: "advisory", Class: "default", Prompt: registry.Prompt{Text: "Zebra prompt."}},
			{Name: "alpha", Role: "advisory", Class: "default", Prompt: registry.Prompt{Text: "Alpha prompt."}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	want := "Lead prompt.\n\n## zebra\n\nZebra prompt.\n\n## alpha\n\nAlpha prompt."
	if string(appendFile.Content) != want {
		t.Errorf("got APPEND_SYSTEM.md content:\n%s\nwant (declaration order, zebra then alpha):\n%s", appendFile.Content, want)
	}
}

// TestRender_ComposeIntoPrimaryRespectsTargetsExclusion covers an agent
// that sets compose_into_primary: true but targets a different harness
// only: omp must neither emit a standalone file nor splice its content.
func TestRender_ComposeIntoPrimaryRespectsTargetsExclusion(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "default", Prompt: registry.Prompt{Text: "Lead prompt."}},
			{
				Name:    "opencode-only",
				Role:    "advisory",
				Class:   "default",
				Prompt:  registry.Prompt{Text: "Should never appear."},
				Targets: []string{"opencode"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Errorf("got %d standalone agent files, want 0: %+v", len(rebuild.Files), rebuild.Files)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	if string(appendFile.Content) != "Lead prompt." {
		t.Errorf("got APPEND_SYSTEM.md content %q, want %q (opencode-only agent must not splice)", appendFile.Content, "Lead prompt.")
	}
}

// TestRender_AdvisoryWithoutPrimaryFallsBackToStandaloneFile covers a
// registry with no role: primary agent at all: an advisory agent has
// nothing to compose into, so it must render as a normal standalone
// file instead of being silently dropped (it must not disappear from
// both the standalone directory and the — nonexistent — composed
// prompt).
func TestRender_AdvisoryWithoutPrimaryFallsBackToStandaloneFile(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "plan", Role: "advisory", Class: "default", Prompt: registry.Prompt{Text: "Research before coding."}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 1 || rebuild.Files[0].Path != "plan.md" {
		t.Fatalf("got files %+v, want exactly one plan.md standalone file", rebuild.Files)
	}

	for _, o := range plan.Outputs {
		if wf, ok := o.(render.WriteFile); ok && wf.Path == appendSystemPath {
			t.Errorf("got an APPEND_SYSTEM.md output %+v, want none (no primary agent to append to)", wf)
		}
	}
}

// TestRender_OpencodeOverrideStepRendersNothingForOmp covers a workflow
// step whose Opencode field names a standing OpencodeAgent persona: omp
// has no such concept, so the step must render nothing at all — no
// standalone agent file, no APPEND_SYSTEM.md composition — regardless of
// its own Role (delegate here) or Targets (unset, meaning "every
// harness", including omp).
func TestRender_OpencodeOverrideStepRendersNothingForOmp(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "default", Prompt: registry.Prompt{Text: "You are the lead."}},
			{
				Name:     "verify",
				Role:     "delegate",
				Class:    "default",
				Prompt:   registry.Prompt{Text: "Verify the change."},
				Opencode: &registry.StepOpencode{Agent: "qa"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Errorf("got %d standalone agent files, want 0 (opencode-override step must not render): %+v", len(rebuild.Files), rebuild.Files)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	if appendFile.Path != appendSystemPath {
		t.Fatalf("got path %q, want %q", appendFile.Path, appendSystemPath)
	}
	want := "You are the lead."
	if string(appendFile.Content) != want {
		t.Errorf("got APPEND_SYSTEM.md content %q, want %q (opencode-override step must not appear)", appendFile.Content, want)
	}
}

// TestRender_OpencodeOverrideAdvisoryStepExcludedFromComposedSections
// covers an advisory step whose Opencode field names a standing
// OpencodeAgent persona: before this change role: advisory plus a
// primary present would have spliced it into APPEND_SYSTEM.md (see
// TestRender_ComposeIntoPrimarySplicesIntoAppendSystem); with Opencode
// set it must be excluded entirely, leaving only the primary's own body.
func TestRender_OpencodeOverrideAdvisoryStepExcludedFromComposedSections(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "default", Prompt: registry.Prompt{Text: "You are the lead."}},
			{
				Name:        "review",
				Role:        "advisory",
				Class:       "default",
				Description: "Reviews before shipping",
				Prompt:      registry.Prompt{Text: "Review the change."},
				Opencode:    &registry.StepOpencode{Agent: "qa"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 0 {
		t.Errorf("got %d standalone agent files, want 0: %+v", len(rebuild.Files), rebuild.Files)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	want := "You are the lead."
	if string(appendFile.Content) != want {
		t.Errorf("got APPEND_SYSTEM.md content %q, want %q (opencode-override advisory step must not compose)", appendFile.Content, want)
	}
}

// TestRender_OpencodeOverridePrimaryWritesNoAppendSystem covers the
// primary step itself having Opencode set (e.g. workflow.yaml.tmpl's
// "orchestrate" step, compiled to opencode's "lead" persona): its own
// prompt is opencode-only and must not leak into APPEND_SYSTEM.md via
// Render's separate primary-body path (renderAgentFiles/composedSections
// alone don't cover this — Render reads the primary's prompt directly).
// With no advisory steps to compose either, nothing is left to write, so
// no WriteFile output should be produced at all.
func TestRender_OpencodeOverridePrimaryWritesNoAppendSystem(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:     "orchestrate",
				Role:     "primary",
				Class:    "default",
				Prompt:   registry.Prompt{Text: "Compiles to opencode's lead persona."},
				Opencode: &registry.StepOpencode{Agent: "lead"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	for _, o := range plan.Outputs {
		if wf, ok := o.(render.WriteFile); ok {
			t.Errorf("got a WriteFile output %+v, want none (opencode-overridden primary with no advisory steps has nothing to write)", wf)
		}
	}
}

// TestRender_OpencodeNilStepsRenderUnchanged is a non-regression check
// for the Opencode-override skip added to renderAgentFiles and
// composedSections: a step with Opencode left nil (every step before
// Agent.Opencode existed) must render exactly as before — a delegate
// step still gets its own standalone file, and an advisory step still
// composes into APPEND_SYSTEM.md when a primary exists. Adapted from
// TestRender_ComposeIntoPrimarySplicesIntoAppendSystem with an added
// delegate step.
func TestRender_OpencodeNilStepsRenderUnchanged(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{Name: "lead", Role: "primary", Class: "default", Prompt: registry.Prompt{Text: "You are the lead."}},
			{
				Name:        "plan",
				Role:        "advisory",
				Class:       "default",
				Description: "Architects before implementing",
				Prompt:      registry.Prompt{Text: "Research before coding."},
			},
			{Name: "build", Role: "delegate", Class: "default", Prompt: registry.Prompt{Text: "You build things."}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	rebuild := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(rebuild.Files) != 1 || rebuild.Files[0].Path != "build.md" {
		t.Fatalf("got files %+v, want exactly one build.md standalone file", rebuild.Files)
	}

	appendFile := outputByType[render.WriteFile](t, plan.Outputs)
	want := "You are the lead.\n\n## plan: Architects before implementing\n\nResearch before coding."
	if string(appendFile.Content) != want {
		t.Errorf("got APPEND_SYSTEM.md content:\n%s\nwant:\n%s", appendFile.Content, want)
	}
}

func TestCapabilities_OnlyDeclaresWhatIsBuilt(t *testing.T) {
	want := map[render.Capability]bool{
		render.CapAgentDefinitions:          true,
		render.CapComposeIntoPrimary:        true,
		render.CapPromptAppend:              true,
		render.CapPromptFileRef:             true,
		render.CapModelClassBinding:         true,
		render.CapBashOrderedList:           true,
		render.CapGlobalBashPolicy:          true,
		render.CapMCPLocalTransport:         true,
		render.CapMCPRemoteTransport:        true,
		render.CapMCPToolGlobs:              true,
		render.CapProjectModelPolicy:        true,
		render.CapCustomCommands:            true,
		render.CapStructuredWorkflowCommand: true,
	}
	got := New().Capabilities()
	if len(got) != len(want) {
		t.Fatalf("got %d capabilities, want %d: %v", len(got), len(want), got)
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("unexpected capability declared: %s", c)
		}
	}
}

func TestRender_CommandsRenderAsSkillFiles(t *testing.T) {
	reg := &registry.Registry{
		Bash: baseBashPolicy(),
		Commands: []registry.Command{
			{Name: "review", Description: "Reviews a diff", Prompt: registry.Prompt{Text: "Review the diff."}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(plan.Gaps) != 0 {
		t.Fatalf("got %d gaps, want 0 (omp declares CapCustomCommands): %+v", len(plan.Gaps), plan.Gaps)
	}

	tree := outputByType[render.RebuildTree](t, plan.Outputs)
	if tree.Dir != render.CommandsSkillsDir {
		t.Errorf("got Dir %q, want %q", tree.Dir, render.CommandsSkillsDir)
	}
	files, ok := tree.Dirs["review"]
	if !ok || len(files) != 1 || files[0].Path != "SKILL.md" {
		t.Fatalf("Dirs[review] = %+v, want exactly one SKILL.md entry", tree.Dirs["review"])
	}
	want := "---\nname: review\ndescription: \"Reviews a diff\"\n---\nReview the diff."
	if string(files[0].Content) != want {
		t.Errorf("Content = %q, want %q", files[0].Content, want)
	}
}

func TestID(t *testing.T) {
	if got := New().ID(); got != "omp" {
		t.Errorf("got ID %q, want omp", got)
	}
}

// findRunCommand returns the RunCommand among outputs whose `omp config
// set <key> <value>` Argv names key, failing the test if none match.
func findRunCommand(t *testing.T, outputs []render.Output, key string) render.RunCommand {
	t.Helper()
	for _, o := range outputs {
		cmd, ok := o.(render.RunCommand)
		if !ok || len(cmd.Argv) < 4 {
			continue
		}
		if cmd.Argv[3] == key {
			return cmd
		}
	}
	t.Fatalf("no RunCommand for omp config set %q found among %d outputs", key, len(outputs))
	panic("unreachable")
}

// TestRenderAgentFile_GrantsMCPToolIDsFromAgentMCP covers issue-class gap
// #1: an agent's mcp: entry must expand to the referenced server's Tools
// allowlist as explicit mcp__<server>_<tool> ids in the subagent's
// frontmatter tools: list — omp's tools: list is a hard visibility
// allowlist, not just an approval gate, so leaving these out would make
// the tools uncallable regardless of tools.approval.
func TestRenderAgentFile_GrantsMCPToolIDsFromAgentMCP(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "atlassian",
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You reach Atlassian."},
				MCP:    []registry.AgentMCP{{Server: "runlayer-atlassian"}},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "runlayer-atlassian",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://example.invalid/mcp"},
				Tools:     []string{"getJiraIssue", "search"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	dir := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(dir.Files) != 1 {
		t.Fatalf("got %d agent files, want 1", len(dir.Files))
	}
	content := string(dir.Files[0].Content)
	for _, want := range []string{"mcp__runlayer_atlassian_getjiraissue", "mcp__runlayer_atlassian_search"} {
		if !strings.Contains(content, want) {
			t.Errorf("agent file tools: line missing %q, got:\n%s", want, content)
		}
	}
}

// TestRenderAgentFile_Context7ToolIDsUseContextPrefix covers the one
// documented naming exception: omp addresses context7 internally as
// "context", not "context7".
func TestRenderAgentFile_Context7ToolIDsUseContextPrefix(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "scout",
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You research."},
				MCP:    []registry.AgentMCP{{Server: "context7"}},
			},
		},
		MCPServers: []registry.MCPServer{
			{Name: "context7", Transport: "remote", URL: registry.Value{Literal: "https://mcp.context7.com/mcp"}, Tools: []string{"query-docs"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	dir := outputByType[render.RebuildDir](t, plan.Outputs)
	content := string(dir.Files[0].Content)
	if !strings.Contains(content, "mcp__context_query_docs") {
		t.Errorf("agent file tools: line missing mcp__context_query_docs (context7 -> context prefix), got:\n%s", content)
	}
	if strings.Contains(content, "mcp__context7_") {
		t.Errorf("agent file tools: line must not use the raw context7 prefix, got:\n%s", content)
	}
}

// TestRender_ToolsApprovalCommandDerivedFromMCPServersAndExtra covers
// tools.approval: every configured server's Tools ids get "allow", merged
// with any static entries under harnesses.omp.extra["tools.approval"]
// (e.g. omp's own built-in write/edit/task/bash tools).
func TestRender_ToolsApprovalCommandDerivedFromMCPServersAndExtra(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Harnesses: map[string]registry.HarnessConfig{
			"omp": {Extra: map[string]any{
				"tools.approval": map[string]any{"write": "allow", "task": "allow"},
			}},
		},
		MCPServers: []registry.MCPServer{
			{Name: "github", Transport: "remote", URL: registry.Value{Literal: "https://example.invalid"}, Tools: []string{"search_code"}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	cmd := findRunCommand(t, plan.Outputs, "tools.approval")
	var approval map[string]string
	if err := json.Unmarshal([]byte(cmd.Argv[4]), &approval); err != nil {
		t.Fatalf("unmarshaling tools.approval JSON: %v", err)
	}
	want := map[string]string{"mcp__github_search_code": "allow", "write": "allow", "task": "allow"}
	if !reflect.DeepEqual(approval, want) {
		t.Errorf("got tools.approval %#v, want %#v", approval, want)
	}
}

// TestRender_ToolsApprovalCommandExpandsAskPatterns covers the fix
// documented in renderToolsApprovalCommand's doc comment and
// docs/decisions/0003: an agent's mcp: ask glob must expand into literal
// "prompt" tools.approval entries, because omp's real approval resolver
// (packages/coding-agent/src/tools/approval.ts's resolveApproval) does an
// exact map lookup with no glob support — unlike opencode, which can
// write the glob straight into a per-agent permission block. Two agents:
// one omp-targeting (github, ordinary case) and one opencode-only
// (slack, targets: [opencode]) — the opencode-only agent's ask pattern
// must still expand, because omp has no per-role tools.approval scoping
// to withhold it from and the pattern describes a real property of the
// tool, not of the step that happened to declare it.
func TestRender_ToolsApprovalCommandExpandsAskPatterns(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name: "github", Transport: "remote", URL: registry.Value{Literal: "https://example.invalid"},
				Tools: []string{"search_code", "create_issue", "merge_pull_request"},
			},
			{
				Name: "runlayer-slack", Transport: "remote", URL: registry.Value{Literal: "https://example.invalid"},
				Tools: []string{"slack_read_channel", "slack_send_message", "slack_schedule_message"},
			},
		},
		Agents: []registry.Agent{
			{
				Name: "github", Targets: []string{"opencode", "omp"},
				MCP: []registry.AgentMCP{{Server: "github", Ask: []string{"create_*", "merge_*"}}},
			},
			{
				Name: "slack", Targets: []string{"opencode"},
				MCP: []registry.AgentMCP{{Server: "runlayer-slack", Ask: []string{"slack_send_*", "slack_schedule_*"}}},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	cmd := findRunCommand(t, plan.Outputs, "tools.approval")
	var approval map[string]string
	if err := json.Unmarshal([]byte(cmd.Argv[4]), &approval); err != nil {
		t.Fatalf("unmarshaling tools.approval JSON: %v", err)
	}
	want := map[string]string{
		"mcp__github_search_code":                    "allow",
		"mcp__github_create_issue":                   "prompt",
		"mcp__github_merge_pull_request":             "prompt",
		"mcp__runlayer_slack_slack_read_channel":     "allow",
		"mcp__runlayer_slack_slack_send_message":     "prompt",
		"mcp__runlayer_slack_slack_schedule_message": "prompt",
	}
	if !reflect.DeepEqual(approval, want) {
		t.Errorf("got tools.approval %#v, want %#v", approval, want)
	}
}

// TestRender_ToolsApprovalCommandAbsentWhenNothingToSet covers the no-op
// case: no MCP servers and no harnesses.omp.extra["tools.approval"] means
// Render must not emit an empty `omp config set tools.approval '{}'` call.
func TestRender_ToolsApprovalCommandAbsentWhenNothingToSet(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	for _, o := range plan.Outputs {
		if cmd, ok := o.(render.RunCommand); ok && len(cmd.Argv) >= 4 && cmd.Argv[3] == "tools.approval" {
			t.Fatalf("got a tools.approval RunCommand %v, want none", cmd.Argv)
		}
	}
}

// TestRender_ExtraSettingsCommandsEmittedSortedExcludingToolsApproval
// covers harnesses.omp.extra's generic pass-through: every entry besides
// "tools.approval" becomes its own `omp config set <key> <json>` call,
// in sorted key order for a deterministic Plan.
func TestRender_ExtraSettingsCommandsEmittedSortedExcludingToolsApproval(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Harnesses: map[string]registry.HarnessConfig{
			"omp": {Extra: map[string]any{
				"tools.approvalMode":  "always-ask",
				"task.disabledAgents": []any{"reviewer", "security-reviewer"},
				"compaction.strategy": "context-full",
				"tools.approval":      map[string]any{"write": "allow"},
			}},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	// Scalar strings are passed bare (unquoted) — confirmed empirically,
	// omp's CLI rejects a JSON-quoted string as not matching its own
	// unquoted enum values (e.g. `"always-ask"` fails, `always-ask`
	// works).
	modeCmd := findRunCommand(t, plan.Outputs, "tools.approvalMode")
	if modeCmd.Argv[4] != "always-ask" {
		t.Errorf("got tools.approvalMode value %q, want unquoted %q", modeCmd.Argv[4], "always-ask")
	}
	stratCmd := findRunCommand(t, plan.Outputs, "compaction.strategy")
	if stratCmd.Argv[4] != "context-full" {
		t.Errorf("got compaction.strategy value %q, want unquoted %q", stratCmd.Argv[4], "context-full")
	}
	disabledCmd := findRunCommand(t, plan.Outputs, "task.disabledAgents")
	var disabled []string
	if err := json.Unmarshal([]byte(disabledCmd.Argv[4]), &disabled); err != nil {
		t.Fatalf("unmarshaling task.disabledAgents JSON: %v", err)
	}
	if !reflect.DeepEqual(disabled, []string{"reviewer", "security-reviewer"}) {
		t.Errorf("got task.disabledAgents %v, want [reviewer security-reviewer]", disabled)
	}

	// "tools.approval" must be handled once, by renderToolsApprovalCommand
	// — not duplicated here as a second, unmerged RunCommand.
	count := 0
	for _, o := range plan.Outputs {
		if cmd, ok := o.(render.RunCommand); ok && len(cmd.Argv) >= 4 && cmd.Argv[3] == "tools.approval" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d tools.approval RunCommands, want exactly 1", count)
	}
}

// TestRenderAgentFile_ExcludesToolIDsFromServerNotTargetingOmp covers a
// real gap: an MCP server scoped to a different renderer (targets:
// [opencode]) must not leak into an omp agent's frontmatter tools: list
// — that server is absent from omp's mcp.json and tools.approval, so
// granting its tool ids in frontmatter would let an agent "see" tools it
// can never actually reach.
func TestRenderAgentFile_ExcludesToolIDsFromServerNotTargetingOmp(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		Agents: []registry.Agent{
			{
				Name:   "github",
				Role:   "delegate",
				Class:  "default",
				Prompt: registry.Prompt{Text: "You reach GitHub."},
				MCP:    []registry.AgentMCP{{Server: "github"}},
			},
		},
		MCPServers: []registry.MCPServer{
			{
				Name:      "github",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://api.githubcopilot.com/mcp/"},
				Tools:     []string{"search_code"},
				Targets:   []string{"opencode"},
			},
		},
	}

	plan, err := New().Render(reg, render.Options{})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	dir := outputByType[render.RebuildDir](t, plan.Outputs)
	if len(dir.Files) != 1 {
		t.Fatalf("got %d agent files, want 1", len(dir.Files))
	}
	content := string(dir.Files[0].Content)
	if strings.Contains(content, "mcp__github_search_code") {
		t.Errorf("agent file tools: line must not grant mcp__github_search_code (server targets opencode only), got:\n%s", content)
	}
}

// TestRenderProject_NoExcludeForModelsMatchOmitsDisabledExtensions covers the
// no-op case: a project whose resolved classes don't route to any server's
// exclude_for_models model must get modelRoles only — no disabledExtensions
// key, and no second output file.
func TestRenderProject_NoExcludeForModelsMatchOmitsDisabledExtensions(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-opus", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:             "remote-one",
				Transport:        "remote",
				URL:              registry.Value{Literal: "https://api.example.com/mcp"},
				ExcludeForModels: []string{"mlx/default_model"},
			},
		},
	}

	classes := map[string]string{"default": "claude-opus", "smol": "claude-haiku"}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1: %#v", len(plan.Outputs), plan.Outputs)
	}

	cfg := outputByType[render.MergeYAML](t, plan.Outputs)
	if cfg.Path != "/repo/.omp/config.yml" {
		t.Errorf("got path %q, want /repo/.omp/config.yml", cfg.Path)
	}
	if _, ok := cfg.Object["disabledExtensions"]; ok {
		t.Errorf("disabledExtensions must be absent when nothing matches, got %#v", cfg.Object)
	}
	// Managed must not claim a key this render didn't write, or a later apply
	// would prune a user's own disabledExtensions entries.
	for _, k := range cfg.Managed {
		if k == "disabledExtensions" {
			t.Error("Managed must not list disabledExtensions when the key is not emitted")
		}
	}
}

// TestRenderProject_ExcludeForModelsMatchDisablesByExtensionID covers the happy
// path: a project whose resolved classes DO route to an excluded model gets the
// matching servers listed under disabledExtensions as omp "mcp:<name>" ids —
// the only lever omp honors, since it reads no <cwd>/.omp/mcp.json and has no
// per-role MCP visibility layer.
func TestRenderProject_ExcludeForModelsMatchDisablesByExtensionID(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "mlx/default_model", "smol": "claude-haiku"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:             "remote-one",
				Transport:        "remote",
				URL:              registry.Value{Literal: "https://api.example.com/mcp"},
				ExcludeForModels: []string{"mlx/default_model"},
			},
			{
				Name:      "remote-two",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://api.other.com/mcp"},
				Tools:     []string{"search", "fetch"},
				// No exclude_for_models — must stay mounted.
			},
		},
	}

	classes := map[string]string{"default": "mlx/default_model", "smol": "claude-haiku"}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	// One file only: omp takes the exclusion through a settings key, not a
	// separate MCP config, so there is nothing else to write.
	if len(plan.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1: %#v", len(plan.Outputs), plan.Outputs)
	}

	cfg := outputByType[render.MergeYAML](t, plan.Outputs)
	if cfg.Path != "/repo/.omp/config.yml" {
		t.Errorf("got config path %q, want /repo/.omp/config.yml", cfg.Path)
	}

	got, ok := cfg.Object["disabledExtensions"].([]string)
	if !ok {
		t.Fatalf("disabledExtensions missing or wrong type: %#v", cfg.Object["disabledExtensions"])
	}
	want := []string{"mcp:remote-one"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("disabledExtensions = %v, want %v", got, want)
	}

	// modelRoles must still be rendered alongside it.
	if roles, ok := cfg.Object["modelRoles"].(map[string]string); !ok || roles["default"] != "mlx/default_model" {
		t.Errorf("modelRoles not rendered correctly: %#v", cfg.Object["modelRoles"])
	}

	// Both keys must be declared managed so apply owns them.
	var managedRoles, managedDisabled bool
	for _, k := range cfg.Managed {
		switch k {
		case "modelRoles":
			managedRoles = true
		case "disabledExtensions":
			managedDisabled = true
		}
	}
	if !managedRoles || !managedDisabled {
		t.Errorf("Managed = %v, want both modelRoles and disabledExtensions", cfg.Managed)
	}
}

// TestRenderProject_ServerWithoutExcludeForModelsNeverDisabled covers the
// invariant: a server with no exclude_for_models — or an explicitly empty one
// — is never disabled, whatever the project's classes resolve to. Also pins
// the ordering guarantee, since Go map iteration over classes is random.
func TestRenderProject_ServerWithoutExcludeForModelsNeverDisabled(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "mlx/default_model", "smol": "mlx/default_model"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:             "zeta-excluded",
				Transport:        "remote",
				URL:              registry.Value{Literal: "https://excluded.example.com/mcp"},
				ExcludeForModels: []string{"mlx/default_model"},
			},
			{
				Name:      "always-on",
				Transport: "remote",
				URL:       registry.Value{Literal: "https://always.example.com/mcp"},
				// No exclude_for_models at all.
			},
			{
				Name:      "empty-exclude",
				Transport: "local",
				Command:   []registry.Value{{Literal: "mcp-server"}},
				// Empty list must behave exactly like an absent one.
				ExcludeForModels: []string{},
			},
			{
				Name:             "alpha-excluded",
				Transport:        "remote",
				URL:              registry.Value{Literal: "https://alpha.example.com/mcp"},
				ExcludeForModels: []string{"mlx/default_model"},
			},
		},
	}

	classes := map[string]string{"default": "mlx/default_model", "smol": "mlx/default_model"}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	cfg := outputByType[render.MergeYAML](t, plan.Outputs)
	got, ok := cfg.Object["disabledExtensions"].([]string)
	if !ok {
		t.Fatalf("disabledExtensions missing or wrong type: %#v", cfg.Object["disabledExtensions"])
	}

	// Sorted, and containing only the two servers that opted in.
	want := []string{"mcp:alpha-excluded", "mcp:zeta-excluded"}
	if len(got) != len(want) {
		t.Fatalf("disabledExtensions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("disabledExtensions = %v, want %v (sorted)", got, want)
		}
	}
}

// TestRenderProject_ExcludeForModelsKeyedOnDefaultClassOnly pins the scoping
// rule: disabledExtensions is session-wide and the cost it sheds is the
// primary session's, so only the `default` class decides. A project whose
// default is a large cloud model must keep its whole MCP surface even when a
// secondary class (here smol) routes to an excluded local model — the
// over-broad "any class matches" reading is what made this repo's earlier
// jq-based attempt at the same policy misfire.
func TestRenderProject_ExcludeForModelsKeyedOnDefaultClassOnly(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "claude-sonnet", "smol": "mlx/default_model"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:             "expensive",
				Transport:        "remote",
				URL:              registry.Value{Literal: "https://api.example.com/mcp"},
				ExcludeForModels: []string{"mlx/default_model"},
			},
		},
	}

	// default is a 1M-window cloud model; only smol is local.
	classes := map[string]string{"default": "claude-sonnet", "smol": "mlx/default_model"}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	cfg := outputByType[render.MergeYAML](t, plan.Outputs)
	if v, ok := cfg.Object["disabledExtensions"]; ok {
		t.Errorf("a non-default class routing to an excluded model must not drop servers, got %#v", v)
	}

	// Flipping default to the local model must drop it.
	classes["default"] = "mlx/default_model"
	plan, err = New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}
	cfg = outputByType[render.MergeYAML](t, plan.Outputs)
	got, ok := cfg.Object["disabledExtensions"].([]string)
	if !ok || len(got) != 1 || got[0] != "mcp:expensive" {
		t.Errorf("default routing to the excluded model must drop it, got %#v", cfg.Object["disabledExtensions"])
	}
}

// TestRenderProject_ExcludeForModelsIgnoresServersNotTargetingOmp pins that the
// exclusion list is scoped to servers this renderer actually mounts: an
// opencode-only server is never named in omp's disabledExtensions, because omp
// never paid for its schemas in the first place.
func TestRenderProject_ExcludeForModelsIgnoresServersNotTargetingOmp(t *testing.T) {
	reg := &registry.Registry{
		ModelClasses: map[string]string{"default": "mlx/default_model"},
		Bash:         baseBashPolicy(),
		MCPServers: []registry.MCPServer{
			{
				Name:             "opencode-only",
				Transport:        "remote",
				URL:              registry.Value{Literal: "https://oc.example.com/mcp"},
				Targets:          []string{"opencode"},
				ExcludeForModels: []string{"mlx/default_model"},
			},
		},
	}

	classes := map[string]string{"default": "mlx/default_model"}
	plan, err := New().(render.ProjectScopeRenderer).RenderProject(classes, reg, "/repo")
	if err != nil {
		t.Fatalf("RenderProject returned error: %v", err)
	}

	cfg := outputByType[render.MergeYAML](t, plan.Outputs)
	if v, ok := cfg.Object["disabledExtensions"]; ok {
		t.Errorf("disabledExtensions must omit servers not targeting omp, got %#v", v)
	}
}
