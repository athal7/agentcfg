package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Validate checks a merged Registry for schema and consistency problems.
// Load calls this internally; it's exported so a `validate` CLI command can
// re-run it against an already-loaded registry.
func Validate(reg *Registry) ([]ValidationError, []ValidationWarning) {
	var errs []ValidationError
	var warns []ValidationWarning

	errs = append(errs, validateModelClasses(reg)...)
	errs = append(errs, validateAgents(reg)...)
	warns = append(warns, validateAgentWarnings(reg)...)
	errs = append(errs, validateOpencodeAgents(reg)...)
	errs = append(errs, validateBash(reg)...)
	errs = append(errs, validateMCPServers(reg)...)
	errs = append(errs, validateContexts(reg)...)
	errs = append(errs, validateCommands(reg)...)
	errs = append(errs, validateValues(reg)...)

	return errs, warns
}

// validateModelClasses reports errors for missing reserved model classes.
func validateModelClasses(reg *Registry) []ValidationError {
	var errs []ValidationError
	if len(reg.ModelClasses) == 0 {
		return errs
	}
	for _, reserved := range []string{"default", "smol"} {
		if _, ok := reg.ModelClasses[reserved]; !ok {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("reserved model class %q is missing from model_classes", reserved),
			})
		}
	}
	return errs
}

// isValidDecision reports whether d is one of the three recognized bash
// policy outcomes. Shared by every validation pass that checks a
// Decision-typed field or map value (bash lists/profiles, per-agent
// external_directory).
func isValidDecision(d Decision) bool {
	return d == Allow || d == Deny || d == Ask
}

// targetsOmp reports whether an empty/omitted targets list ("every
// harness") or an explicit list naming "omp" applies to omp's renderer.
// Mirrors internal/render/omp's own unexported targets() helper — kept
// duplicated rather than shared to avoid registry importing render.
func targetsOmp(list []string) bool {
	return len(list) == 0 || slices.Contains(list, "omp")
}

// stepsEqual reports whether two *int step-budget values are equal:
// both nil, or both non-nil with the same dereferenced value. Used to
// compare an Agent.Steps budget across multiple steps sharing one
// opencode_agents persona.
func stepsEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// validateAgents reports errors in the registry's workflow steps.
func validateAgents(reg *Registry) []ValidationError {
	var errs []ValidationError

	// Cache the real (symlink-resolved) root path once, outside the loop.
	rootReal, _ := filepath.EvalSymlinks(reg.RootDir)

	seenNames := map[string]bool{}
	primaryCount := 0
	var advisorySteps []string

	opencodeAgentNames := map[string]bool{}
	for _, oa := range reg.OpencodeAgents {
		if oa.Name != "" {
			opencodeAgentNames[oa.Name] = true
		}
	}

	// referencedOpencodeAgents tracks which opencode_agents personas are
	// actually pointed at by at least one step — needed below to reject
	// a plain (non-overridden) step whose own Name would collide with a
	// persona's rendered opencode.json agent.<name> key. An unreferenced
	// persona sharing a name with a plain step is fine: it never
	// renders, so there's no key for the plain step to collide with.
	// firstOpencodeRef records the first (in Agents declaration order —
	// the same order the opencode renderer resolves mode/steps from) step
	// to reference each persona, so every later reference can be checked
	// against it for the compatible-references check below.
	referencedOpencodeAgents := map[string]bool{}
	firstOpencodeRef := map[string]Agent{}
	for _, a := range reg.Agents {
		if a.Opencode == nil || a.Opencode.Agent == "" {
			continue
		}
		referencedOpencodeAgents[a.Opencode.Agent] = true
		if _, ok := firstOpencodeRef[a.Opencode.Agent]; !ok {
			firstOpencodeRef[a.Opencode.Agent] = a
		}
	}

	for _, a := range reg.Agents {
		if a.Name == "" {
			errs = append(errs, ValidationError{Message: "agent has no name"})
		} else if seenNames[a.Name] {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("duplicate agent name %q", a.Name)})
		} else {
			seenNames[a.Name] = true
		}

		if a.Opencode == nil && referencedOpencodeAgents[a.Name] {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q renders as opencode agent key %q, which collides with a referenced opencode_agents persona of the same name — rename one of them", a.Name, a.Name),
			})
		}

		switch a.Role {
		case "primary":
			primaryCount++
		case "advisory":
			advisorySteps = append(advisorySteps, a.Name)
			if a.Permissions.Edit != string(Deny) || a.Permissions.Write != string(Deny) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("agent %q has role: advisory and must set permissions.edit and permissions.write to deny (advisory steps must not write)", a.Name),
				})
			}
		case "delegate":
			// Reserved-name collision: "plan" collides with omp's native
			// model-role name and interactive plan-mode toggle. Only a
			// role: delegate step is ever dispatched by name as a
			// standalone omp agent file — a primary or advisory step
			// never reaches that dispatch path (see agentcfg#14). Scoped
			// to steps that actually target omp (empty Targets means
			// every harness) AND aren't opencode-agent-overridden: a
			// step with targets: [opencode], or one naming an Opencode
			// persona (which renders nothing for omp regardless of
			// Targets/Role — see Agent.Opencode), never reaches omp's
			// dispatch path either, so the name is safe there
			// regardless of role.
			if a.Name == "plan" && targetsOmp(a.Targets) && a.Opencode == nil {
				errs = append(errs, ValidationError{
					Message: `agent name "plan" collides with omp's native plan-mode machinery and will hang when dispatched — see agentcfg#14`,
				})
			}
		default:
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q has invalid role %q (must be primary, advisory, or delegate)", a.Name, a.Role),
			})
		}

		if a.Class == "" {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("agent %q has no class", a.Name)})
		} else if _, ok := reg.ModelClasses[a.Class]; !ok {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q references unknown model class %q", a.Name, a.Class),
			})
		}

		errs = append(errs, validatePromptField(a.Prompt, fmt.Sprintf("agent %q", a.Name), reg.RootDir, rootReal)...)

		if !a.Permissions.Bash.IsZero() {
			if a.Permissions.Bash.Profile != "" {
				if _, ok := reg.Bash.Profiles[a.Permissions.Bash.Profile]; !ok {
					errs = append(errs, ValidationError{
						Message: fmt.Sprintf("agent %q references unknown bash profile %q", a.Name, a.Permissions.Bash.Profile),
					})
				}
			} else if a.Permissions.Bash.Decision != string(Allow) && a.Permissions.Bash.Decision != string(Deny) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("agent %q has invalid permissions.bash %q (must be allow or deny)", a.Name, a.Permissions.Bash.Decision),
				})
			}
		}

		for pattern, decision := range a.Permissions.ExternalDirectory {
			if !isValidDecision(decision) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("agent %q permissions.external_directory pattern %q has invalid decision %q (must be allow, deny, or ask)", a.Name, pattern, decision),
				})
			}
		}

		if a.Opencode != nil {
			if a.Opencode.Agent == "" {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("agent %q sets opencode: but its agent field is empty", a.Name),
				})
			} else if !opencodeAgentNames[a.Opencode.Agent] {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("agent %q references unknown opencode agent %q (not declared under opencode_agents)", a.Name, a.Opencode.Agent),
				})
			} else if first := firstOpencodeRef[a.Opencode.Agent]; (a.Role == "primary") != (first.Role == "primary") || !stepsEqual(a.Steps, first.Steps) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("agent %q references opencode agent %q with a different effective mode or steps budget than agent %q — every step sharing one opencode_agents persona must agree on role: primary vs. non-primary and on steps:", a.Name, a.Opencode.Agent, first.Name),
				})
			}
		}
	}

	if primaryCount > 1 {
		errs = append(errs, ValidationError{
			Message: fmt.Sprintf("exactly 0 or 1 agent may have role: primary, found %d", primaryCount),
		})
	}

	if primaryCount == 0 {
		for _, name := range advisorySteps {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q has role: advisory but the registry has no role: primary agent to compose into", name),
			})
		}
	}

	return errs
}

// validateAgentWarnings reports non-fatal warnings about the registry's agents.
func validateAgentWarnings(reg *Registry) []ValidationWarning {
	var warns []ValidationWarning
	for _, a := range reg.Agents {
		if len(a.MCP) == 0 {
			continue
		}
		if a.Permissions.Bash.Decision == string(Deny) {
			continue
		}
		warns = append(warns, ValidationWarning{
			Message: fmt.Sprintf("agent %q declares mcp servers but does not deny bash; consider denying bash on MCP-proxy-style agents to avoid a shell-out bypass.", a.Name),
		})
	}
	return warns
}

// validateOpencodeAgents reports errors in the registry's opencode_agents
// section — standing opencode personas referenced by name from workflow
// steps via Agent.Opencode (see that field's doc comment). Validated the
// same way as a workflow step's own name/class/prompt/bash/
// external_directory fields, minus Role (opencode_agents has no role —
// it's not a workflow step, just a rendering target one or more steps
// point at).
func validateOpencodeAgents(reg *Registry) []ValidationError {
	var errs []ValidationError

	rootReal, _ := filepath.EvalSymlinks(reg.RootDir)
	seenNames := map[string]bool{}

	for _, oa := range reg.OpencodeAgents {
		if oa.Name == "" {
			errs = append(errs, ValidationError{Message: "opencode agent has no name"})
		} else if seenNames[oa.Name] {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("duplicate opencode agent name %q", oa.Name)})
		} else {
			seenNames[oa.Name] = true
		}

		if oa.Class == "" {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("opencode agent %q has no class", oa.Name)})
		} else if _, ok := reg.ModelClasses[oa.Class]; !ok {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("opencode agent %q references unknown model class %q", oa.Name, oa.Class),
			})
		}

		errs = append(errs, validatePromptField(oa.Prompt, fmt.Sprintf("opencode agent %q", oa.Name), reg.RootDir, rootReal)...)

		if !oa.Permissions.Bash.IsZero() {
			if oa.Permissions.Bash.Profile != "" {
				if _, ok := reg.Bash.Profiles[oa.Permissions.Bash.Profile]; !ok {
					errs = append(errs, ValidationError{
						Message: fmt.Sprintf("opencode agent %q references unknown bash profile %q", oa.Name, oa.Permissions.Bash.Profile),
					})
				}
			} else if oa.Permissions.Bash.Decision != string(Allow) && oa.Permissions.Bash.Decision != string(Deny) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("opencode agent %q has invalid permissions.bash %q (must be allow or deny)", oa.Name, oa.Permissions.Bash.Decision),
				})
			}
		}

		for pattern, decision := range oa.Permissions.ExternalDirectory {
			if !isValidDecision(decision) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("opencode agent %q permissions.external_directory pattern %q has invalid decision %q (must be allow, deny, or ask)", oa.Name, pattern, decision),
				})
			}
		}
	}

	return errs
}

// validateBash reports errors in the registry's bash policy lists and profiles.
func validateBash(reg *Registry) []ValidationError {
	var errs []ValidationError

	for listName, rules := range reg.Bash.Lists {
		for pattern, decision := range rules {
			if !isValidDecision(decision) {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("bash list %q pattern %q has invalid decision %q (must be allow, deny, or ask)", listName, pattern, decision),
				})
			}
		}
	}

	for profileName, prof := range reg.Bash.Profiles {
		if !isValidDecision(prof.Base) {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("bash profile %q has invalid base decision %q (must be allow, deny, or ask)", profileName, prof.Base),
			})
		}
	}

	return errs
}

// validateMCPServers reports errors in the registry's MCP servers section.
func validateMCPServers(reg *Registry) []ValidationError {
	var errs []ValidationError

	seenNames := map[string]bool{}
	for _, s := range reg.MCPServers {
		if s.Name == "" {
			errs = append(errs, ValidationError{Message: "mcp server has no name"})
		} else if seenNames[s.Name] {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("duplicate mcp server name %q", s.Name)})
		} else {
			seenNames[s.Name] = true
		}

		switch s.Transport {
		case "remote":
			if s.URL.IsZero() {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("mcp server %q has transport: remote but no url", s.Name),
				})
			}
		case "local":
			if len(s.Command) == 0 {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("mcp server %q has transport: local but no command", s.Name),
				})
			}
		default:
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("mcp server %q has invalid transport %q (must be remote or local)", s.Name, s.Transport),
			})
		}
	}

	return errs
}

// validateContexts reports errors in the registry's contexts section.
func validateContexts(reg *Registry) []ValidationError {
	var errs []ValidationError
	for i, c := range reg.Contexts {
		if c.Match.GitRemoteHost == "" && c.Match.GitRemoteOwner == "" {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("context entry %d must set at least one of match.git_remote_host or match.git_remote_owner", i),
			})
		}
	}
	return errs
}

// isValidCommandName reports whether name satisfies the Agent Skills spec's
// naming rule: lowercase letters, digits, and hyphens only, no leading or
// trailing hyphen, at most 64 characters. Violating this makes the
// rendered skill silently fail to load in opencode, so it's caught here
// instead of at either harness's own discovery step.
func isValidCommandName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, r := range name {
		if r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// validatePromptField reports errors in a single Prompt field: exactly
// one of file/text must be set, and a file-backed prompt must resolve
// inside the registry root and exist on disk. subject is a
// pre-formatted description of the owning entity (e.g. `agent "lead"`,
// `command "review" step "plan"`) used verbatim in error messages.
// Shared by Agent, Command, and CommandStep — every prompt-bearing
// registry entity validates its prompt identically.
func validatePromptField(p Prompt, subject, rootDir, rootReal string) []ValidationError {
	switch {
	case p.File == "" && p.Text == "":
		return []ValidationError{{Message: fmt.Sprintf("%s must set exactly one of prompt.file or prompt.text", subject)}}
	case p.File != "" && p.Text != "":
		return []ValidationError{{Message: fmt.Sprintf("%s must set exactly one of prompt.file or prompt.text, not both", subject)}}
	case p.File != "":
		violates, resolved := promptFileTraversal(rootDir, rootReal, p.File)
		if violates {
			return []ValidationError{{Message: fmt.Sprintf("prompt file escapes registry root: %s", p.File)}}
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return []ValidationError{{Message: fmt.Sprintf("referenced prompt file does not exist: %s", resolved)}}
		}
		if !info.Mode().IsRegular() {
			return []ValidationError{{Message: fmt.Sprintf("referenced prompt path is not a regular file: %s", resolved)}}
		}
	}
	return nil
}

// validateCommands reports errors in the registry's commands section. A
// command is exactly one of two shapes: a flat prompt, or an ordered
// list of named steps (a structured multi-step workflow command — see
// Command's doc comment).
func validateCommands(reg *Registry) []ValidationError {
	var errs []ValidationError

	rootReal, _ := filepath.EvalSymlinks(reg.RootDir)
	seenNames := map[string]bool{}

	for _, c := range reg.Commands {
		if c.Name == "" {
			errs = append(errs, ValidationError{Message: "command has no name"})
		} else if seenNames[c.Name] {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("duplicate command name %q", c.Name)})
		} else {
			seenNames[c.Name] = true
		}

		if c.Name != "" && !isValidCommandName(c.Name) {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("command %q has invalid name (must be lowercase letters, digits, and hyphens, no leading or trailing hyphen, at most 64 characters)", c.Name),
			})
		}

		if c.Description == "" {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("command %q has no description", c.Name)})
		}

		hasPrompt := c.Prompt.File != "" || c.Prompt.Text != ""
		hasSteps := len(c.Steps) > 0
		switch {
		case hasPrompt && hasSteps:
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("command %q must set exactly one of prompt or steps, not both", c.Name),
			})
		case !hasPrompt && !hasSteps:
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("command %q must set exactly one of prompt or steps", c.Name),
			})
		case hasPrompt:
			errs = append(errs, validatePromptField(c.Prompt, fmt.Sprintf("command %q", c.Name), reg.RootDir, rootReal)...)
		case hasSteps:
			seenStepNames := map[string]bool{}
			for _, s := range c.Steps {
				if s.Name == "" {
					errs = append(errs, ValidationError{Message: fmt.Sprintf("command %q has a step with no name", c.Name)})
				} else if seenStepNames[s.Name] {
					errs = append(errs, ValidationError{Message: fmt.Sprintf("command %q has duplicate step name %q", c.Name, s.Name)})
				} else {
					seenStepNames[s.Name] = true
				}
				errs = append(errs, validatePromptField(s.Prompt, fmt.Sprintf("command %q step %q", c.Name, s.Name), reg.RootDir, rootReal)...)
			}
		}
	}

	return errs
}

// validateValues reports errors in value fields across the registry.
func validateValues(reg *Registry) []ValidationError {
	var errs []ValidationError

	for _, s := range reg.MCPServers {
		if !s.URL.IsZero() {
			errs = append(errs, validateValue(s.URL, fmt.Sprintf("mcp server %q url", s.Name))...)
		}
		for i, v := range s.Command {
			errs = append(errs, validateValue(v, fmt.Sprintf("mcp server %q command[%d]", s.Name, i))...)
		}
		for k, v := range s.Headers {
			errs = append(errs, validateValue(v, fmt.Sprintf("mcp server %q header %q", s.Name, k))...)
		}
	}

	return errs
}

// validateValue reports errors in a single Value's from/field configuration.
func validateValue(v Value, context string) []ValidationError {
	var errs []ValidationError
	if v.IsZero() {
		return errs
	}
	switch v.From {
	case "":
		// literal value — always valid
	case "env":
		if v.Name == "" {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("%s: from: env requires a name", context),
			})
		}
	case "file":
		if v.Path == "" {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("%s: from: file requires a path", context),
			})
		}
	case "command":
		if len(v.Run) == 0 {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("%s: from: command requires a run list", context),
			})
		}
	default:
		errs = append(errs, ValidationError{
			Message: fmt.Sprintf("%s: unknown value source %q", context, v.From),
		})
	}
	return errs
}
