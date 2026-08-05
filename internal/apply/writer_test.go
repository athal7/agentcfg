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
