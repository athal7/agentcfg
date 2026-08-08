// Package registry loads and validates an agentcfg registry: a directory of
// YAML files describing model classes, bash policy, MCP servers, agents, and
// per-directory context overrides. It has no knowledge of how the registry
// directory came to exist — it only reads and validates YAML.
package registry

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Decision is a bash command policy outcome.
type Decision string

// Bash policy decision outcomes.
const (
	Allow Decision = "allow"
	Deny  Decision = "deny"
	Ask   Decision = "ask"
)

// Registry is the fully loaded, merged contents of an agentcfg registry
// directory (agentcfg.yaml plus its imports and local.yaml).
type Registry struct {
	RootDir      string
	Version      int
	Harnesses    map[string]HarnessConfig
	ModelClasses map[string]string
	Bash         BashPolicy
	Agents       []Agent
	MCPServers   []MCPServer
	Contexts     []Context
}

// Workflow is the content of the top-level workflow: key — the registry's
// one ordered pipeline of steps. v1 supports linear ordering only:
// declaration order is dispatch order, and each step's Role decides how
// it participates (see Agent.Role).
type Workflow struct {
	Steps []Agent `yaml:"steps"`
}

// HarnessConfig is the per-harness block under agentcfg.yaml's harnesses:
// key. Fields are a superset across all v1 harnesses; unused fields for a
// given harness are simply left empty.
type HarnessConfig struct {
	Out         string `yaml:"out,omitempty"`
	AgentsDir   string `yaml:"agents_dir,omitempty"`
	BashProfile string `yaml:"bash_profile,omitempty"`
}

// BashPolicy is the content of bash.yaml (merged with bash.d/*.yaml).
type BashPolicy struct {
	DefaultLists *[]string                      `yaml:"default_lists,omitempty"`
	Lists        map[string]map[string]Decision `yaml:"lists,omitempty"`
	Profiles     map[string]BashProfile         `yaml:"profiles,omitempty"`
}

// BashProfile is a single named entry under bash.profiles.
type BashProfile struct {
	Base  Decision `yaml:"base"`
	Lists []string `yaml:"lists,omitempty"`
	// DefaultLists is nil when the profile doesn't mention default_lists
	// (meaning: use the policy's default_lists chain). A non-nil false
	// suppresses the default chain entirely.
	DefaultLists *bool `yaml:"default_lists,omitempty"`
}

// Prompt is an agent's prompt source: exactly one of File or Text.
type Prompt struct {
	File string `yaml:"file,omitempty"`
	Text string `yaml:"text,omitempty"`
}

// BashPermission is permissions.bash, which accepts either a bare
// "allow"/"deny" string or an object naming a bash profile.
type BashPermission struct {
	Decision string
	Profile  string
}

// UnmarshalYAML implements yaml.BytesUnmarshaler, accepting either a bare
// string ("allow"/"deny") or an object of the form {profile: <name>}.
func (b *BashPermission) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err == nil {
		b.Decision = s
		return nil
	}
	var obj struct {
		Profile string `yaml:"profile"`
	}
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("permissions.bash must be a string (\"allow\"/\"deny\") or an object with a profile key: %w", err)
	}
	b.Profile = obj.Profile
	return nil
}

// IsZero reports whether no bash permission was specified at all.
func (b BashPermission) IsZero() bool {
	return b.Decision == "" && b.Profile == ""
}

// Permissions is an agent's permissions block.
type Permissions struct {
	Task  string         `yaml:"task,omitempty"`
	Edit  string         `yaml:"edit,omitempty"`
	Write string         `yaml:"write,omitempty"`
	Bash  BashPermission `yaml:"bash,omitempty"`
	Skill string         `yaml:"skill,omitempty"`

	// ExternalDirectory is a path-glob-to-Decision map controlling access
	// outside the working directory, e.g. {"*": "ask", "~/code/**": "allow"}.
	ExternalDirectory map[string]Decision `yaml:"external_directory,omitempty"`
}

// AgentMCP is one entry under an agent's mcp: list.
type AgentMCP struct {
	Server string   `yaml:"server"`
	Ask    []string `yaml:"ask,omitempty"`
}

// Agent is one entry under workflow.steps: — a workflow step's authoring
// unit. Each step declares what discipline it needs (Role), not which
// harness-specific mechanism expresses it; agentcfg compiles Role to each
// target renderer's native primitive for that discipline (see
// docs/schema.md and ADR-0001).
type Agent struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Class       string      `yaml:"class,omitempty"`
	Prompt      Prompt      `yaml:"prompt"`
	Targets     []string    `yaml:"targets,omitempty"`
	Steps       *int        `yaml:"steps,omitempty"`
	Permissions Permissions `yaml:"permissions,omitempty"`
	MCP         []AgentMCP  `yaml:"mcp,omitempty"`

	// Role is the step's discipline: "primary" (the workflow's one entry
	// point/orchestrator), "advisory" (reads and reasons, must not write
	// or edit — compiles to a real permission-enforced standalone
	// subagent on opencode, and is spliced into the primary's own prompt
	// on omp, which has no per-subagent enforcement surface to dispatch
	// it to safely), or "delegate" (independently dispatchable, full
	// permissions as declared — a standalone agent on every harness).
	// Defaults to "delegate" if omitted. See docs/schema.md.
	Role string `yaml:"role,omitempty"`

	// ResolvedPromptFile is populated by Load (see resolve.go) as the
	// absolute path of Prompt.File resolved against the registry root.
	// Empty when the agent uses Prompt.Text instead.
	ResolvedPromptFile string `yaml:"-"`
}

// Value is a string-typed field that can be a literal YAML string, or an
// object describing how to resolve the value at render time: from an
// environment variable, a file, or a command's stdout. Resolution is
// side-effecting and deliberately NOT performed during Load — callers
// invoke Resolve() explicitly when they need the value.
type Value struct {
	Literal string
	From    string
	Name    string   // from: env
	Path    string   // from: file
	Run     []string // from: command
	Format  string
}

// UnmarshalYAML implements yaml.BytesUnmarshaler, accepting either a bare
// string literal or a {from: ..., ...} object.
func (v *Value) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err == nil {
		v.Literal = s
		return nil
	}
	var obj struct {
		From   string   `yaml:"from"`
		Name   string   `yaml:"name"`
		Path   string   `yaml:"path"`
		Run    []string `yaml:"run"`
		Format string   `yaml:"format"`
	}
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("value must be a string or a {from: ...} object: %w", err)
	}
	v.From = obj.From
	v.Name = obj.Name
	v.Path = obj.Path
	v.Run = obj.Run
	v.Format = obj.Format
	return nil
}

// IsZero reports whether the value was never set (absent from YAML).
func (v Value) IsZero() bool {
	return v.Literal == "" && v.From == ""
}

// Resolve computes the value's runtime string. It is side-effecting: it may
// read environment variables, read files, or execute commands.
func (v Value) Resolve() (string, error) {
	var resolved string
	switch v.From {
	case "":
		resolved = v.Literal
	case "env":
		resolved = os.Getenv(v.Name)
	case "file":
		path, err := expandHome(v.Path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("resolving value from file %s: %w", v.Path, err)
		}
		resolved = strings.TrimSpace(string(data))
	case "command":
		if len(v.Run) == 0 {
			return "", fmt.Errorf("resolving value from command: run list is empty")
		}
		out, err := exec.Command(v.Run[0], v.Run[1:]...).Output()
		if err != nil {
			return "", fmt.Errorf("resolving value from command %v: %w", v.Run, err)
		}
		resolved = strings.TrimSpace(string(out))
	default:
		return "", fmt.Errorf("unknown value source %q", v.From)
	}
	if v.Format != "" {
		resolved = strings.ReplaceAll(v.Format, "{}", resolved)
	}
	return resolved, nil
}

// MCPServer is one entry under mcp.yaml's mcp_servers: list.
type MCPServer struct {
	Name      string           `yaml:"name"`
	Transport string           `yaml:"transport"`
	URL       Value            `yaml:"url,omitempty"`
	Command   []Value          `yaml:"command,omitempty"`
	Targets   []string         `yaml:"targets,omitempty"`
	Headers   map[string]Value `yaml:"headers,omitempty"`

	// Tools is an explicit tool-name allowlist, for harnesses that
	// enumerate MCP tools individually rather than enabling a whole
	// glob-based namespace (e.g. tools: [slack_search, slack_send_message]).
	// Empty means no such allowlist was declared.
	Tools []string `yaml:"tools,omitempty"`
}

// ContextMatch is the match: block of a contexts.yaml entry.
type ContextMatch struct {
	GitRemoteHost  string `yaml:"git_remote_host,omitempty"`
	GitRemoteOwner string `yaml:"git_remote_owner,omitempty"`
}

// Context is one entry under contexts.yaml's contexts: list.
type Context struct {
	Match        ContextMatch      `yaml:"match"`
	ModelClasses map[string]string `yaml:"model_classes,omitempty"`
}

// Matches reports whether every match key set on c equals the corresponding
// argument. Unset match keys act as wildcards.
func (c Context) Matches(host, owner string) bool {
	if c.Match.GitRemoteHost != "" && c.Match.GitRemoteHost != host {
		return false
	}
	if c.Match.GitRemoteOwner != "" && c.Match.GitRemoteOwner != owner {
		return false
	}
	return true
}

// ValidationError is a schema/consistency problem found while loading a
// registry. Unlike the error returned by Load, validation errors don't stop
// loading — all of them are collected so a `validate` command can report
// every problem in one pass.
type ValidationError struct {
	Message string
}

// Error implements the error interface for ValidationError.
func (e ValidationError) Error() string { return e.Message }

// ValidationWarning is a non-fatal advisory finding (e.g. an MCP-proxy
// agent that doesn't deny bash).
type ValidationWarning struct {
	Message string
}

// fileContents is the union of every top-level key that can appear in any
// registry YAML file. A given file typically only populates a subset; the
// loader uses presence (non-nil) to detect which keys a file declared.
type fileContents struct {
	Version      *int                     `yaml:"version"`
	Imports      []string                 `yaml:"imports"`
	Harnesses    map[string]HarnessConfig `yaml:"harnesses"`
	ModelClasses map[string]string        `yaml:"model_classes"`
	Bash         *BashPolicy              `yaml:"bash"`
	Workflow     *Workflow                `yaml:"workflow"`
	MCPServers   []MCPServer              `yaml:"mcp_servers"`
	Contexts     []Context                `yaml:"contexts"`
}

// expandHome expands a leading ~ or ~/ to the current user's home directory.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("expanding %s: %w", path, err)
	}
	if path == "~" {
		return u.HomeDir, nil
	}
	return filepath.Join(u.HomeDir, path[2:]), nil
}
