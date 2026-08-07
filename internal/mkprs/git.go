package mkprs

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// gitCommand builds a git invocation rooted at repoPath. The helpers below all
// start here and differ only in what they do with the two streams.
func gitCommand(repoPath string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	return cmd
}

// git runs a git command in repoPath and returns its trimmed stdout. Stderr
// does not reach the caller's output, but does reach the error -- see gitError.
func git(repoPath string, args ...string) (string, error) {
	out, err := gitCommand(repoPath, args...).Output()
	return strings.TrimSpace(string(out)), gitError(err)
}

// gitOK reports whether a git command succeeded, ignoring all of its output.
func gitOK(repoPath string, args ...string) bool {
	_, err := git(repoPath, args...)
	return err == nil
}

// gitTo runs a git command with both streams sent to w.
func gitTo(repoPath string, w io.Writer, args ...string) error {
	cmd := gitCommand(repoPath, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// gitErrTo runs a git command with stdout discarded and stderr sent to w, for
// the commands whose output is noise but whose complaints are not.
func gitErrTo(repoPath string, w io.Writer, args ...string) error {
	cmd := gitCommand(repoPath, args...)
	cmd.Stderr = w
	return cmd.Run()
}

// gitError folds git's own stderr into the error text. exec.Cmd.Output already
// captures stderr into ExitError.Stderr, but Error() reports only "exit status
// 1", so a caller that wraps the error loses the one line explaining it.
func gitError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if msg := strings.TrimSpace(string(exit.Stderr)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
	}
	return err
}

// validateBranchName rejects a name git would refuse, so that one bad -b is one
// message at startup rather than the same failure in every repo. The rules are
// git's own, via check-ref-format, so they cannot drift from what git enforces.
//
// The leading dash has to be tested separately: git's "cannot begin with a
// dash" rule lives in its branch-name path and not in check-ref-format, which
// exits 0 for refs/heads/--draft. That is also the case this check mainly
// exists for -- a flag left without its value, as in `mkprs ~/repos -b --draft
// -- true`.
func validateBranchName(branch string) error {
	// check-ref-format reads only its argument, so there is no repo to run it
	// in; the empty path leaves cmd.Dir unset, i.e. the process's own cwd.
	if strings.HasPrefix(branch, "-") || !gitOK("", "check-ref-format", "refs/heads/"+branch) {
		return fmt.Errorf("invalid branch name %q (see 'git help check-ref-format')", branch)
	}
	return nil
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

// isCleanTree reports whether the working tree has no changes, and whether the
// question could be answered at all.
func isCleanTree(repoPath string) (clean, ok bool) {
	out, err := git(repoPath, "status", "--porcelain")
	if err != nil {
		return false, false
	}
	return out == "", true
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
// last fetch: call this after a pruning fetch, or a branch deleted upstream
// still looks like it exists.
func branchLocation(repoPath, branch string) string {
	if gitOK(repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch) {
		return "locally"
	}
	if gitOK(repoPath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch) {
		return "on origin"
	}
	return ""
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
// whether the question could be answered at all. This is what decides that a
// repo has something to open a PR for.
func branchAhead(repoPath, base, branch string) (ahead, ok bool) {
	out, err := git(repoPath, "rev-list", "--count", base+".."+branch)
	if err != nil {
		return false, false
	}
	return out != "0", true
}

// restoreRepo abandons the working branch and returns the repo to startBranch.
// The checkout and the delete go together on purpose: checking out with the
// command's uncommitted edits still in the tree can carry them across, leaving
// those edits stranded on startBranch after the branch that explains them is
// gone. Callers that do not want the branch deleted must not call this at all.
func restoreRepo(repoPath, startBranch, branch string, w io.Writer) {
	_ = gitTo(repoPath, w, "checkout", startBranch, "--quiet")
	// The "Deleted branch" line is noise, but errors belong in the capture.
	_ = gitErrTo(repoPath, w, "branch", "-D", branch)
}
