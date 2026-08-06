// Package contextres resolves a directory's git remote into the
// host/owner pair that internal/registry's Context.Matches compares
// against. It has no knowledge of the registry itself — it only turns a
// filesystem directory into "what repo is this".
package contextres

import (
	"context"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// RemoteInfo is a resolved git remote's host and owner (e.g. "github.com",
// "athal7").
type RemoteInfo struct {
	Host  string
	Owner string
}

// Resolve determines the git remote host/owner for dir by finding its repo
// root and parsing the "origin" remote URL. It returns (nil, nil) — not an
// error — for every case where a context simply can't be determined: dir
// isn't inside a git repo, there's no "origin" remote, or the remote URL
// can't be parsed into host+owner. This is a deliberate design choice:
// "no context resolvable" is a normal, silent outcome for project-scope
// rendering, not a failure. A non-nil error is reserved for genuinely
// unexpected conditions — none currently arise in this implementation
// (git exiting non-zero for "not a repo" or "no origin" is expected
// control flow, not a Go error), but the signature carries error for
// whatever future failure mode might need to surface as one.
func Resolve(ctx context.Context, dir string) (*RemoteInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	if err := cmd.Run(); err != nil {
		return nil, nil
	}

	cmd = exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	return parseRemoteURL(strings.TrimSpace(string(out))), nil
}

// parseRemoteURL parses a git remote URL into host+owner, supporting the
// three real-world forms: scp-like ("git@github.com:owner/repo.git"),
// https ("https://github.com/owner/repo[.git]"), and explicit ssh
// ("ssh://git@github.com/owner/repo.git"). It returns nil for any shape it
// can't confidently parse, rather than guessing.
func parseRemoteURL(raw string) *RemoteInfo {
	if raw == "" {
		return nil
	}

	if !strings.Contains(raw, "://") {
		return parseSCPLike(raw)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	host := u.Hostname()
	owner := ownerFromPath(u.Path)
	if host == "" || owner == "" {
		return nil
	}
	return &RemoteInfo{Host: host, Owner: owner}
}

// parseSCPLike handles "[user@]host:path" remotes (no "://" scheme) — the
// short form ssh clone URL git itself defaults to for github.com etc.
func parseSCPLike(raw string) *RemoteInfo {
	idx := strings.Index(raw, ":")
	if idx == -1 || strings.Contains(raw[:idx], "/") {
		// No colon at all, or the part before it contains a "/" — that's
		// a local filesystem path remote, not scp-like syntax.
		return nil
	}
	host := stripUserinfo(raw[:idx])
	owner := ownerFromPath(raw[idx+1:])
	if host == "" || owner == "" {
		return nil
	}
	return &RemoteInfo{Host: host, Owner: owner}
}

func stripUserinfo(hostPart string) string {
	if idx := strings.Index(hostPart, "@"); idx != -1 {
		return hostPart[idx+1:]
	}
	return hostPart
}

// ownerFromPath returns the path segment immediately before the repo name
// (the last segment). This is correct regardless of a trailing ".git" on
// the repo name — that suffix lives on the last segment, never the one
// ownerFromPath returns — and regardless of extra nesting (e.g. GitLab
// subgroups), since "immediately before the last segment" is always well
// defined.
func ownerFromPath(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return ""
	}
	return segments[len(segments)-2]
}
