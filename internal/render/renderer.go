// Package render defines the harness-agnostic rendering contract:
// Renderer turns a loaded registry into a Plan of file/command Outputs plus
// a list of Gaps describing any registry feature the target harness can't
// express. Concrete renderers (internal/render/opencode, internal/render/omp,
// ...) implement Renderer; nothing in this package knows about a specific
// harness's file formats.
package render

import (
	"fmt"
	"io/fs"

	"github.com/athal7/agentcfg/internal/registry"
)

// Capability names one thing a renderer can express in a target harness's
// native configuration. Renderers declare the subset they implement;
// DetectGaps uses the declared set to find registry features that would
// otherwise be silently dropped.
type Capability string

// Capability constants enumerate every registry feature a renderer may or may
// not be able to express in its target harness's native configuration.
const (
	CapAgentDefinitions           Capability = "agent_definitions"
	CapPrimaryAgent               Capability = "primary_agent"
	CapPrimaryAgentToolPermission Capability = "primary_agent_tool_permission"
	CapComposeIntoPrimary         Capability = "compose_into_primary"
	CapPromptAppend               Capability = "prompt_append"
	CapPromptFileRef              Capability = "prompt_file_reference"
	CapAgentSteps                 Capability = "agent_steps"
	CapAgentTaskPermission        Capability = "agent_task_permission"
	CapModelLiteralBinding        Capability = "model_literal_binding"
	CapModelClassBinding          Capability = "model_class_binding"
	CapBashUnorderedMap           Capability = "bash_unordered_map"
	CapBashOrderedList            Capability = "bash_ordered_list"
	CapBashInteriorGlob           Capability = "bash_interior_glob"
	CapPerAgentBashPolicy         Capability = "per_agent_bash_policy"
	CapGlobalBashPolicy           Capability = "global_bash_policy"
	CapExternalDirectory          Capability = "external_directory_policy"
	CapMCPLocalTransport          Capability = "mcp_local_transport"
	CapMCPRemoteTransport         Capability = "mcp_remote_transport"
	CapMCPToolGlobs               Capability = "mcp_tool_globs"
	CapMCPPerToolAsk              Capability = "mcp_per_tool_ask"
	CapProjectModelPolicy         Capability = "project_model_policy"

	// CapCustomCommands marks support for custom commands (registry.Command),
	// rendered as Agent Skills SKILL.md files. Covers both a flat,
	// single-prompt command and a structured multi-step command flattened
	// into numbered prose — see CapStructuredWorkflowCommand for the
	// latter's native (non-flattened) rendering path. Unlike
	// CapPrimaryAgent/CapPromptAppend, this capability has no registered
	// substitute pair: both opencode and omp are confirmed to discover the
	// identical Agent Skills path natively, so both declare it directly
	// (see internal/render/commands.go) rather than one declaring a
	// differently-shaped equivalent.
	CapCustomCommands Capability = "custom_commands"

	// CapStructuredWorkflowCommand marks support for rendering a
	// multi-step command (registry.Command.Steps) into a harness's
	// native structured-workflow mechanism, instead of flattening the
	// steps into plain numbered prose (which CapCustomCommands alone
	// still does on any renderer). Currently only omp declares this,
	// via its `workflowz` magic keyword — see
	// internal/render/commands.go's workflowzDirective and
	// athal7/agentcfg#3.
	CapStructuredWorkflowCommand Capability = "structured_workflow_command"

	// CapHarnessPromptSuffix marks support for Agent.HarnessPrompts: a
	// renderer that declares this appends its own id's entry (if any)
	// after the agent's shared Prompt when rendering that agent's prompt
	// body/reference. A renderer without this capability would silently
	// drop the extra content for any agent that both targets it and sets
	// harness_prompts[<that renderer's id>] — see DetectGaps.
	CapHarnessPromptSuffix Capability = "harness_prompt_suffix"
)

// capabilitySubstitutePairs lists every pair of capabilities that express
// the same underlying registry feature via two different harness-native
// mechanisms — a renderer declaring either one has already covered the
// feature, not left a gap. See detectPrimaryAgentGap for the canonical
// case: a default-agent key (CapPrimaryAgent) vs. appending the primary
// agent's prompt as a whole-session system prompt (CapPromptAppend).
// CapModelLiteralBinding/CapModelClassBinding is the model-binding
// equivalent (embed the resolved literal vs. reference the class name and
// let the harness resolve it), and CapBashUnorderedMap/CapBashOrderedList
// is bash policy's (a glob-to-decision map vs. its specificity-ordered
// projection — bashpolicy.AsOrderedList is proven equivalent to the
// unordered most-specific-match semantics by
// TestDifferential_OrderedMatchesUnordered).
var capabilitySubstitutePairs = [][2]Capability{
	{CapPrimaryAgent, CapPromptAppend},
	{CapModelLiteralBinding, CapModelClassBinding},
	{CapBashUnorderedMap, CapBashOrderedList},
}

// SubstituteOf returns the capability that, if declared, satisfies the
// same underlying registry feature as c via a different harness-native
// mechanism, and whether c has a registered substitute at all.
func SubstituteOf(c Capability) (Capability, bool) {
	for _, pair := range capabilitySubstitutePairs {
		switch c {
		case pair[0]:
			return pair[1], true
		case pair[1]:
			return pair[0], true
		}
	}
	return "", false
}

// Options carries render-time inputs that aren't part of the registry
// itself.
type Options struct {
	// RegistryRoot is the absolute path to the registry directory. Agent
	// prompt files are usually read via Agent.ResolvedPromptFile (already
	// joined against the root by registry.Load), but RegistryRoot is here
	// for any renderer that needs the root directly.
	RegistryRoot string

	// ReadFile reads a file's contents. Defaults to os.ReadFile when nil.
	// Tests inject a fixture-backed implementation so Render exercises no
	// real filesystem I/O.
	ReadFile func(path string) ([]byte, error)
}

// Renderer turns a loaded registry into a Plan for one target harness.
type Renderer interface {
	ID() string
	Capabilities() []Capability

	// Render MUST be pure: no file writes, no exec, no network. Resolving
	// a registry.Value (env/file/command) is a read, not a write, and is
	// allowed here — but a resolved secret value must never appear in a
	// Gap.Detail or any other Plan field that might get logged or printed
	// by --explain.
	Render(reg *registry.Registry, opt Options) (*Plan, error)
}

// ProjectScopeRenderer is implemented only by renderers whose harness can
// express directory-local model-class policy (e.g. a per-project config
// file). Renderers that can't express this simply don't implement it.
type ProjectScopeRenderer interface {
	RenderProject(classes map[string]string, reg *registry.Registry, dir string) (*Plan, error)
}

// GapKind categorizes a Gap: whether the dropped feature was silently
// skipped, or the renderer produced a reduced/alternative expression of it.
type GapKind string

// GapKind constants categorize how a renderer handled a registry feature it
// couldn't fully express: silently skipped or reduced to an alternative form.
const (
	GapSkip      GapKind = "skip"
	GapReduction GapKind = "reduction"
)

// Gap records one registry feature a renderer couldn't fully express.
type Gap struct {
	Kind       GapKind
	Capability Capability
	Subject    string // e.g. "agent:build.permissions.bash"
	Detail     string // one human sentence stating the consequence; never a secret value
}

// Plan is the full set of native-config outputs and capability gaps
// produced by rendering one registry for one harness.
type Plan struct {
	Outputs []Output
	Gaps    []Gap
}

// Output is one file write or command a renderer wants applied. The apply
// layer (a later phase) is responsible for actually writing files or
// running commands; Render only describes what should happen.
type Output interface {
	// Describe returns a one-line human summary for --explain. It must
	// never include a resolved secret value.
	Describe() string
}

// WriteFile is a single file to write verbatim.
type WriteFile struct {
	Path    string
	Mode    fs.FileMode
	Content []byte
}

// Describe returns a one-line summary of the file write for --explain output.
func (w WriteFile) Describe() string {
	return fmt.Sprintf("write %s (%d bytes, mode %s)", w.Path, len(w.Content), w.Mode)
}

// MergeJSON merges Object into the JSON file at Path, touching only the
// dotted paths listed in Managed (a "*" path segment matches any key, e.g.
// "agent.*.model"). Keys outside Managed in an existing file are left
// alone.
type MergeJSON struct {
	Path    string
	Mode    fs.FileMode
	Managed []string
	Object  map[string]any
}

// Describe returns a one-line summary of the JSON merge for --explain output.
func (m MergeJSON) Describe() string {
	return fmt.Sprintf("merge JSON into %s (managed: %v)", m.Path, m.Managed)
}

// MergeYAML is MergeJSON's YAML-file equivalent.
type MergeYAML struct {
	Path    string
	Mode    fs.FileMode
	Managed []string
	Object  map[string]any
}

// Describe returns a one-line summary of the YAML merge for --explain output.
func (m MergeYAML) Describe() string {
	return fmt.Sprintf("merge YAML into %s (managed: %v)", m.Path, m.Managed)
}

// MergeTOML is MergeJSON's TOML-file equivalent.
type MergeTOML struct {
	Path    string
	Mode    fs.FileMode
	Managed []string
	Object  map[string]any
}

// Describe returns a one-line summary of the TOML merge for --explain output.
func (m MergeTOML) Describe() string {
	return fmt.Sprintf("merge TOML into %s (managed: %v)", m.Path, m.Managed)
}

// RebuildDir replaces every file matching Glob under Dir with exactly the
// contents of Files (files present in Dir but absent from Files are
// removed). Paths on each WriteFile in Files are relative to Dir.
type RebuildDir struct {
	Dir   string
	Glob  string
	Files []WriteFile
}

// Describe returns a one-line summary of the directory rebuild for --explain output.
func (r RebuildDir) Describe() string {
	return fmt.Sprintf("rebuild %s/%s (%d files)", r.Dir, r.Glob, len(r.Files))
}

// RebuildTree replaces the immediate subdirectories of Dir with exactly
// the entries in Dirs (subdirectory name -> files, each WriteFile's Path
// relative to that subdirectory): a subdirectory of Dir agentcfg itself
// rendered on a previous apply, but that's absent from Dirs now, is
// removed in full (recursively) — see internal/apply's
// rebuildTreeManifestFile for how apply distinguishes "agentcfg rendered
// this, and no longer wants it" from "something else lives here," since
// Dir is typically a harness-shared discovery path (e.g. Agent Skills'
// "~/.agents/skills"), not an agentcfg-exclusive directory.
//
// Unlike RebuildDir, which prunes stale files by matching Glob results
// against each kept file's *basename* (correct when every kept file's
// basename is unique, e.g. an agent's own "<name>.md"), RebuildTree keys
// by immediate-subdirectory name instead — the right primitive when
// every entry's own file has an identical basename across entries (every
// Agent Skill's file is named "SKILL.md"; basename-based pruning can't
// tell one command's SKILL.md from another's).
type RebuildTree struct {
	Dir  string
	Dirs map[string][]WriteFile
}

// Describe returns a one-line summary of the tree rebuild for --explain output.
func (r RebuildTree) Describe() string {
	return fmt.Sprintf("rebuild %s/*/ (%d subdirectories)", r.Dir, len(r.Dirs))
}

// RunCommand runs Argv as part of applying a Plan. Secret marks whether
// Argv or the command's output may contain sensitive data — when true,
// --explain must not print either verbatim.
type RunCommand struct {
	Argv   []string
	Why    string
	Secret bool
}

// Describe returns a one-line summary of the command execution for --explain output.
func (r RunCommand) Describe() string {
	if r.Secret {
		return fmt.Sprintf("run %s (%s) [output redacted]", r.Argv[0], r.Why)
	}
	return fmt.Sprintf("run %v (%s)", r.Argv, r.Why)
}
