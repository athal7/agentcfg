// Package apply executes the render.Output values a Renderer produces:
// writing files, merging structured config, rebuilding directories, and
// running commands. Nothing in internal/render performs I/O — render.Plan
// is the dry-run/preview mechanism, produced with zero filesystem or
// process side effects. apply is where a Plan actually takes effect.
package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// writeFileContent creates path's parent directory if needed, writes
// content to a temporary file in the same directory, then atomically
// renames it onto the final path. This prevents partial/corrupt files
// on timeout or crash — the rename is atomic on the same filesystem,
// so readers always see either the old file or the new file, never a
// partially-written one.
//
// Callers are responsible for any git-tracked guard — this helper is
// also used by RebuildDir, whose individual Files are intentionally NOT
// git-guarded (the whole directory is renderer-owned; see
// applyRebuildDir).
func writeFileContent(path string, content []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, ".agentcfg-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any failure path.
	defer func() {
		if err != nil {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions on %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpName, path, err)
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
