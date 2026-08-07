package opencode

import (
	"sort"

	"github.com/athal7/agentcfg/internal/vocab"
)

// permissionKey maps a canonical tool/permission name to opencode's native
// permission.<key> field name, for canonicals that render as a static
// harness-wide default (no registry field backs a global per-canonical
// value, so these are always "allow"). vocab.Bash is handled separately
// (its value comes from bashpolicy.Compile, not a static default);
// vocab.ExternalDirectory is not included here because it is a map type
// (map[string]Decision) rather than a simple string permission — it is
// emitted separately in agentPermissionMap via the external_directory key.
var permissionKey = map[vocab.Canonical]string{
	vocab.Read:     "read",
	vocab.Edit:     "edit",
	vocab.Write:    "write",
	vocab.Task:     "task",
	vocab.Skill:    "skill",
	vocab.WebFetch: "webfetch",
}

// unsupportedCanonicals is every canonical name opencode has no permission
// surface for at all (it either always allows the underlying tool, or has
// no equivalent tool). Render must not emit a permission key for these.
var unsupportedCanonicals = map[vocab.Canonical]bool{
	vocab.Grep:         true,
	vocab.Glob:         true,
	vocab.LSP:          true,
	vocab.WebSearch:    true,
	vocab.Todo:         true,
	vocab.ASTSearch:    true,
	vocab.ImageInspect: true,
}

// managedPermissionPaths returns the leaf-level "permission.<key>" paths
// Render actually owns: permission.bash (the compiled bash policy) plus
// one leaf per permissionKey entry (read/edit/write/task/skill/webfetch).
// Render must manage these leaves individually rather than the bare
// "permission" path — per merge.go's semantics, a managed path ending
// mid-object is a full subtree replace, which would silently delete any
// permission.* key Render doesn't itself emit (e.g. a hand-set
// permission.external_directory) on every apply. Sorted for a
// deterministic, reproducible Plan.
func managedPermissionPaths() []string {
	paths := make([]string, 0, len(permissionKey)+1)
	paths = append(paths, "permission.bash")
	for _, key := range permissionKey {
		paths = append(paths, "permission."+key)
	}
	sort.Strings(paths)
	return paths
}
