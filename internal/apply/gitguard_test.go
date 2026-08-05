package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a real git repo in dir via the git binary — this
// exercises the actual "git ls-files" contract isGitTracked relies on,
// not a mock of it.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-q", "-m", "init")
}

func gitAdd(t *testing.T, dir, relPath string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("add", relPath)
	run("-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-q", "-m", "add "+relPath)
}

func TestIsGitTracked_TrackedFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gitAdd(t, dir, "tracked.txt")

	tracked, err := isGitTracked(path)
	if err != nil {
		t.Fatalf("isGitTracked returned error: %v", err)
	}
	if !tracked {
		t.Errorf("isGitTracked(%q) = false, want true", path)
	}
}

func TestIsGitTracked_UntrackedFileInRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	path := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Deliberately not added/committed.

	tracked, err := isGitTracked(path)
	if err != nil {
		t.Fatalf("isGitTracked returned error: %v", err)
	}
	if tracked {
		t.Errorf("isGitTracked(%q) = true, want false (never added)", path)
	}
}

func TestIsGitTracked_NotAGitRepoAtAll(t *testing.T) {
	dir := t.TempDir() // no git init at all

	path := filepath.Join(dir, "somefile.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	tracked, err := isGitTracked(path)
	if err != nil {
		t.Fatalf("isGitTracked returned error: %v", err)
	}
	if tracked {
		t.Errorf("isGitTracked(%q) = true, want false (not a git repo)", path)
	}
}

func TestIsGitTracked_FileDoesNotExistYetInRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// A path that has never been written at all — the common case when
	// apply is about to create a brand-new file.
	path := filepath.Join(dir, "not-yet-created.txt")

	tracked, err := isGitTracked(path)
	if err != nil {
		t.Fatalf("isGitTracked returned error: %v", err)
	}
	if tracked {
		t.Errorf("isGitTracked(%q) = true, want false (file doesn't exist)", path)
	}
}

func TestIsGitTracked_DeletedParentDirReturnsFalseNil(t *testing.T) {
	// When the parent directory is gone, the file can't possibly be
	// tracked. Return (false, nil) so apply proceeds with the write.
	dir := t.TempDir()
	initGitRepo(t, dir)

	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gitAdd(t, dir, "tracked.txt")

	// Delete the parent directory — the file is still tracked in git,
	// but the path no longer exists on disk.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	// Parent dir is gone → file can't be tracked → (false, nil).
	tracked, err := isGitTracked(path)
	if err != nil {
		t.Errorf("isGitTracked(%q) = (_, %v), want (false, nil) when parent dir is gone", path, err)
	}
	if tracked {
		t.Errorf("isGitTracked(%q) = (true, nil), want (false, nil) when parent dir is gone", path)
	}
}

func TestIsGitTracked_TrackedFileDeletedFromIndexStillDetected(t *testing.T) {
	// When a file was tracked but deleted from the working tree (and
	// also removed from the index), git ls-files --error-unmatch fails.
	// This is a "confirmed not tracked" result.
	dir := t.TempDir()
	initGitRepo(t, dir)

	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	gitAdd(t, dir, "tracked.txt")

	// Remove from index and working tree.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("rm", "--cached", "tracked.txt")
	os.Remove(path)

	tracked, err := isGitTracked(path)
	if err != nil {
		t.Fatalf("isGitTracked returned error: %v", err)
	}
	if tracked {
		t.Errorf("isGitTracked(%q) = true, want false (removed from index)", path)
	}
}
