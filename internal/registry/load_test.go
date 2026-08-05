package registry_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
)

// writeFixture creates a registry directory in t.TempDir() populated with
// files, keyed by path relative to the registry root. It returns the root
// directory.
func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// minimalFixtureFiles returns a complete, valid minimal registry's file set.
// Individual tests override or add entries to exercise specific behavior.
func minimalFixtureFiles() map[string]string {
	return map[string]string{
		"agentcfg.yaml": `
version: 1
imports:
  - models.yaml
  - bash.yaml
  - bash.d/*.yaml
  - mcp.yaml
  - agents.yaml
  - contexts.yaml
harnesses:
  opencode:
    out: ~/.config/opencode/opencode.json
  omp:
    agents_dir: ~/.omp/agent/agents
    bash_profile: global
`,
		"models.yaml": `
model_classes:
  default: anthropic/claude-sonnet-5
  smol:    anthropic/claude-haiku-4-5
  slow:    anthropic/claude-opus-5
  plan:    anthropic/claude-opus-5
`,
		"bash.yaml": `
bash:
  default_lists: [guardrails, git]
  lists:
    guardrails:
      "rm -rf /*": ask
      "sudo *": ask
    git:
      "git commit*": ask
      "git status*": allow
  profiles:
    global: { base: allow }
    lead:   { base: allow, lists: [lead] }
    locked: { base: deny, lists: [readonly], default_lists: false }
`,
		"mcp.yaml": `
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
`,
		"agents.yaml": `
agents:
  - name: lead
    description: "Primary orchestrator"
    mode: primary
    class: default
    prompt: { file: prompts/lead.md }
    permissions:
      task: allow
      edit: deny
      write: deny
      bash: { profile: lead }
      skill: deny
`,
		"contexts.yaml": `
contexts:
  - match: { git_remote_owner: athal7 }
    model_classes:
      default: anthropic/claude-sonnet-5
`,
		"prompts/lead.md": "You are the lead agent.\n",
	}
}

func loadFixture(t *testing.T, files map[string]string) (*registry.Registry, []registry.ValidationError, []registry.ValidationWarning, error) {
	t.Helper()
	dir := writeFixture(t, files)
	return registry.Load(dir)
}

func requireNoProblems(t *testing.T, errs []registry.ValidationError, warns []registry.ValidationWarning, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Load() returned hard error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("Load() returned unexpected validation errors: %v", errs)
	}
	if len(warns) != 0 {
		t.Fatalf("Load() returned unexpected validation warnings: %v", warns)
	}
}

func TestLoad_HappyPath(t *testing.T) {
	reg, errs, warns, err := loadFixture(t, minimalFixtureFiles())
	requireNoProblems(t, errs, warns, err)

	if reg.Version != 1 {
		t.Errorf("Version = %d, want 1", reg.Version)
	}
	if got, want := reg.ModelClasses["default"], "anthropic/claude-sonnet-5"; got != want {
		t.Errorf("ModelClasses[default] = %q, want %q", got, want)
	}
	if len(reg.Agents) != 1 || reg.Agents[0].Name != "lead" {
		t.Fatalf("Agents = %+v, want one agent named lead", reg.Agents)
	}
	if reg.Agents[0].Mode != "primary" {
		t.Errorf("Agents[0].Mode = %q, want primary", reg.Agents[0].Mode)
	}
	wantPrompt := filepath.Join(reg.RootDir, "prompts/lead.md")
	if reg.Agents[0].ResolvedPromptFile != wantPrompt {
		t.Errorf("ResolvedPromptFile = %q, want %q", reg.Agents[0].ResolvedPromptFile, wantPrompt)
	}
	if reg.Bash.Profiles["lead"].Base != registry.Allow {
		t.Errorf("bash profile lead base = %q, want allow", reg.Bash.Profiles["lead"].Base)
	}
	if len(reg.MCPServers) != 1 || reg.MCPServers[0].Name != "context7" {
		t.Fatalf("MCPServers = %+v", reg.MCPServers)
	}
	if len(reg.Contexts) != 1 {
		t.Fatalf("Contexts = %+v", reg.Contexts)
	}
}

func TestLoad_MissingEntryFile(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := registry.Load(dir)
	if err == nil {
		t.Fatal("expected error for missing agentcfg.yaml, got nil")
	}
	if want := "no agentcfg.yaml found at"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
	if want := "agentcfg init"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want it to mention %q", err.Error(), want)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	files := minimalFixtureFiles()
	files["models.yaml"] = "model_classes: [this is not a map"
	_, _, _, err := loadFixture(t, files)
	if err == nil {
		t.Fatal("expected hard error for malformed YAML, got nil")
	}
}

func TestLoad_DuplicateTopLevelKeyAcrossImports(t *testing.T) {
	files := minimalFixtureFiles()
	// Declare model_classes again from a second imported file.
	files["agentcfg.yaml"] = `
version: 1
imports:
  - models.yaml
  - models2.yaml
  - bash.yaml
  - agents.yaml
`
	files["models2.yaml"] = "model_classes:\n  default: something-else\n"
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("expected soft validation error, got hard error: %v", err)
	}
	if !anyErrorContains(errs, "model_classes declared in both") {
		t.Errorf("errs = %v, want one mentioning duplicate model_classes", errs)
	}
}

func TestLoad_LocalYAMLOverridesSilently(t *testing.T) {
	files := minimalFixtureFiles()
	files["local.yaml"] = `
model_classes:
  default: mlx/default_model
  smol: mlx/default_model
`
	reg, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if got, want := reg.ModelClasses["default"], "mlx/default_model"; got != want {
		t.Errorf("ModelClasses[default] = %q, want local.yaml override %q", got, want)
	}
}

func TestLoad_LocalYAMLAbsentIsFine(t *testing.T) {
	files := minimalFixtureFiles()
	delete(files, "local.yaml")
	_, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)
}

func TestLoad_BashDGlobMerge(t *testing.T) {
	files := minimalFixtureFiles()
	files["bash.d/lead.yaml"] = `
bash:
  lists:
    lead:
      "gh run view*": allow
`
	reg, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)

	if _, ok := reg.Bash.Lists["lead"]; !ok {
		t.Fatalf("expected bash.d/lead.yaml's lead list to be merged, got lists: %v", reg.Bash.Lists)
	}
	if reg.Bash.Lists["lead"]["gh run view*"] != registry.Allow {
		t.Errorf("lead list gh run view* = %v, want allow", reg.Bash.Lists["lead"]["gh run view*"])
	}
	// Original bash.yaml lists must still be present too.
	if _, ok := reg.Bash.Lists["guardrails"]; !ok {
		t.Errorf("expected bash.yaml's guardrails list to survive the merge, got: %v", reg.Bash.Lists)
	}
}

func TestLoad_BashDListNameCollision(t *testing.T) {
	files := minimalFixtureFiles()
	// bash.yaml already declares "guardrails"; redeclare it in bash.d.
	files["bash.d/dup.yaml"] = `
bash:
  lists:
    guardrails:
      "curl *": ask
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("expected soft validation error, got hard error: %v", err)
	}
	if !anyErrorContains(errs, `bash list "guardrails" declared in both`) {
		t.Errorf("errs = %v, want a bash list collision error naming both files", errs)
	}
}

// -- validation rule failure cases --

func TestValidate_DuplicateAgentName(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    class: default
    prompt: { text: "a" }
  - name: lead
    class: default
    prompt: { text: "b" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `duplicate agent name "lead"`) {
		t.Errorf("errs = %v, want duplicate agent name error", errs)
	}
}

func TestValidate_MoreThanOnePrimaryAgent(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    mode: primary
    class: default
    prompt: { text: "a" }
  - name: lead2
    mode: primary
    class: default
    prompt: { text: "b" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "exactly 0 or 1 agent may have mode: primary") {
		t.Errorf("errs = %v, want a too-many-primary-agents error", errs)
	}
}

func TestValidate_AgentUnknownClass(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    class: nonexistent
    prompt: { text: "a" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `agent "lead" references unknown model class "nonexistent"`) {
		t.Errorf("errs = %v, want unknown class error", errs)
	}
}

func TestValidate_PromptRequiresExactlyOneOfFileOrText(t *testing.T) {
	tests := []struct {
		name    string
		prompt  string
		wantMsg string
	}{
		{
			name:    "neither",
			prompt:  ``,
			wantMsg: `must set exactly one of prompt.file or prompt.text`,
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
			files["agents.yaml"] = "agents:\n  - name: lead\n    class: default\n    prompt: { " + tt.prompt + " }\n"
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

func TestValidate_PromptFileMustExist(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    class: default
    prompt: { file: prompts/does-not-exist.md }
`
	delete(files, "prompts/lead.md")
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "referenced prompt file does not exist") {
		t.Errorf("errs = %v, want a missing-prompt-file error", errs)
	}
}

func TestValidate_PromptFileResolvedRelativeToRegistryRoot(t *testing.T) {
	// Regression: prompt.file must resolve against the registry root, not
	// cwd or home. Load the fixture from an unrelated cwd to prove it.
	files := minimalFixtureFiles()
	dir := writeFixture(t, files)

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	reg, errs, warns, err := registry.Load(dir)
	requireNoProblems(t, errs, warns, err)
	if reg.Agents[0].ResolvedPromptFile != filepath.Join(dir, "prompts/lead.md") {
		t.Errorf("ResolvedPromptFile = %q, want path under registry root %q", reg.Agents[0].ResolvedPromptFile, dir)
	}
}

func TestValidate_McpWithoutBashDenyWarns(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: proxy
    class: default
    prompt: { text: "proxies mcp calls" }
    permissions:
      bash: allow
    mcp:
      - server: context7
`
	_, errs, warns, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if !anyWarningContains(warns, `agent "proxy" declares mcp servers but does not deny bash`) {
		t.Errorf("warns = %v, want mcp-without-bash-deny warning", warns)
	}
}

func TestValidate_McpWithBashDenyDoesNotWarn(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: proxy
    class: default
    prompt: { text: "proxies mcp calls" }
    permissions:
      bash: deny
    mcp:
      - server: context7
`
	_, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)
}

func TestValidate_BashPermissionBareStringMustBeAllowOrDeny(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    class: default
    prompt: { text: "a" }
    permissions:
      bash: ask
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `invalid permissions.bash "ask"`) {
		t.Errorf("errs = %v, want invalid bare bash permission error", errs)
	}
}

func TestValidate_BashPermissionProfileMustExist(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    class: default
    prompt: { text: "a" }
    permissions:
      bash: { profile: nonexistent }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `agent "lead" references unknown bash profile "nonexistent"`) {
		t.Errorf("errs = %v, want unknown bash profile error", errs)
	}
}

func TestValidate_BashListInvalidDecision(t *testing.T) {
	files := minimalFixtureFiles()
	files["bash.yaml"] = `
bash:
  default_lists: [guardrails]
  lists:
    guardrails:
      "rm -rf /*": maybe
  profiles:
    global: { base: allow }
    lead:   { base: allow }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `invalid decision "maybe"`) {
		t.Errorf("errs = %v, want invalid decision error", errs)
	}
}

func TestLoad_AgentExternalDirectoryPermission(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    description: "Primary orchestrator"
    mode: primary
    class: default
    prompt: { file: prompts/lead.md }
    permissions:
      task: allow
      edit: deny
      write: deny
      bash: { profile: lead }
      skill: deny
      external_directory:
        "*": ask
        "~/code/**": allow
`
	reg, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)

	want := map[string]registry.Decision{
		"*":         registry.Ask,
		"~/code/**": registry.Allow,
	}
	if !reflect.DeepEqual(reg.Agents[0].Permissions.ExternalDirectory, want) {
		t.Errorf("Permissions.ExternalDirectory = %v, want %v", reg.Agents[0].Permissions.ExternalDirectory, want)
	}
}

func TestValidate_ExternalDirectoryInvalidDecision(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    mode: primary
    class: default
    prompt: { text: "a" }
    permissions:
      external_directory:
        "*": maybe
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `agent "lead" permissions.external_directory pattern "*" has invalid decision "maybe"`) {
		t.Errorf("errs = %v, want invalid external_directory decision error", errs)
	}
}

func TestLoad_MCPServerTools(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: slack
    transport: remote
    url: https://mcp.slack.com/mcp
    tools: [slack_search, slack_send_message]
`
	reg, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)

	if len(reg.MCPServers) != 1 {
		t.Fatalf("MCPServers = %+v, want 1 entry", reg.MCPServers)
	}
	want := []string{"slack_search", "slack_send_message"}
	if !reflect.DeepEqual(reg.MCPServers[0].Tools, want) {
		t.Errorf("MCPServers[0].Tools = %v, want %v", reg.MCPServers[0].Tools, want)
	}
}

func TestValidate_ModelClassesReservedNamesMissing(t *testing.T) {
	files := minimalFixtureFiles()
	files["models.yaml"] = `
model_classes:
  slow: anthropic/claude-opus-5
`
	files["agents.yaml"] = `
agents:
  - name: lead
    class: slow
    prompt: { text: "a" }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `reserved model class "default" is missing`) {
		t.Errorf("errs = %v, want missing default class error", errs)
	}
	if !anyErrorContains(errs, `reserved model class "smol" is missing`) {
		t.Errorf("errs = %v, want missing smol class error", errs)
	}
}

func TestValidate_MCPServerNameUniqueness(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `duplicate mcp server name "context7"`) {
		t.Errorf("errs = %v, want duplicate mcp server name error", errs)
	}
}

func TestValidate_MCPServerTransportInvalid(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: carrier-pigeon
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `invalid transport "carrier-pigeon"`) {
		t.Errorf("errs = %v, want invalid transport error", errs)
	}
}

func TestValidate_MCPServerRemoteRequiresURL(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: remote
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `transport: remote but no url`) {
		t.Errorf("errs = %v, want remote-requires-url error", errs)
	}
}

func TestValidate_MCPServerLocalRequiresCommand(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: firefox-devtools
    transport: local
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `transport: local but no command`) {
		t.Errorf("errs = %v, want local-requires-command error", errs)
	}
}

func TestValidate_ContextMatchRequiresAtLeastOneKey(t *testing.T) {
	files := minimalFixtureFiles()
	files["contexts.yaml"] = `
contexts:
  - match: {}
    model_classes:
      default: anthropic/claude-sonnet-5
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "must set at least one of match.git_remote_host or match.git_remote_owner") {
		t.Errorf("errs = %v, want empty match error", errs)
	}
}

func TestContext_Matches(t *testing.T) {
	tests := []struct {
		name  string
		match registry.ContextMatch
		host  string
		owner string
		want  bool
	}{
		{"both match", registry.ContextMatch{GitRemoteHost: "github.com", GitRemoteOwner: "athal7"}, "github.com", "athal7", true},
		{"host mismatch", registry.ContextMatch{GitRemoteHost: "github.com"}, "gitlab.com", "athal7", false},
		{"owner mismatch", registry.ContextMatch{GitRemoteOwner: "athal7"}, "github.com", "other", false},
		{"owner-only wildcard host", registry.ContextMatch{GitRemoteOwner: "0din-ai"}, "any-host.example", "0din-ai", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := registry.Context{Match: tt.match}
			if got := c.Matches(tt.host, tt.owner); got != tt.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.host, tt.owner, got, tt.want)
			}
		})
	}
}

// -- small local helpers --

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func anyErrorContains(errs []registry.ValidationError, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

func anyWarningContains(warns []registry.ValidationWarning, substr string) bool {
	for _, w := range warns {
		if strings.Contains(w.Message, substr) {
			return true
		}
	}
	return false
}

// -- Finding 1: explicit empty default_lists should be tracked as a declaration --

func TestLoad_BashDefaultListsExplicitEmptyIsCollision(t *testing.T) {
	files := minimalFixtureFiles()
	// bash.yaml declares default_lists; bash.d/override.yaml explicitly
	// sets default_lists: [] (empty list, not absent). This should be a
	// collision because the second file *declared* the field.
	files["bash.d/override.yaml"] = `
bash:
  default_lists: []
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("expected soft validation error, got hard error: %v", err)
	}
	if !anyErrorContains(errs, `bash.default_lists declared in both`) {
		t.Errorf("errs = %v, want a bash.default_lists collision error", errs)
	}
}

func TestLoad_BashDefaultListsExplicitEmptyOverrides(t *testing.T) {
	files := minimalFixtureFiles()
	// local.yaml explicitly sets default_lists: [] — this should override
	// the value from bash.yaml (local.yaml replaces whole keys).
	// Use an agent that doesn't reference a bash profile, since local.yaml's
	// bash: block replaces the entire Bash struct (wiping profiles).
	files["agents.yaml"] = `
agents:
  - name: lead
    description: "Primary orchestrator"
    mode: primary
    class: default
    prompt: { text: "You are the lead." }
    permissions:
      task: allow
      edit: deny
      write: deny
      skill: deny
`
	files["local.yaml"] = `
bash:
  default_lists: []
`
	reg, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if reg.Bash.DefaultLists == nil || len(*reg.Bash.DefaultLists) != 0 {
		t.Errorf("Bash.DefaultLists = %v, want empty slice after local.yaml override", reg.Bash.DefaultLists)
	}
}

// -- Finding 2: prompt.file path traversal must be rejected --

func TestValidate_PromptFileTraversalRejected(t *testing.T) {
	files := minimalFixtureFiles()
	files["agents.yaml"] = `
agents:
  - name: lead
    class: default
    prompt: { file: ../../etc/passwd }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, "prompt file escapes registry root") {
		t.Errorf("errs = %v, want a path-traversal rejection error", errs)
	}
}

func TestValidate_PromptFileRelativeInRootStillWorks(t *testing.T) {
	files := minimalFixtureFiles()
	// A normal relative path inside the registry should still resolve fine.
	files["agents.yaml"] = `
agents:
  - name: lead
    class: default
    prompt: { file: prompts/lead.md }
`
	reg, errs, warns, err := loadFixture(t, files)
	requireNoProblems(t, errs, warns, err)
	wantPrompt := filepath.Join(reg.RootDir, "prompts/lead.md")
	if reg.Agents[0].ResolvedPromptFile != wantPrompt {
		t.Errorf("ResolvedPromptFile = %q, want %q", reg.Agents[0].ResolvedPromptFile, wantPrompt)
	}
}

// -- Finding 3: Value shape validation --

func TestValidate_ValueFromCommandRequiresRun(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
    headers:
      X-Run: { from: command }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `from: command requires a run list`) {
		t.Errorf("errs = %v, want from:command-requires-run error", errs)
	}
}

func TestValidate_ValueFromEnvRequiresName(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
    headers:
      X-Env: { from: env }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `from: env requires a name`) {
		t.Errorf("errs = %v, want from:env-requires-name error", errs)
	}
}

func TestValidate_ValueFromFileRequiresPath(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
    headers:
      X-File: { from: file }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `from: file requires a path`) {
		t.Errorf("errs = %v, want from:file-requires-path error", errs)
	}
}

func TestValidate_ValueFromUnknownSourceRejected(t *testing.T) {
	files := minimalFixtureFiles()
	files["mcp.yaml"] = `
mcp_servers:
  - name: context7
    transport: remote
    url: https://mcp.context7.com/mcp
    headers:
      X-Bad: { from: magic }
`
	_, errs, _, err := loadFixture(t, files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !anyErrorContains(errs, `unknown value source "magic"`) {
		t.Errorf("errs = %v, want unknown value source error", errs)
	}
}
