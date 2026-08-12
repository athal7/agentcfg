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
// one fresh sandbox, validate -> render --explain -> apply must each run
// in turn, with validate and render --explain writing nothing anywhere
// and apply writing only inside the sandbox. The test never touches the
// real process home directory — a second, independent t.TempDir() stands
// in for "some other home-directory tree" the run must never touch, and
// every filesystem check stays confined to the two temp dirs. --target
// opencode scopes every render/apply to the one renderer that never
// shells out to a harness's own CLI, so the test never invokes a real
// external harness binary.
func TestSandboxWorkflow_ConfinesValidateRenderApplyToSandbox(t *testing.T) {
	processHome := os.Getenv("HOME")

	simulatedHostHome := t.TempDir()
	sandbox := t.TempDir()
	if simulatedHostHome == processHome || sandbox == processHome {
		t.Fatalf("t.TempDir() collided with the real process HOME %q; refusing to run", processHome)
	}

	registryDir := filepath.Join(sandbox, "registry")
	if err := runInit(io.Discard, registryDir); err != nil {
		t.Fatalf("runInit(%q) failed: %v", registryDir, err)
	}
	hostHomeBefore := treeSnapshot(t, simulatedHostHome)
	sandboxBaseline := treeSnapshot(t, sandbox)

	t.Setenv("HOME", sandbox)
	t.Setenv("AGENTCFG_REGISTRY", registryDir)

	runAgentcfg(t, "validate")
	if after := treeSnapshot(t, sandbox); !treesEqual(sandboxBaseline, after) {
		t.Fatalf("validate wrote to the sandbox; before=%+v after=%+v", sandboxBaseline, after)
	}

	explainOut := runAgentcfg(t, "render", "--explain", "--target", "opencode")
	if after := treeSnapshot(t, sandbox); !treesEqual(sandboxBaseline, after) {
		t.Fatalf("render --explain wrote to the sandbox; before=%+v after=%+v", sandboxBaseline, after)
	}
	if !strings.Contains(explainOut, "== opencode") || !strings.Contains(explainOut, "opencode.json") {
		t.Fatalf("render --explain output missing the expected opencode plan:\n%s", explainOut)
	}

	runAgentcfg(t, "apply", "--target", "opencode")

	sandboxAfterApply := treeSnapshot(t, sandbox)
	if len(sandboxAfterApply) <= len(sandboxBaseline) {
		t.Fatalf("expected apply to create at least one new file under the sandbox; before=%d after=%d files", len(sandboxBaseline), len(sandboxAfterApply))
	}
	appliedConfig := filepath.Join(sandbox, ".config", "opencode", "opencode.json")
	if _, ok := sandboxAfterApply[filepath.Join(".config", "opencode", "opencode.json")]; !ok {
		t.Fatalf("expected apply to write %s inside the sandbox", appliedConfig)
	}

	if after := treeSnapshot(t, simulatedHostHome); !treesEqual(hostHomeBefore, after) {
		t.Fatalf("simulated host home changed during a sandboxed run: before=%+v after=%+v", hostHomeBefore, after)
	}
}

// runAgentcfg executes newRootCmd with args, failing the test on any
// error and returning everything the command wrote to stdout/stderr.
func runAgentcfg(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("agentcfg %v failed: %v\noutput:\n%s", args, err, out.String())
	}
	return out.String()
}

// fileSnapshot records enough about a path to detect a modification
// without depending on file content: a size or mtime change catches it.
type fileSnapshot struct {
	size    int64
	modTime time.Time
}

// treeSnapshot maps every regular file under root, keyed by its path
// relative to root, to a fileSnapshot — for detecting any create,
// modify, or remove under root between two points in a test.
func treeSnapshot(t *testing.T, root string) map[string]fileSnapshot {
	t.Helper()
	snap := make(map[string]fileSnapshot)
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		snap[rel] = fileSnapshot{size: info.Size(), modTime: info.ModTime()}
		return nil
	}); err != nil {
		t.Fatalf("walking %q: %v", root, err)
	}
	return snap
}

// treesEqual reports whether two treeSnapshot results describe the same
// set of files with the same size and modification time.
func treesEqual(a, b map[string]fileSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for path, snapA := range a {
		snapB, ok := b[path]
		if !ok || snapA.size != snapB.size || !snapA.modTime.Equal(snapB.modTime) {
			return false
		}
	}
	return true
}
