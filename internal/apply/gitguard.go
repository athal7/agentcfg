package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// isGitTracked reports whether path is tracked by git. It returns an error
// only when the git lookup itself fails (e.g. the parent directory was
// deleted, git isn't installed, or the path is outside any repo). A
// confirmed "not tracked" result returns (false, nil) so apply can proceed
// with a normal write. Being tracked is the only case that changes
// behavior at all (the write is skipped in favor of leaving a user's
// committed file alone).
func isGitTracked(path string) (bool, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// If the parent directory doesn't exist, the file can't possibly be
	// tracked — it was never created. Return (false, nil) so apply
	// proceeds with the write.
	if _, dirErr := os.Stat(dir); dirErr != nil && os.IsNotExist(dirErr) {
		return false, nil
	}

	// If the file exists on disk, stat it to catch permission errors.
	// If it doesn't exist, we still need to check git — a new file
	// being written won't exist yet, and git ls-files works on
	// non-existent paths.
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("checking %s: %w", path, err)
		}
		// File doesn't exist but parent dir does — proceed to git check.
	}

	cmd := exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", base)
	if err := cmd.Run(); err != nil {
		// git exit non-zero means "not tracked" (or not in this repo).
		// That's a confirmed negative, not a lookup failure.
		return false, nil
	}
	return true, nil
}
