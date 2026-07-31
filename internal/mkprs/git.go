package mkprs

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// git runs a git command in repoPath and returns its trimmed stdout. Stderr is
// discarded; callers that need it route output through gitTo instead.
func git(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// gitOK reports whether a git command succeeded, ignoring all of its output.
func gitOK(repoPath string, args ...string) bool {
	_, err := git(repoPath, args...)
	return err == nil
}

// gitTo runs a git command with both streams sent to w.
func gitTo(repoPath string, w io.Writer, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// originURL is the configured (not insteadOf-rewritten) URL of origin.
func originURL(repoPath string) (string, error) {
	return git(repoPath, "config", "--get", "remote.origin.url")
}

// isGitHubRepo reports whether origin points at github.com, and the reason when
// it does not.
func isGitHubRepo(repoPath string) (bool, string) {
	url, err := originURL(repoPath)
	if err != nil {
		return false, "no 'origin' remote"
	}
	if !strings.Contains(url, "github.com:") && !strings.Contains(url, "github.com/") {
		return false, fmt.Sprintf("non-GitHub remote (%s)", url)
	}
	return true, ""
}

// isCleanTree reports whether the working tree has no changes.
func isCleanTree(repoPath string) bool {
	out, err := git(repoPath, "status", "--porcelain")
	return err == nil && out == ""
}

// getDefaultBranch resolves the repo's default branch, preferring origin/HEAD and
// falling back to the conventional names.
func getDefaultBranch(repoPath string) (string, bool) {
	if name, err := git(repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && name != "" {
		return strings.TrimPrefix(name, "origin/"), true
	}
	for _, candidate := range []string{"main", "master"} {
		if gitOK(repoPath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+candidate) {
			return candidate, true
		}
	}
	return "", false
}

// branchLocation reports where the branch already exists -- "locally", "on
// origin", or "" when it does not exist at all. Local wins when both match.
//
// The origin half reads a remote-tracking ref, which is only as fresh as the
// last fetch: call this after fetchOrigin, or a branch deleted upstream still
// looks like it exists.
func branchLocation(repoPath, branch string) string {
	if gitOK(repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch) {
		return "locally"
	}
	if gitOK(repoPath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch) {
		return "on origin"
	}
	return ""
}

// fetchOrigin refreshes origin, reporting into w but never failing the repo:
// stale local refs are better than no run at all.
func fetchOrigin(repoPath, repoName string, w io.Writer) {
	if err := gitTo(repoPath, w, "fetch", "origin", "--quiet", "--prune"); err != nil {
		fmt.Fprintf(w, "Could not fetch origin for %s; using local refs.\n", repoName)
	}
}

// resolveBase prefers the remote-tracking ref so new branches start from what
// origin has, not from a stale local branch.
func resolveBase(repoPath, dflt string) string {
	if gitOK(repoPath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+dflt) {
		return "origin/" + dflt
	}
	return dflt
}

// headBranch is the checked-out branch, and whether HEAD is on a branch at all.
// symbolic-ref fails on a detached HEAD, which is the distinction callers need.
func headBranch(repoPath string) (string, bool) {
	name, err := git(repoPath, "symbolic-ref", "--short", "HEAD")
	if err != nil || name == "" {
		return "", false
	}
	return name, true
}

// branchAhead reports whether branch carries commits that base does not, and
// whether the question could be answered at all.
//
// This is what decides that a repo has something to open a PR for. A commit the
// command made itself counts exactly as much as one mkprs made, which is why
// this asks about the branch rather than about the index.
func branchAhead(repoPath, base, branch string) (ahead, ok bool) {
	out, err := git(repoPath, "rev-list", "--count", base+".."+branch)
	if err != nil {
		return false, false
	}
	return out != "0", true
}

// restoreRepo abandons the working branch and returns the repo to dflt.
func restoreRepo(repoPath, dflt, branch string, w io.Writer) {
	_ = gitTo(repoPath, w, "checkout", dflt, "--quiet")
	// Matches the original's `git branch -D ... >/dev/null`: the "Deleted
	// branch" line is noise, but errors still belong in the capture.
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = repoPath
	cmd.Stderr = w
	_ = cmd.Run()
}
