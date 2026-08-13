package mkprs

import (
	"fmt"
	"io"
	"strings"
)

// repo is a repository mkprs is working in, for the length of its turn through
// the pipeline: where it is, and the capture its steps write to. cfg and prs are
// the run's rather than this repo's -- they are the same for every repo -- and
// are held here because every step needs them.
type repo struct {
	path   string
	output *capture
	cfg    config
	prs    prOpener
}

func newRepo(cfg config, prs prOpener, path string, output *capture) *repo {
	return &repo{path: path, output: output, cfg: cfg, prs: prs}
}

// log is where a git command's streams go, and the one place that decides it.
// It is io.Discard before a repo has a capture to stream to.
func (r *repo) log() io.Writer {
	if r.output == nil {
		return io.Discard
	}
	return r.output
}

// isGitHubRepo reports whether origin points at github.com, and the reason when
// it does not.
func (r *repo) isGitHubRepo() (bool, string) {
	url, err := git(r.path, remoteOriginURL()).text()
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
func (r *repo) isCleanTree() (clean, ok bool) {
	out, err := git(r.path, status()).text()
	if err != nil {
		return false, false
	}
	return out == "", true
}

// defaultBranch resolves the repo's default branch, preferring origin/HEAD and
// falling back to the conventional names.
func (r *repo) defaultBranch() (string, bool) {
	if name, err := git(r.path, originHeadRef()).text(); err == nil && name != "" {
		return strings.TrimPrefix(name, "origin/"), true
	}
	for _, candidate := range []string{"main", "master"} {
		if git(r.path, originBranchExists(candidate)).ok() {
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
func (r *repo) branchLocation(branch string) string {
	if git(r.path, localBranchExists(branch)).ok() {
		return "locally"
	}
	if git(r.path, originBranchExists(branch)).ok() {
		return "on origin"
	}
	return ""
}

// resolveBase prefers the remote-tracking ref so new branches start from what
// origin has, not from a stale local branch.
func (r *repo) resolveBase(defaultBranch string) string {
	if git(r.path, originBranchExists(defaultBranch)).ok() {
		return "origin/" + defaultBranch
	}
	return defaultBranch
}

// headBranch is the checked-out branch, and whether HEAD is on a branch at all.
// symbolic-ref fails on a detached HEAD, which is the distinction callers need.
func (r *repo) headBranch() (string, bool) {
	name, err := git(r.path, currentHeadRef()).text()
	if err != nil || name == "" {
		return "", false
	}
	return name, true
}

// branchAhead reports whether branch carries commits that base does not, and
// whether the question could be answered at all. This is what decides that a
// repo has something to open a PR for.
func (r *repo) branchAhead(base, branch string) (ahead, ok bool) {
	out, err := git(r.path, commitsAhead(base, branch)).text()
	if err != nil {
		return false, false
	}
	return out != "0", true
}

// restore abandons the working branch and returns the repo to startBranch.
// The checkout and the delete go together on purpose: checking out with the
// command's uncommitted edits still in the tree can carry them across, leaving
// those edits stranded on startBranch after the branch that explains them is
// gone. Callers that do not want the branch deleted must not call this at all.
func (r *repo) restore(startBranch, branch string) {
	_ = git(r.path, checkoutBranch(startBranch)).to(r.log(), r.log()).run()
	// The "Deleted branch" line is noise, but errors belong in the capture.
	_ = git(r.path, deleteBranch(branch)).to(nil, r.log()).run()
}
