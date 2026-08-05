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
	errs = append(errs, validateValues(reg)...)

	return errs, warns
}

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

func validateAgents(reg *Registry) []ValidationError {
	var errs []ValidationError

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
			resolved := filepath.Join(reg.RootDir, a.Prompt.File)
			rel, err := filepath.Rel(reg.RootDir, resolved)
			if err != nil || filepath.IsAbs(rel) || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
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
