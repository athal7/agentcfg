package cli

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSandboxWorkflow_ConfinesValidateRenderApplyToSandbox exercises the
// sandbox-local workflow documented in AGENTS.md/README.md end to end
// through the real agentcfg command tree (newRootCmd, not a helper
// function): with HOME and AGENTCFG_REGISTRY both pointed at paths inside
// one fresh sandbox, validate -> render --explain -> apply must all
// succeed and touch no path outside that sandbox. --target opencode
// scopes every render/apply to the one renderer that never shells out to
// a harness's own CLI, so the test never invokes a real external harness
// binary.
func TestSandboxWorkflow_ConfinesValidateRenderApplyToSandbox(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() before sandboxing: %v", err)
	}
	realOpencodeConfig := filepath.Join(realHome, ".config", "opencode", "opencode.json")
	realConfigBefore := snapshotFile(t, realOpencodeConfig)

	sandbox := t.TempDir()
	registryDir := filepath.Join(sandbox, "registry")
	if err := runInit(io.Discard, registryDir); err != nil {
		t.Fatalf("runInit(%q) failed: %v", registryDir, err)
	}
	sandboxFilesBefore := sandboxFilePaths(t, sandbox)

	t.Setenv("HOME", sandbox)
	t.Setenv("AGENTCFG_REGISTRY", registryDir)

	for _, args := range [][]string{
		{"validate"},
		{"render", "--explain", "--target", "opencode"},
		{"apply", "--target", "opencode"},
	} {
		var out bytes.Buffer
		root := newRootCmd()
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("agentcfg %v failed: %v\noutput:\n%s", args, err, out.String())
		}
	}

	sandboxFilesAfter := sandboxFilePaths(t, sandbox)
	for _, path := range sandboxFilesAfter {
		rel, err := filepath.Rel(sandbox, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("path %q written by the sandboxed run resolves outside sandbox %q", path, sandbox)
		}
	}
	if len(sandboxFilesAfter) <= len(sandboxFilesBefore) {
		t.Fatalf("expected apply to create at least one new file under the sandbox; before=%d after=%d files", len(sandboxFilesBefore), len(sandboxFilesAfter))
	}

	appliedConfig := filepath.Join(sandbox, ".config", "opencode", "opencode.json")
	if _, err := os.Stat(appliedConfig); err != nil {
		t.Fatalf("expected apply to write %s inside the sandbox: %v", appliedConfig, err)
	}

	realConfigAfter := snapshotFile(t, realOpencodeConfig)
	if realConfigAfter != realConfigBefore {
		t.Fatalf("real home config %s changed during a sandboxed run: before=%+v after=%+v", realOpencodeConfig, realConfigBefore, realConfigAfter)
	}
}

// fileSnapshot records enough about a path to detect any create, modify,
// or remove without depending on file content: a size or mtime change
// catches a modification, and exists flipping catches a create/remove.
type fileSnapshot struct {
	exists  bool
	size    int64
	modTime time.Time
}

// snapshotFile stats path, treating a missing file as a valid (absent)
// snapshot rather than a test failure — the real-home opencode.json this
// test guards is not expected to exist on every machine that runs it.
func snapshotFile(t *testing.T, path string) fileSnapshot {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileSnapshot{}
		}
		t.Fatalf("stat %q: %v", path, err)
	}
	return fileSnapshot{exists: true, size: info.Size(), modTime: info.ModTime()}
}

// sandboxFilePaths lists every regular file under root, for diffing the
// sandbox tree before and after a run.
func sandboxFilePaths(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking %q: %v", root, err)
	}
	return files
}
