package apply

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"

	"github.com/athal7/agentcfg/internal/render"
)

// applyMergeJSON merges m.Object into the JSON file at m.Path, touching
// only the dotted paths listed in m.Managed.
func applyMergeJSON(m render.MergeJSON) (applied, skipped string, err error) {
	return applyMerge(m.Path, m.Mode, m.Managed, m.Object, json.Unmarshal, marshalIndentJSON)
}

// applyMergeYAML merges m.Object into the YAML file at m.Path, touching
// only the dotted paths listed in m.Managed.
func applyMergeYAML(m render.MergeYAML) (applied, skipped string, err error) {
	return applyMerge(m.Path, m.Mode, m.Managed, m.Object, yaml.Unmarshal, yaml.Marshal)
}

// applyMergeTOML merges m.Object into the TOML file at m.Path, touching
// only the dotted paths listed in m.Managed.
func applyMergeTOML(m render.MergeTOML) (applied, skipped string, err error) {
	return applyMerge(m.Path, m.Mode, m.Managed, m.Object, toml.Unmarshal, toml.Marshal)
}

// marshalIndentJSON encodes v as indented JSON (2-space indent).
func marshalIndentJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// applyMerge is the shared managed-key-allowlist merge behind
// MergeJSON/MergeYAML/MergeTOML: decode the existing file (if any) into a
// generic tree, splice in only the dotted paths listed in managed from
// object, then re-encode. Every key outside managed is left byte-for-byte
// equivalent to what was decoded (format-specific styling aside —
// preserving comments/formatting is the registry loader's concern, not
// this merge target's).
func applyMerge(
	path string,
	mode fs.FileMode,
	managed []string,
	object map[string]any,
	unmarshal func([]byte, any) error,
	marshal func(any) ([]byte, error),
) (applied, skipped string, err error) {
	expanded, err := expandPath(path)
	if err != nil {
		return "", "", err
	}

	tracked, err := isGitTracked(expanded)
	if err != nil {
		return "", "", err
	}
	if tracked {
		return "", fmt.Sprintf("skipped: %s is git-tracked", expanded), nil
	}

	existing, err := loadTree(expanded, unmarshal)
	if err != nil {
		return "", "", err
	}

	applyManagedPaths(existing, object, managed)

	encoded, err := marshal(existing)
	if err != nil {
		return "", "", fmt.Errorf("encoding %s: %w", expanded, err)
	}

	if err := writeFileContent(expanded, encoded, mode); err != nil {
		return "", "", err
	}

	return fmt.Sprintf("merged into %s", expanded), "", nil
}

// loadTree reads and decodes path into a generic map tree. A missing file
// decodes to an empty tree — the first merge into a not-yet-existing file
// behaves as if it started from an empty object.
func loadTree(path string, unmarshal func([]byte, any) error) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}

	tree := map[string]any{}
	if err := unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return tree, nil
}

// applyManagedPaths splices every dotted path in managed from src into dst,
// in place.
func applyManagedPaths(dst, src map[string]any, managed []string) {
	for _, path := range managed {
		if path == "" {
			continue
		}
		applyManagedPath(dst, src, strings.Split(path, "."))
	}
}

// applyManagedPath walks one dotted path's segments in parallel through
// dst and src.
//
//   - A literal segment (e.g. "permission"): if src has that key, splice
//     it in — at the end of the path this is a full subtree replace
//     (dst[seg] = src[seg] wholesale); mid-path, it descends into both
//     trees at that key and recurses.
//   - A "*" segment (e.g. the middle of "agent.*.model"): for every key
//     present in src's map at this position, splice that key's subtree
//     into dst at the same key. Keys present in dst but absent from src at
//     this position are never touched — this is what lets an
//     un-rendered/unmanaged sibling (an agent this renderer didn't
//     produce, e.g. one the user hand-authored) survive untouched.
//
// A key present in managed's path but absent from src at that position is
// a no-op: nothing is deleted, nothing is set.
func applyManagedPath(dst, src map[string]any, segments []string) {
	if len(segments) == 0 {
		return
	}
	seg := segments[0]
	rest := segments[1:]

	if seg == "*" {
		for key, srcVal := range src {
			applyManagedKey(dst, key, srcVal, rest)
		}
		return
	}

	srcVal, ok := src[seg]
	if !ok {
		return
	}
	applyManagedKey(dst, seg, srcVal, rest)
}

// applyManagedKey sets dst[key] from srcVal: a full replace when rest is
// empty (path bottoms out here), or a recursive descent into both trees
// at key otherwise.
func applyManagedKey(dst map[string]any, key string, srcVal any, rest []string) {
	if len(rest) == 0 {
		dst[key] = srcVal
		return
	}
	dstChild := childMap(dst, key)
	srcChild, _ := srcVal.(map[string]any)
	applyManagedPath(dstChild, srcChild, rest)
	dst[key] = dstChild
}

// childMap returns m[key] as a map[string]any, or a fresh empty map if
// m[key] doesn't exist or isn't a map — this is what lets a managed path
// create intermediate structure in a file that didn't have it yet.
func childMap(m map[string]any, key string) map[string]any {
	if existing, ok := m[key].(map[string]any); ok {
		return existing
	}
	return map[string]any{}
}
