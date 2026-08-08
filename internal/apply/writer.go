// Package apply executes the render.Output values a Renderer produces:
// writing files, merging structured config, rebuilding directories, and
// running commands. Nothing in internal/render performs I/O — render.Plan
// is the dry-run/preview mechanism, produced with zero filesystem or
// process side effects. apply is where a Plan actually takes effect.
package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/athal7/agentcfg/internal/render"
)

// Options carries apply-time configuration. It's intentionally minimal:
// Apply always performs real I/O when called. render.Plan is already the
// preview mechanism (a Plan is built with no I/O at all), so Apply itself
// has no dry-run mode — once the CLI layer (phase 4) decides to call
// Apply, it commits. Add fields here only when a real caller needs them.
type Options struct{}

// Result is the outcome of applying a Plan: human-readable descriptions of
// what was actually written or run, and what was skipped (and why — e.g. a
// target file is git-tracked and apply refuses to clobber it). A skip is a
// normal, expected outcome, not a failure.
type Result struct {
	Applied []string
	Skipped []string
}

// Apply executes every Output in plan, in declared order.
//
// Behavioral decision: a hard error on one Output does not stop Apply from
// attempting the rest. A single bad mcp server config or one git-tracked
// file shouldn't prevent every other output from being written. Apply
// always attempts every Output, then joins every hard error encountered
// (via errors.Join) into the single error it returns. Callers that need
// per-output detail should also inspect Result — Applied/Skipped record
// what actually happened for outputs that didn't error.
func Apply(plan *render.Plan, _ Options) (Result, error) {
	var result Result
	var errs []error

	for _, out := range plan.Outputs {
		applied, skipped, err := applyOne(out)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", out.Describe(), err))
		case skipped != "":
			result.Skipped = append(result.Skipped, skipped)
		default:
			result.Applied = append(result.Applied, applied)
		}
	}

	return result, errors.Join(errs...)
}

// applyOne dispatches one render.Output to the correct apply function
// based on its concrete type.
func applyOne(out render.Output) (applied, skipped string, err error) {
	switch o := out.(type) {
	case render.WriteFile:
		return applyWriteFile(o)
	case render.MergeJSON:
		return applyMergeJSON(o)
	case render.MergeYAML:
		return applyMergeYAML(o)
	case render.MergeTOML:
		return applyMergeTOML(o)
	case render.RebuildDir:
		return applyRebuildDir(o)
	case render.RebuildTree:
		return applyRebuildTree(o)
	case render.RunCommand:
		return applyRunCommand(o)
	default:
		return "", "", fmt.Errorf("apply: unknown output type %T", out)
	}
}

// expandPath expands a leading "~" or "~/" to the current user's home
// directory via os.UserHomeDir(). Every Output.Path is expanded through
// this single helper before touching the filesystem — nothing else in
// this package should call os.UserHomeDir() or interpret "~" itself.
func expandPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expanding %s: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// applyWriteFile expands w.Path, skips the write if the target is
// git-tracked (see gitguard.go), and otherwise writes w.Content verbatim.
func applyWriteFile(w render.WriteFile) (applied, skipped string, err error) {
	path, err := expandPath(w.Path)
	if err != nil {
		return "", "", err
	}

	tracked, err := isGitTracked(path)
	if err != nil {
		return "", "", err
	}
	if tracked {
		return "", fmt.Sprintf("skipped: %s is git-tracked", path), nil
	}

	if err := writeFileContent(path, w.Content, w.Mode); err != nil {
		return "", "", err
	}
	return fmt.Sprintf("wrote %s", path), "", nil
}

// resolveWriteTarget follows path if it is a symlink, returning the real
// file it points to. writeFileContent writes through this resolved path
// instead of path itself so that os.Rename never replaces an externally
// managed symlink (e.g. one chezmoi points at a dotfiles repo) — it
// updates what the symlink points to and leaves the symlink in place.
//
// Relative symlink targets are resolved against the symlink's own
// directory; chains of symlinks are followed to their end. A target that
// doesn't exist yet (a dangling symlink) is returned as-is rather than
// treated as an error — writeFileContent creates it fresh, same as it
// would for any other missing path.
func resolveWriteTarget(path string) (string, error) {
	const maxDepth = 40
	for range maxDepth {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return path, nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		link, err := os.Readlink(path)
		if err != nil {
			return "", fmt.Errorf("reading symlink %s: %w", path, err)
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}
		path = link
	}
	return "", fmt.Errorf("too many levels of symbolic links: %s", path)
}

// writeFileContent creates target's parent directory if needed, writes
// content to a temporary file in the same directory, then atomically
// renames it onto the final path. This prevents partial/corrupt files
// on timeout or crash — the rename is atomic on the same filesystem,
// so readers always see either the old file or the new file, never a
// partially-written one. If path is a symlink, target is its resolved
// destination (see resolveWriteTarget) — the write lands there, and the
// symlink itself is left untouched.
//
// Callers are responsible for any git-tracked guard — this helper is
// also used by RebuildDir, whose individual Files are intentionally NOT
// git-guarded (the whole directory is renderer-owned; see
// applyRebuildDir).
func writeFileContent(path string, content []byte, mode fs.FileMode) (err error) {
	target, err := resolveWriteTarget(path)
	if err != nil {
		return fmt.Errorf("resolving symlink for %s: %w", path, err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", target, err)
	}

	tmp, err := os.CreateTemp(dir, ".agentcfg-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", target, err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any failure path.
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	if _, werr := tmp.Write(content); werr != nil {
		if cerr := tmp.Close(); cerr != nil {
			err = fmt.Errorf("writing %s: %w (closing temp file: %v)", target, werr, cerr)
			return err
		}
		err = fmt.Errorf("writing %s: %w", target, werr)
		return err
	}
	if cerr := tmp.Chmod(mode); cerr != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			err = fmt.Errorf("setting permissions on %s: %w (closing temp file: %v)", target, cerr, closeErr)
			return err
		}
		err = fmt.Errorf("setting permissions on %s: %w", target, cerr)
		return err
	}
	if cerr := tmp.Close(); cerr != nil {
		err = fmt.Errorf("closing %s: %w", target, cerr)
		return err
	}
	if rerr := os.Rename(tmpName, target); rerr != nil {
		err = fmt.Errorf("renaming %s to %s: %w", tmpName, target, rerr)
		return err
	}
	return nil
}

// applyRebuildDir writes every r.Files entry first, then prunes every file
// matching r.Glob under r.Dir that isn't in r.Files — as one unit, so a
// write failure never leaves stale files behind. Individual files are not
// git-guarded: RebuildDir only ever targets renderer-owned directories
// (e.g. an omp agents/ directory), never a file a user might hand-edit and
// commit.
func applyRebuildDir(r render.RebuildDir) (applied, skipped string, err error) {
	dir, err := expandPath(r.Dir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	// Write all replacement files first. If any write fails, no stale
	// files have been removed yet — the directory is still in its
	// pre-apply state.
	for _, f := range r.Files {
		path := filepath.Join(dir, f.Path)
		if err := writeFileContent(path, f.Content, f.Mode); err != nil {
			return "", "", err
		}
	}

	// Now remove stale files that aren't in the replacement set.
	stale, err := filepath.Glob(filepath.Join(dir, r.Glob))
	if err != nil {
		return "", "", fmt.Errorf("globbing %s: %w", filepath.Join(dir, r.Glob), err)
	}
	// Build a set of replacement file basenames for O(1) lookup.
	replaced := make(map[string]bool, len(r.Files))
	for _, f := range r.Files {
		replaced[filepath.Base(f.Path)] = true
	}
	for _, f := range stale {
		if !replaced[filepath.Base(f)] {
			if err := os.Remove(f); err != nil {
				return "", "", fmt.Errorf("removing stale file %s: %w", f, err)
			}
		}
	}

	return fmt.Sprintf("rebuilt %s (%d files)", dir, len(r.Files)), "", nil
}

// rebuildTreeManifestFile is a dotfile agentcfg writes inside every
// RebuildTree.Dir, listing the subdirectory names it rendered on the
// last apply. RebuildTree targets a harness-shared discovery path (e.g.
// Agent Skills' "~/.agents/skills" — see CommandsSkillsDir), not an
// agentcfg-exclusive one like RebuildDir's typical targets: a user or
// another tool may have their own, unrelated subdirectories there.
// Without this manifest, pruning "every subdirectory not in the current
// registry" would delete anything agentcfg didn't happen to render this
// run, including content it never created. The manifest scopes pruning
// to exactly what agentcfg itself previously wrote: a subdirectory is
// only ever removed if it's absent from the current render AND present
// in the manifest from the last apply. A directory that was never
// agentcfg-managed is never touched, no matter its name.
const rebuildTreeManifestFile = ".agentcfg-managed.json"

// readRebuildTreeManifest returns the subdirectory names dir's manifest
// recorded as agentcfg-managed on the last apply. A missing or corrupt
// manifest (first-ever apply, or a directory that predates this
// mechanism) returns an empty set — first-run behavior is to touch
// nothing but what's freshly written, never to guess at prior state.
func readRebuildTreeManifest(dir string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(dir, rebuildTreeManifestFile))
	if err != nil {
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil
	}
	managed := make(map[string]bool, len(names))
	for _, n := range names {
		managed[n] = true
	}
	return managed
}

// writeRebuildTreeManifest persists the subdirectory names this apply
// rendered, so the next apply knows what it's safe to prune.
func writeRebuildTreeManifest(dir string, dirs map[string][]render.WriteFile) error {
	names := make([]string, 0, len(dirs))
	for name := range dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	data, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return writeFileContent(filepath.Join(dir, rebuildTreeManifestFile), data, 0o600)
}

// applyRebuildTree writes every entry in r.Dirs into its own subdirectory
// of r.Dir first, then removes any immediate subdirectory of r.Dir that
// was agentcfg-managed on the last apply (per the manifest) but isn't
// named in r.Dirs now — as one unit, so a write failure never leaves
// stale subdirectories behind, and pruning never touches a subdirectory
// agentcfg didn't itself create (see rebuildTreeManifestFile). Unlike
// applyRebuildDir's basename-keyed pruning, this matches by subdirectory
// name, so entries whose files share an identical basename (every Agent
// Skill's SKILL.md) don't collide. Not git-guarded, for the same reason
// RebuildDir isn't: this only ever removes subdirectories the manifest
// proves are renderer-owned (see RebuildTree's doc comment in
// internal/render/renderer.go).
func applyRebuildTree(r render.RebuildTree) (applied, skipped string, err error) {
	dir, err := expandPath(r.Dir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	previouslyManaged := readRebuildTreeManifest(dir)

	// Write every replacement subdirectory's files first. If any write
	// fails, no stale subdirectory has been removed yet.
	for name, files := range r.Dirs {
		for _, f := range files {
			path := filepath.Join(dir, name, f.Path)
			if err := writeFileContent(path, f.Content, f.Mode); err != nil {
				return "", "", err
			}
		}
	}

	// Now remove only the immediate subdirectories of dir that this
	// renderer managed last time but no longer wants — never a
	// subdirectory absent from the manifest, which agentcfg never
	// created and so must not delete.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", fmt.Errorf("reading directory %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := r.Dirs[e.Name()]; ok {
			continue
		}
		if !previouslyManaged[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return "", "", fmt.Errorf("removing stale directory %s: %w", e.Name(), err)
		}
	}

	if err := writeRebuildTreeManifest(dir, r.Dirs); err != nil {
		return "", "", fmt.Errorf("writing manifest for %s: %w", dir, err)
	}

	return fmt.Sprintf("rebuilt %s (%d subdirectories)", dir, len(r.Dirs)), "", nil
}
