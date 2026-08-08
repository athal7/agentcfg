package registry

import (
	"fmt"
	"os"
	"path/filepath"
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

// validateAgents reports errors in the registry's agents section.
func validateAgents(reg *Registry) []ValidationError {
	var errs []ValidationError

	// Cache the real (symlink-resolved) root path once, outside the loop.
	rootReal, _ := filepath.EvalSymlinks(reg.RootDir)

	seenNames := map[string]bool{}
	primaryCount := 0

	for _, a := range reg.Agents {
		if a.Name == "" {
			errs = append(errs, ValidationError{Message: "agent has no name"})
		} else if seenNames[a.Name] {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("duplicate agent name %q", a.Name)})
		} else {
			seenNames[a.Name] = true
		}

		mode := a.Mode
		if mode == "" {
			mode = "subagent"
		}
		switch mode {
		case "primary":
			primaryCount++
		case "subagent":
		default:
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q has invalid mode %q (must be primary or subagent)", a.Name, a.Mode),
			})
		}

		if a.Class == "" {
			errs = append(errs, ValidationError{Message: fmt.Sprintf("agent %q has no class", a.Name)})
		} else if _, ok := reg.ModelClasses[a.Class]; !ok {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q references unknown model class %q", a.Name, a.Class),
			})
		}

		switch {
		case a.Prompt.File == "" && a.Prompt.Text == "":
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q must set exactly one of prompt.file or prompt.text", a.Name),
			})
		case a.Prompt.File != "" && a.Prompt.Text != "":
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("agent %q must set exactly one of prompt.file or prompt.text, not both", a.Name),
			})
		case a.Prompt.File != "":
			violates, resolved := promptFileTraversal(reg.RootDir, rootReal, a.Prompt.File)
			if violates {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("prompt file escapes registry root: %s", a.Prompt.File),
				})
			} else if _, err := os.Stat(resolved); err != nil {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("referenced prompt file does not exist: %s", resolved),
				})
			}
		}

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
	}

	if primaryCount > 1 {
		errs = append(errs, ValidationError{
			Message: fmt.Sprintf("exactly 0 or 1 agent may have mode: primary, found %d", primaryCount),
		})
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

// validateCommands reports errors in the registry's commands section.
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

		switch {
		case c.Prompt.File == "" && c.Prompt.Text == "":
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("command %q must set exactly one of prompt.file or prompt.text", c.Name),
			})
		case c.Prompt.File != "" && c.Prompt.Text != "":
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("command %q must set exactly one of prompt.file or prompt.text, not both", c.Name),
			})
		case c.Prompt.File != "":
			violates, resolved := promptFileTraversal(reg.RootDir, rootReal, c.Prompt.File)
			if violates {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("prompt file escapes registry root: %s", c.Prompt.File),
				})
			} else if _, err := os.Stat(resolved); err != nil {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("referenced prompt file does not exist: %s", resolved),
				})
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
