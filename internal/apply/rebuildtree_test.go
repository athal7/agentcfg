package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/athal7/agentcfg/internal/render"
)

func TestApplyRebuildTree_WritesEachEntryIntoItsOwnSubdirectory(t *testing.T) {
	dir := t.TempDir()

	_, _, err := applyRebuildTree(render.RebuildTree{
		Dir: dir,
		Dirs: map[string][]render.WriteFile{
			"review": {{Path: "SKILL.md", Mode: 0o600, Content: []byte("review body")}},
			"ship":   {{Path: "SKILL.md", Mode: 0o600, Content: []byte("ship body")}},
		},
	})
	if err != nil {
		t.Fatalf("applyRebuildTree returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "review", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading review/SKILL.md: %v", err)
	}
	if string(got) != "review body" {
		t.Errorf("review/SKILL.md = %q, want %q", got, "review body")
	}

	got, err = os.ReadFile(filepath.Join(dir, "ship", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading ship/SKILL.md: %v", err)
	}
	if string(got) != "ship body" {
		t.Errorf("ship/SKILL.md = %q, want %q", got, "ship body")
	}
}

func TestApplyRebuildTree_PrunesSubdirectoryNoLongerListed(t *testing.T) {
	dir := t.TempDir()

	// First apply: two entries, both with an identically-named inner file
	// ("SKILL.md") — the exact shape RebuildDir's basename-keyed pruning
	// can't handle correctly.
	if _, _, err := applyRebuildTree(render.RebuildTree{
		Dir: dir,
		Dirs: map[string][]render.WriteFile{
			"review": {{Path: "SKILL.md", Mode: 0o600, Content: []byte("review body")}},
			"ship":   {{Path: "SKILL.md", Mode: 0o600, Content: []byte("ship body")}},
		},
	}); err != nil {
		t.Fatalf("first applyRebuildTree returned error: %v", err)
	}

	// Second apply: "ship" removed from the registry (as if a command was
	// deleted). "review" must survive with its content untouched, and
	// "ship"'s directory must be gone entirely, not just its file.
	if _, _, err := applyRebuildTree(render.RebuildTree{
		Dir: dir,
		Dirs: map[string][]render.WriteFile{
			"review": {{Path: "SKILL.md", Mode: 0o600, Content: []byte("review body")}},
		},
	}); err != nil {
		t.Fatalf("second applyRebuildTree returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "ship")); !os.IsNotExist(err) {
		t.Errorf("expected ship/ to be removed entirely, stat err = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "review", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading review/SKILL.md after prune: %v", err)
	}
	if string(got) != "review body" {
		t.Errorf("review/SKILL.md after prune = %q, want untouched %q", got, "review body")
	}
}

// TestApplyRebuildTree_EmptyDirsPrunesEverything covers the "command
// removed from the registry entirely" case: every subdirectory this
// package itself rendered on the prior apply is pruned. The manifest
// file (rebuildTreeManifestFile) is expected to remain — it's agentcfg's
// own bookkeeping, not a rendered command, and lives at dir's top level
// as a plain file, never a subdirectory a harness's skill discovery
// would ever walk into.
func TestApplyRebuildTree_EmptyDirsPrunesEverything(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := applyRebuildTree(render.RebuildTree{
		Dir:  dir,
		Dirs: map[string][]render.WriteFile{"review": {{Path: "SKILL.md", Content: []byte("x")}}},
	}); err != nil {
		t.Fatalf("first applyRebuildTree returned error: %v", err)
	}

	if _, _, err := applyRebuildTree(render.RebuildTree{Dir: dir, Dirs: map[string][]render.WriteFile{}}); err != nil {
		t.Fatalf("second applyRebuildTree returned error: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != rebuildTreeManifestFile || entries[0].IsDir() {
		t.Errorf("got entries %v, want exactly the manifest file %q", entries, rebuildTreeManifestFile)
	}
}

// TestApplyRebuildTree_NeverPrunesADirectoryItNeverManaged covers the
// CodeRabbit finding on PR #27: RebuildTree.Dir is a harness-shared
// discovery path (Agent Skills' "~/.agents/skills"), not an
// agentcfg-exclusive directory. A subdirectory that predates agentcfg
// ever running there (no manifest entry for it) must survive every
// apply indefinitely, even across many runs, even if its name never
// appears in any registry — agentcfg only ever prunes what it can prove,
// via the manifest, that it created itself.
func TestApplyRebuildTree_NeverPrunesADirectoryItNeverManaged(t *testing.T) {
	dir := t.TempDir()

	unrelated := filepath.Join(dir, "local-skill")
	if err := os.MkdirAll(unrelated, 0o755); err != nil {
		t.Fatalf("seeding local-skill/: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "SKILL.md"), []byte("hand-authored"), 0o644); err != nil {
		t.Fatalf("seeding local-skill/SKILL.md: %v", err)
	}

	for i := range 2 {
		if _, _, err := applyRebuildTree(render.RebuildTree{
			Dir:  dir,
			Dirs: map[string][]render.WriteFile{"review": {{Path: "SKILL.md", Content: []byte("x")}}},
		}); err != nil {
			t.Fatalf("applyRebuildTree run %d returned error: %v", i, err)
		}
	}

	got, err := os.ReadFile(filepath.Join(unrelated, "SKILL.md"))
	if err != nil {
		t.Fatalf("expected local-skill/SKILL.md to survive, stat/read err = %v", err)
	}
	if string(got) != "hand-authored" {
		t.Errorf("local-skill/SKILL.md content = %q, want untouched %q", got, "hand-authored")
	}
}

func TestApplyRebuildTree_UnrelatedFileInDirIsIgnored(t *testing.T) {
	dir := t.TempDir()
	// A plain file (not a directory) directly under dir must never be
	// touched — RebuildTree only manages immediate *subdirectories*.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("seeding README.md: %v", err)
	}

	if _, _, err := applyRebuildTree(render.RebuildTree{
		Dir:  dir,
		Dirs: map[string][]render.WriteFile{"review": {{Path: "SKILL.md", Content: []byte("x")}}},
	}); err != nil {
		t.Fatalf("applyRebuildTree returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("expected README.md to survive untouched, stat err = %v", err)
	}
}

func TestApplyOne_DispatchesRebuildTree(t *testing.T) {
	dir := t.TempDir()
	applied, skipped, err := applyOne(render.RebuildTree{
		Dir:  dir,
		Dirs: map[string][]render.WriteFile{"review": {{Path: "SKILL.md", Content: []byte("x")}}},
	})
	if err != nil {
		t.Fatalf("applyOne returned error: %v", err)
	}
	if skipped != "" {
		t.Errorf("got skipped %q, want empty", skipped)
	}
	if applied == "" {
		t.Error("got empty applied description, want a non-empty summary")
	}
}
