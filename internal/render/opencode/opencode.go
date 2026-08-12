// Package opencode renders an agentcfg registry into opencode's native
// opencode.json configuration.
package opencode

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/athal7/agentcfg/internal/bashpolicy"
	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/render"
)

// id is the renderer identifier used by ID() and capability checks.
const id = "opencode"

// configPath is left unexpanded (literal "~"): tilde-expansion is the
// apply layer's job, not Render's — Render must stay pure.
const configPath = "~/.config/opencode/opencode.json"

// globalBashProfile is the profile name every renderer compiles for its
// harness-wide bash policy baseline.
const globalBashProfile = "global"

// New returns a Renderer that produces opencode's native opencode.json.
func New() render.Renderer { return renderer{} }

// renderer implements render.Renderer for the opencode harness.
type renderer struct{}

// ID returns the renderer's identifier, "opencode".
func (renderer) ID() string { return id }

// Capabilities returns the set of registry features this renderer can
// express in opencode's native config. CapPrimaryAgentToolPermission is
// included because renderAgent (below) applies permissions.edit/write to
// every agent uniformly, including the primary one — opencode's
// default_agent key still gets a full agent.<name>.permission block, unlike
// omp's primary agent, which only gets a system-prompt append with no
// permission surface at all.
func (renderer) Capabilities() []render.Capability {
	return []render.Capability{
		render.CapAgentDefinitions,
		render.CapPrimaryAgent,
		render.CapPrimaryAgentToolPermission,
		render.CapPromptFileRef,
		render.CapAgentSteps,
		render.CapAgentTaskPermission,
		render.CapModelLiteralBinding,
		render.CapBashUnorderedMap,
		render.CapBashInteriorGlob,
		render.CapPerAgentBashPolicy,
		render.CapGlobalBashPolicy,
		render.CapExternalDirectory,
		render.CapMCPLocalTransport,
		render.CapMCPRemoteTransport,
		render.CapMCPToolGlobs,
		render.CapMCPPerToolAsk,
		render.CapProjectModelPolicy,
		render.CapCustomCommands,
	}
}

// reservedTopLevelKeys are the opencode.json top-level keys Render always
// manages itself. harnesses.opencode.extra must not redeclare any of
// them, or the two writers would silently fight over the same key on
// every apply — the exact failure mode this renderer was built to avoid
// (see the harness_prompts/extra design note in docs/schema.md).
var reservedTopLevelKeys = map[string]bool{
	"default_agent": true,
	"agent":         true,
	"tools":         true,
	"mcp":           true,
	"model":         true,
	"small_model":   true,
}

// Render produces a Plan that merges the registry into opencode's native
// opencode.json, covering model classes, agents, permissions, MCP tools,
// MCP server configuration, and any harnesses.opencode.extra passthrough.
func (r renderer) Render(reg *registry.Registry, opt render.Options) (*render.Plan, error) {
	plan := &render.Plan{}
	plan.Gaps = append(plan.Gaps, render.DetectGaps(reg, r.Capabilities())...)

	readFile := opt.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}

	globalBash, err := bashpolicy.Compile(reg.Bash, globalBashProfile)
	if err != nil {
		return nil, fmt.Errorf("opencode: compiling global bash policy: %w", err)
	}

	obj := map[string]any{
		"model":       reg.ModelClasses["default"],
		"small_model": reg.ModelClasses["smol"],
		"permission":  renderGlobalPermission(globalBash),
	}

	if primary := render.PrimaryAgent(reg); primary != nil {
		if primary.Opencode != nil {
			if _, ok := findOpencodeAgent(reg, primary.Opencode.Agent); ok {
				obj["default_agent"] = primary.Opencode.Agent
			} else {
				obj["default_agent"] = primary.Name
			}
		} else {
			obj["default_agent"] = primary.Name
		}
	}

	agentsObj := map[string]any{}
	renderedOpencodeAgents := map[string]bool{}
	for _, a := range reg.Agents {
		if a.Opencode != nil {
			if renderedOpencodeAgents[a.Opencode.Agent] {
				continue
			}
			oa, ok := findOpencodeAgent(reg, a.Opencode.Agent)
			if !ok {
				continue
			}
			agentObj, err := renderOpencodeAgentPersona(reg, oa, a.Role, a.Steps)
			if err != nil {
				return nil, err
			}
			agentsObj[oa.Name] = agentObj
			renderedOpencodeAgents[a.Opencode.Agent] = true
			continue
		}
		agentObj, err := renderAgent(reg, a)
		if err != nil {
			return nil, err
		}
		agentsObj[a.Name] = agentObj
	}
	obj["agent"] = agentsObj

	toolsObj := map[string]any{}
	for _, s := range reg.MCPServers {
		toolsObj[s.Name+"_*"] = false
	}
	obj["tools"] = toolsObj

	mcpObj := map[string]any{}
	for _, s := range reg.MCPServers {
		entry, ok, gap := renderMCPServer(s)
		if gap != nil {
			plan.Gaps = append(plan.Gaps, *gap)
		}
		if ok {
			mcpObj[s.Name] = entry
		}
	}
	obj["mcp"] = mcpObj

	managed := []string{"default_agent", "agent", "tools", "mcp", "model", "small_model"}
	managed = append(managed, managedPermissionPaths()...)

	extraManaged, err := applyExtra(obj, reg.Harnesses[id].Extra)
	if err != nil {
		return nil, err
	}
	managed = append(managed, extraManaged...)

	plan.Outputs = append(plan.Outputs, render.MergeJSON{
		Path:    configPath,
		Mode:    0600,
		Managed: managed,
		Object:  obj,
	})

	commandsTree, err := render.RenderCommands(render.CommandsSkillsDir, reg, readFile)
	if err != nil {
		return nil, fmt.Errorf("opencode: rendering commands: %w", err)
	}
	plan.Outputs = append(plan.Outputs, commandsTree)

	return plan, nil
}

// applyExtra splices harnesses.opencode.extra into obj, returning the
// dotted paths it added so the caller can mark them Managed. Each key is
// a dotted JSON path (e.g. "server", "permission.grep"): a single-segment
// key is a full top-level subtree replace; a dotted key descends into
// (creating, if absent) intermediate objects and replaces only the final
// leaf, so "permission.grep" merges alongside the permission leaves this
// renderer already owns (managedPermissionPaths) rather than clobbering
// them. Keys colliding with a reservedTopLevelKeys entry, or a
// permission leaf Render already manages, are rejected — the registry
// author must not declare the same key twice under two different
// mechanisms.
func applyExtra(obj map[string]any, extra map[string]any) ([]string, error) {
	if len(extra) == 0 {
		return nil, nil
	}

	reservedPermissionLeaves := map[string]bool{"bash": true}
	for _, leaf := range permissionKey {
		reservedPermissionLeaves[leaf] = true
	}

	managed := make([]string, 0, len(extra))
	for key, value := range extra {
		if key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.Contains(key, "..") {
			return nil, fmt.Errorf("opencode: harnesses.opencode.extra key %q must contain non-empty JSON path segments", key)
		}
		root, suffix, dotted := strings.Cut(key, ".")
		if reservedTopLevelKeys[root] {
			return nil, fmt.Errorf("opencode: harnesses.opencode.extra key %q collides with a key Render already manages", key)
		}
		if root == "permission" {
			leaf, _, nested := strings.Cut(suffix, ".")
			if !dotted || leaf == "" || nested {
				return nil, fmt.Errorf(`opencode: harnesses.opencode.extra key %q must be "permission.<leaf>" (Render already owns the bare "permission" object)`, key)
			}
			if reservedPermissionLeaves[leaf] {
				return nil, fmt.Errorf("opencode: harnesses.opencode.extra key %q collides with a permission leaf Render already manages", key)
			}
		}
		setDottedPath(obj, key, value)
		managed = append(managed, key)
	}
	sort.Strings(managed)
	return managed, nil
}

// setDottedPath sets value at a dotted path within obj, creating any
// missing intermediate objects along the way. A key with no dot is a
// direct top-level assignment.
func setDottedPath(obj map[string]any, dotted string, value any) {
	segments := strings.Split(dotted, ".")
	cur := obj
	for _, seg := range segments[:len(segments)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	cur[segments[len(segments)-1]] = value
}

// projectConfigPath is a literal, non-"~" path relative to the resolved
// project directory — unlike configPath (the user-scope opencode.json),
// this file lives inside the project itself, so it's never expanded
// against the home directory.
const projectConfigDir, projectConfigFile = ".opencode", "opencode.json"

// RenderProject implements render.ProjectScopeRenderer: a directory-local
// opencode.json that pins model/small_model and each class-bearing agent's
// model to the literal resolved from classes (which may already carry a
// matched Context's overrides — see internal/scope). This reuses the same
// "class name -> literal" lookup renderAgent uses for the user-scope
// config, just sourced from the caller's classes map instead of
// reg.ModelClasses directly.
func (r renderer) RenderProject(classes map[string]string, reg *registry.Registry, dir string) (*render.Plan, error) {
	agentsObj := map[string]any{}
	for _, a := range reg.Agents {
		if a.Class == "" {
			continue
		}
		model, ok := classes[a.Class]
		if !ok {
			// A real class name should always resolve; skip defensively
			// rather than writing a zero-value model.
			continue
		}
		agentsObj[a.Name] = map[string]any{"model": model}
	}

	obj := map[string]any{
		"model":       classes["default"],
		"small_model": classes["smol"],
		"agent":       agentsObj,
	}

	return &render.Plan{
		Outputs: []render.Output{
			render.MergeJSON{
				Path:    filepath.Join(dir, projectConfigDir, projectConfigFile),
				Mode:    0600,
				Managed: []string{"model", "small_model", "agent.*.model"},
				Object:  obj,
			},
		},
	}, nil
}

// renderGlobalPermission builds the harness-wide default permission block:
// bash comes from the compiled global profile; every other supported
// canonical has no registry-level global source, so it defaults to allow.
func renderGlobalPermission(globalBash map[string]bashpolicy.Decision) map[string]any {
	perm := map[string]any{
		"bash": bashMapToAny(bashpolicy.AsMap(globalBash)),
	}
	for _, key := range permissionKey {
		perm[key] = "allow"
	}
	return perm
}

// agentBashMap resolves an agent's effective bash decision map: a named
// profile compiles via bashpolicy.Compile; a bare allow/deny becomes a
// single "*" rule; an unset permission falls back to the global profile.
func agentBashMap(reg *registry.Registry, b registry.BashPermission) (map[string]bashpolicy.Decision, error) {
	switch {
	case b.Profile != "":
		return bashpolicy.Compile(reg.Bash, b.Profile)
	case b.Decision != "":
		return map[string]bashpolicy.Decision{"*": bashpolicy.Decision(b.Decision)}, nil
	default:
		return bashpolicy.Compile(reg.Bash, globalBashProfile)
	}
}

// renderAgent builds the opencode agent object for a single registry.Agent,
// including its permission block (bash from agentBashMap, task/edit/write
// from Permissions, external_directory, and MCP tool/ask settings).
func renderAgent(reg *registry.Registry, a registry.Agent) (map[string]any, error) {
	bashMap, err := agentBashMap(reg, a.Permissions.Bash)
	if err != nil {
		return nil, fmt.Errorf("opencode: agent %q: %w", a.Name, err)
	}

	perm := map[string]any{
		"bash": bashMapToAny(bashpolicy.AsMap(bashMap)),
	}
	if a.Permissions.Task != "" {
		perm["task"] = a.Permissions.Task
	}
	if a.Permissions.Edit != "" {
		perm["edit"] = a.Permissions.Edit
	}
	if a.Permissions.Write != "" {
		perm["write"] = a.Permissions.Write
	}
	if len(a.Permissions.ExternalDirectory) > 0 {
		perm["external_directory"] = decisionMapToAny(a.Permissions.ExternalDirectory)
	}

	tools := map[string]any{}
	for _, m := range a.MCP {
		tools[m.Server+"_*"] = true
		for _, pattern := range m.Ask {
			perm[m.Server+"_"+pattern] = "ask"
		}
	}

	opencodeMode := "subagent"
	if a.Role == "primary" {
		opencodeMode = "primary"
	}
	agentObj := map[string]any{
		"description": a.Description,
		"mode":        opencodeMode,
		"model":       reg.ModelClasses[a.Class],
		"prompt":      renderPrompt(a),
		"permission":  perm,
	}
	if len(tools) > 0 {
		agentObj["tools"] = tools
	}
	if a.Steps != nil {
		agentObj["steps"] = *a.Steps
	}
	return agentObj, nil
}

// renderPrompt returns opencode's "{file:...}" load-at-runtime reference
// for a file-backed prompt, or the literal text for an inline prompt.
func renderPrompt(a registry.Agent) string {
	if a.ResolvedPromptFile != "" {
		return fmt.Sprintf("{file:%s}", a.ResolvedPromptFile)
	}
	return a.Prompt.Text
}

// findOpencodeAgent looks up a standing OpencodeAgent persona by name.
// registry.Validate already rejects a workflow step's Opencode.Agent
// reference that doesn't name a real OpencodeAgents entry, but this
// renderer stays defensive regardless — a lookup miss is a silent skip
// (see the Opencode-override branch in Render), never a panic.
func findOpencodeAgent(reg *registry.Registry, name string) (registry.OpencodeAgent, bool) {
	for _, oa := range reg.OpencodeAgents {
		if oa.Name == name {
			return oa, true
		}
	}
	return registry.OpencodeAgent{}, false
}

// renderOpencodeAgentPersona builds the opencode agent object for a
// standing registry.OpencodeAgent persona — renderAgent's twin for a
// workflow step that overrides opencode's compilation via Agent.Opencode.
// Every field opencode can express (Description/Class/Prompt/
// Permissions/MCP) comes from oa itself, never the referencing step; only
// mode and steps come from the step's own role/steps, since those are
// workflow-level concepts OpencodeAgent has no field for.
func renderOpencodeAgentPersona(reg *registry.Registry, oa registry.OpencodeAgent, role string, steps *int) (map[string]any, error) {
	bashMap, err := agentBashMap(reg, oa.Permissions.Bash)
	if err != nil {
		return nil, fmt.Errorf("opencode: agent %q: %w", oa.Name, err)
	}

	perm := map[string]any{
		"bash": bashMapToAny(bashpolicy.AsMap(bashMap)),
	}
	if oa.Permissions.Task != "" {
		perm["task"] = oa.Permissions.Task
	}
	if oa.Permissions.Edit != "" {
		perm["edit"] = oa.Permissions.Edit
	}
	if oa.Permissions.Write != "" {
		perm["write"] = oa.Permissions.Write
	}
	if len(oa.Permissions.ExternalDirectory) > 0 {
		perm["external_directory"] = decisionMapToAny(oa.Permissions.ExternalDirectory)
	}

	tools := map[string]any{}
	for _, m := range oa.MCP {
		tools[m.Server+"_*"] = true
		for _, pattern := range m.Ask {
			perm[m.Server+"_"+pattern] = "ask"
		}
	}

	opencodeMode := "subagent"
	if role == "primary" {
		opencodeMode = "primary"
	}
	agentObj := map[string]any{
		"description": oa.Description,
		"mode":        opencodeMode,
		"model":       reg.ModelClasses[oa.Class],
		"prompt":      renderOpencodeAgentPrompt(oa),
		"permission":  perm,
	}
	if len(tools) > 0 {
		agentObj["tools"] = tools
	}
	if steps != nil {
		agentObj["steps"] = *steps
	}
	return agentObj, nil
}

// renderOpencodeAgentPrompt is renderPrompt's twin for a standing
// OpencodeAgent persona: same "{file:...}"-vs-literal logic, reading the
// persona's own ResolvedPromptFile/Prompt.Text instead of an Agent's.
func renderOpencodeAgentPrompt(oa registry.OpencodeAgent) string {
	if oa.ResolvedPromptFile != "" {
		return fmt.Sprintf("{file:%s}", oa.ResolvedPromptFile)
	}
	return oa.Prompt.Text
}

// renderMCPServer resolves one mcp_servers entry into opencode's native mcp
// block shape. A resolver failure (bad env var, missing file, failing
// command) skips the server rather than failing the whole Render; the
// caller records the returned Gap.
func renderMCPServer(s registry.MCPServer) (map[string]any, bool, *render.Gap) {
	entry := map[string]any{}
	switch s.Transport {
	case "remote":
		entry["type"] = "remote"
		url, err := s.URL.Resolve()
		if err != nil {
			return nil, false, resolveFailureGap(s, "url", err)
		}
		entry["url"] = url

		if len(s.Headers) > 0 {
			headers := map[string]any{}
			for name, v := range s.Headers {
				resolved, err := v.Resolve()
				if err != nil {
					return nil, false, resolveFailureGap(s, "headers."+name, err)
				}
				headers[name] = resolved
			}
			entry["headers"] = headers
		}
	case "local":
		entry["type"] = "local"
		cmd := make([]any, 0, len(s.Command))
		for _, part := range s.Command {
			resolved, err := part.Resolve()
			if err != nil {
				return nil, false, resolveFailureGap(s, "command", err)
			}
			cmd = append(cmd, resolved)
		}
		entry["command"] = cmd
	default:
		return nil, false, resolveFailureGap(s, "transport", fmt.Errorf("unknown transport %q", s.Transport))
	}
	return entry, true, nil
}

// resolveFailureGap builds a GapSkip for an MCP server whose URL, command,
// or transport could not be resolved at render time.
func resolveFailureGap(s registry.MCPServer, field string, err error) *render.Gap {
	cap := render.CapMCPLocalTransport
	if s.Transport == "remote" {
		cap = render.CapMCPRemoteTransport
	}
	return &render.Gap{
		Kind:       render.GapSkip,
		Capability: cap,
		Subject:    "mcp:" + s.Name,
		Detail: fmt.Sprintf(
			"mcp server %q %s could not be resolved (%s); it was omitted from this harness's config.",
			s.Name, field, err,
		),
	}
}

// bashMapToAny converts a map[string]bashpolicy.Decision into the
// map[string]any shape that JSON encoding expects.
func bashMapToAny(m map[string]bashpolicy.Decision) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}

// decisionMapToAny converts a path-glob-to-Decision map (e.g.
// Permissions.ExternalDirectory) into the plain map[string]any JSON needs.
func decisionMapToAny(m map[string]registry.Decision) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}
