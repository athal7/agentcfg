package apply

import (
	"os/exec"
	"path/filepath"
)

// isGitTracked reports whether path is tracked by git. Any failure to
// determine this — path isn't inside a git repo, git isn't installed, or
// the file genuinely isn't tracked — resolves to false ("not tracked"),
// so apply proceeds with a normal write. Being tracked is the only case
// that changes behavior at all (the write is skipped in favor of leaving
// a user's committed file alone), so every failure mode safely defaults
// to "write it".
func isGitTracked(path string) bool {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	cmd := exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", base)
	return cmd.Run() == nil
}
