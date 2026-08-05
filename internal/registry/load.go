package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Load reads and merges a registry directory (agentcfg.yaml, its imports,
// and an implicit local.yaml override) into a Registry, then validates it.
//
// The returned error is non-nil only for I/O or YAML-parse failures — a
// missing agentcfg.yaml, an unreadable import, malformed YAML. Schema and
// consistency problems (duplicate agent names, unknown class references,
// etc.) are returned as validationErrors/validationWarnings instead, so a
// caller can report every problem in one pass rather than stopping at the
// first one.
func Load(registryDir string) (*Registry, []ValidationError, []ValidationWarning, error) {
	dir, err := expandHome(registryDir)
	if err != nil {
		return nil, nil, nil, err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return nil, nil, nil, err
	}

	entryPath := filepath.Join(dir, "agentcfg.yaml")
	entry, err := parseFile(entryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, fmt.Errorf("no agentcfg.yaml found at %s — run `agentcfg init`", dir)
		}
		return nil, nil, nil, err
	}

	reg := &Registry{RootDir: dir}
	st := newMergeState()
	var verrs []ValidationError
	var vWarns []ValidationWarning

	verrs = append(verrs, mergeFileInto(reg, entry, "agentcfg.yaml", false, st)...)

	for _, pattern := range entry.Imports {
		paths, err := expandImport(dir, pattern)
		if err != nil {
			return nil, nil, nil, err
		}
		// Only warn when the glob's parent directory exists but nothing
		// inside it matched — that's the "typo in the extension/pattern"
		// case. A parent directory that doesn't exist at all (e.g. an
		// optional bash.d/*.yaml split a registry never opted into) is a
		// normal, silent no-op, not a mistake worth flagging.
		if len(paths) == 0 && strings.Contains(pattern, "*") {
			if parentInfo, statErr := os.Stat(filepath.Dir(filepath.Join(dir, pattern))); statErr == nil && parentInfo.IsDir() {
				vWarns = append(vWarns, ValidationWarning{
					Message: fmt.Sprintf("import glob %q matched no files", pattern),
				})
			}
		}
		for _, p := range paths {
			fc, err := parseFile(p)
			if err != nil {
				return nil, nil, nil, err
			}
			verrs = append(verrs, mergeFileInto(reg, fc, relPath(dir, p), false, st)...)
		}
	}

	localPath := filepath.Join(dir, "local.yaml")
	if _, statErr := os.Stat(localPath); statErr == nil {
		fc, err := parseFile(localPath)
		if err != nil {
			return nil, nil, nil, err
		}
		verrs = append(verrs, mergeFileInto(reg, fc, "local.yaml", true, st)...)
	} else if !os.IsNotExist(statErr) {
		return nil, nil, nil, statErr
	}

	normalizeAgentModes(reg)
	resolvePromptPaths(reg)

	vErrs, vWarns2 := Validate(reg)
	verrs = append(verrs, vErrs...)
	vWarns = append(vWarns, vWarns2...)

	return reg, verrs, vWarns, nil
}

func parseFile(path string) (fileContents, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileContents{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var fc fileContents
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return fileContents{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return fc, nil
}

// expandImport resolves one imports: entry against the registry root. Glob
// entries (containing "*") are expanded and sorted alphabetically so
// bash.d/*.yaml-style imports merge in a deterministic order.
func expandImport(dir, pattern string) ([]string, error) {
	full := filepath.Join(dir, pattern)
	if !strings.Contains(pattern, "*") {
		return []string{full}, nil
	}
	matches, err := filepath.Glob(full)
	if err != nil {
		return nil, fmt.Errorf("expanding import glob %s: %w", pattern, err)
	}
	sort.Strings(matches)
	return matches, nil
}

// relPath returns p relative to dir for error messages, falling back to p
// itself if it can't be made relative.
func relPath(dir, p string) string {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return p
	}
	return rel
}
