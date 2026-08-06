package contextres

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want *RemoteInfo
	}{
		{
			name: "scp-like",
			raw:  "git@github.com:athal7/agentcfg.git",
			want: &RemoteInfo{Host: "github.com", Owner: "athal7"},
		},
		{
			name: "https with .git suffix",
			raw:  "https://github.com/athal7/agentcfg.git",
			want: &RemoteInfo{Host: "github.com", Owner: "athal7"},
		},
		{
			name: "https without .git suffix",
			raw:  "https://github.com/athal7/agentcfg",
			want: &RemoteInfo{Host: "github.com", Owner: "athal7"},
		},
		{
			name: "explicit ssh scheme",
			raw:  "ssh://git@github.com/athal7/agentcfg.git",
			want: &RemoteInfo{Host: "github.com", Owner: "athal7"},
		},
		{
			name: "https with port is stripped from host",
			raw:  "https://git.example.com:8443/athal7/agentcfg.git",
			want: &RemoteInfo{Host: "git.example.com", Owner: "athal7"},
		},
		{
			name: "nested gitlab subgroup path uses segment immediately before repo name",
			raw:  "https://gitlab.example.com/group/subgroup/repo.git",
			want: &RemoteInfo{Host: "gitlab.example.com", Owner: "subgroup"},
		},
		{
			name: "empty string",
			raw:  "",
			want: nil,
		},
		{
			name: "local filesystem path is not parseable",
			raw:  "/Users/athal/code/agentcfg",
			want: nil,
		},
		{
			name: "scheme URL with no owner segment",
			raw:  "https://github.com/justonesegment",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRemoteURL(tt.raw)
			if !remoteInfoEqual(got, tt.want) {
				t.Errorf("parseRemoteURL(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func remoteInfoEqual(a, b *RemoteInfo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// runGit runs a git command in dir, failing the test on error. Used only
// to set up fixtures inside t.TempDir() — never touches a real repo.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--allow-empty", "-q", "-m", "init")
}

func TestResolve_NotAGitRepoAtAll(t *testing.T) {
	dir := t.TempDir()

	got, err := Resolve(context.Background(), dir)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != nil {
		t.Errorf("Resolve(%q) = %+v, want nil (not a git repo)", dir, got)
	}
}

func TestResolve_RepoWithNoOriginRemote(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	got, err := Resolve(context.Background(), dir)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != nil {
		t.Errorf("Resolve(%q) = %+v, want nil (no origin remote)", dir, got)
	}
}

func TestResolve_RepoWithOriginRemote(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	runGit(t, dir, "remote", "add", "origin", "git@github.com:athal7/agentcfg.git")

	got, err := Resolve(context.Background(), dir)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	want := &RemoteInfo{Host: "github.com", Owner: "athal7"}
	if !remoteInfoEqual(got, want) {
		t.Errorf("Resolve(%q) = %+v, want %+v", dir, got, want)
	}
}

func TestResolve_WorksFromSubdirectoryOfRepo(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/athal7/agentcfg.git")

	subdir := filepath.Join(dir, "nested", "deeper")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}

	got, err := Resolve(context.Background(), subdir)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	want := &RemoteInfo{Host: "github.com", Owner: "athal7"}
	if !remoteInfoEqual(got, want) {
		t.Errorf("Resolve(%q) = %+v, want %+v", subdir, got, want)
	}
}
