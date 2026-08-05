package registry

import "fmt"

// mergeState tracks, per top-level key, which file first declared it — so a
// second declaration (outside local.yaml) can be reported as a conflict
// naming both files.
type mergeState struct {
	sourceOf        map[string]string
	bashListSource  map[string]string
	bashDefaultsSrc string
	bashProfileSrc  map[string]string
}

func newMergeState() *mergeState {
	return &mergeState{
		sourceOf:       map[string]string{},
		bashListSource: map[string]string{},
		bashProfileSrc: map[string]string{},
	}
}

// mergeFileInto folds fc (parsed from the file at path) into reg. Non-local
// files error when a top-level key was already declared by an earlier file
// (except bash, which merges at the list level — see mergeBash). local.yaml
// instead overwrites whole top-level keys unconditionally.
func mergeFileInto(reg *Registry, fc fileContents, path string, isLocal bool, st *mergeState) []ValidationError {
	var errs []ValidationError

	mergeKey := func(key string, present bool, apply func()) {
		if !present {
			return
		}
		if !isLocal {
			if prev, ok := st.sourceOf[key]; ok {
				errs = append(errs, ValidationError{
					Message: fmt.Sprintf("%s declared in both %s and %s", key, prev, path),
				})
				return
			}
			st.sourceOf[key] = path
		}
		apply()
	}

	if fc.Version != nil {
		reg.Version = *fc.Version
	}
	mergeKey("harnesses", fc.Harnesses != nil, func() { reg.Harnesses = fc.Harnesses })
	mergeKey("model_classes", fc.ModelClasses != nil, func() { reg.ModelClasses = fc.ModelClasses })
	mergeKey("agents", fc.Agents != nil, func() { reg.Agents = fc.Agents })
	mergeKey("mcp_servers", fc.MCPServers != nil, func() { reg.MCPServers = fc.MCPServers })
	mergeKey("contexts", fc.Contexts != nil, func() { reg.Contexts = fc.Contexts })

	if fc.Bash != nil {
		if isLocal {
			reg.Bash = *fc.Bash
		} else {
			errs = append(errs, mergeBash(reg, *fc.Bash, path, st)...)
		}
	}

	return errs
}

// mergeBash merges bash.yaml and bash.d/*.yaml contributions. Lists merge by
// name (collision across files is an error naming both files); default_lists
// and profiles are expected to come from a single file each, but the same
// collision rule applies defensively if a second file also sets them.
func mergeBash(reg *Registry, src BashPolicy, path string, st *mergeState) []ValidationError {
	var errs []ValidationError

	if src.DefaultLists != nil {
		if st.bashDefaultsSrc != "" {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("bash.default_lists declared in both %s and %s", st.bashDefaultsSrc, path),
			})
		} else {
			reg.Bash.DefaultLists = src.DefaultLists
			st.bashDefaultsSrc = path
		}
	}

	for name, rules := range src.Lists {
		if prev, ok := st.bashListSource[name]; ok {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("bash list %q declared in both %s and %s", name, prev, path),
			})
			continue
		}
		if reg.Bash.Lists == nil {
			reg.Bash.Lists = map[string]map[string]Decision{}
		}
		reg.Bash.Lists[name] = rules
		st.bashListSource[name] = path
	}

	for name, prof := range src.Profiles {
		if prev, ok := st.bashProfileSrc[name]; ok {
			errs = append(errs, ValidationError{
				Message: fmt.Sprintf("bash profile %q declared in both %s and %s", name, prev, path),
			})
			continue
		}
		if reg.Bash.Profiles == nil {
			reg.Bash.Profiles = map[string]BashProfile{}
		}
		reg.Bash.Profiles[name] = prof
		st.bashProfileSrc[name] = path
	}

	return errs
}
