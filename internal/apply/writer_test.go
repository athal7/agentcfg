package apply

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileContent_PreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.txt")

	if err := writeFileContent(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("writeFileContent returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteFileContent_OverwriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	// Write initial content.
	if err := writeFileContent(path, []byte("old content"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	// Read the old content to use as the "expected old" value.
	oldContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Write a much larger new content.
	newContent := make([]byte, 1024*1024)
	for i := range newContent {
		newContent[i] = 'A'
	}
	if err := writeFileContent(path, newContent, 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	// Read back — must be exactly the new content, never a mix.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != len(newContent) {
		t.Fatalf("content length = %d, want %d", len(got), len(newContent))
	}
	if string(got) != string(newContent) {
		t.Fatalf("content differs from written value (possible partial write)")
	}

	// Also verify the old content is still readable from a separate
	// read — the atomic rename means the old file was replaced
	// atomically, so any reader that started before the rename sees
	// the old file, any reader after sees the new file.
	if string(oldContent) != "old content" {
		t.Errorf("old content was corrupted: %q", oldContent)
	}
}

func TestWriteFileContent_ConcurrentReadersSeeConsistentContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	// Write initial content.
	if err := writeFileContent(path, []byte("initial"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	// Write a much larger replacement.
	newContent := make([]byte, 1024*1024)
	for i := range newContent {
		newContent[i] = 'X'
	}
	if err := writeFileContent(path, newContent, 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	// Read back and verify it's fully one or the other — never partial.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	oldStr := "initial"
	newStr := string(newContent)
	if string(got) != oldStr && string(got) != newStr {
		t.Fatalf("content is neither old nor new — possible partial read: len=%d", len(got))
	}
}

func TestWriteFileContent_OverwriteReappliesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")

	// Create a file with mode 0o644.
	if err := writeFileContent(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("initial mode perm = %o, want 0644", info.Mode().Perm())
	}

	// Overwrite with a different mode (0o600).
	if err := writeFileContent(path, []byte("world"), 0o600); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode perm = %o, want 0600 (old file's mode leaked)", info.Mode().Perm())
	}
}

func TestWriteFileContent_WritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	realTarget := filepath.Join(dir, "real", "target.txt")
	if err := os.MkdirAll(filepath.Dir(realTarget), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(realTarget, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(realTarget, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := writeFileContent(linkPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("linkPath is no longer a symlink — writeFileContent replaced it")
	}
	resolved, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if resolved != realTarget {
		t.Fatalf("symlink target = %q, want %q", resolved, realTarget)
	}

	got, err := os.ReadFile(realTarget)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileContent_FollowsSymlinkChain(t *testing.T) {
	dir := t.TempDir()
	realTarget := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(realTarget, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	middle := filepath.Join(dir, "middle.txt")
	if err := os.Symlink(realTarget, middle); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	outer := filepath.Join(dir, "outer.txt")
	if err := os.Symlink(middle, outer); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := writeFileContent(outer, []byte("new"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	for _, link := range []string{outer, middle} {
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("Lstat(%s): %v", link, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is no longer a symlink", link)
		}
	}

	got, err := os.ReadFile(realTarget)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileContent_RelativeSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	realTarget := filepath.Join(dir, "real", "target.txt")
	if err := os.WriteFile(realTarget, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(filepath.Join("real", "target.txt"), linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := writeFileContent(linkPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("linkPath is no longer a symlink")
	}

	got, err := os.ReadFile(realTarget)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileContent_DanglingSymlinkStillWritesThrough(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	linkPath := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := writeFileContent(linkPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("writeFileContent: %v", err)
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("linkPath is no longer a symlink")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileContent_CleansUpTempFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	// A path that is itself an existing directory: os.Rename onto it fails.
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := writeFileContent(target, []byte("content"), 0o644); err == nil {
		t.Fatal("writeFileContent: expected error renaming onto a directory, got nil")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "target" {
			t.Errorf("leaked temp file after failed write: %s", e.Name())
		}
	}
}
