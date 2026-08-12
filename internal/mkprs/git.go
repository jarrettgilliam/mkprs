package mkprs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// repo is a repository mkprs is working in: where it is, and where what git
// says about it goes.
type repo struct {
	path string
	// output is nil for the two callers that ask git a question outside any
	// repo -- validateBranchName and discoverRepos -- which have nowhere to
	// stream to. See log.
	output *capture
}

// log is where a git command's streams go, and the one place that decides it.
func (r repo) log() io.Writer {
	if r.output == nil {
		return io.Discard
	}
	return r.output
}

func (r repo) git(args gitArgs) *gitRun {
	return &gitRun{repo: r, args: args}
}

type gitRun struct {
	repo
	args           gitArgs
	stdout, stderr io.Writer
}

func (g *gitRun) toLog() *gitRun {
	g.stdout, g.stderr = g.log(), g.log()
	return g
}

func (g *gitRun) errToLog() *gitRun {
	g.stderr = g.log()
	return g
}

// run runs the command, discarding whatever stream was not redirected.
// Unredirected stderr is buffered rather than discarded so the error can carry
// git's own words; redirected, the buffer stays empty and gitError leaves the
// error alone, since repeating a complaint already in the capture prints it
// twice.
func (g *gitRun) run() error {
	var complaint bytes.Buffer
	stderr := g.stderr
	if stderr == nil {
		stderr = &complaint
	}

	cmd := exec.Command("git", g.args...)
	cmd.Dir = g.path
	cmd.Stdout, cmd.Stderr = g.stdout, stderr

	return gitError(cmd.Run(), complaint.String())
}

func (g *gitRun) ok() bool { return g.run() == nil }

// text returns the command's trimmed stdout. Stderr reaches the error rather
// than the caller's output unless it was redirected -- see run.
func (g *gitRun) text() (string, error) {
	var out bytes.Buffer
	g.stdout = &out
	err := g.run()
	return strings.TrimSpace(out.String()), err
}

// gitError folds git's own stderr into the error text: an *exec.ExitError
// reports only "exit status 1", so a caller that wraps it loses the one line
// explaining it.
func gitError(err error, stderr string) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if msg := strings.TrimSpace(stderr); msg != "" {
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
	// in; the zero repo leaves cmd.Dir unset, i.e. the process's own cwd.
	if strings.HasPrefix(branch, "-") || !(repo{}).git(checkFormat(branch)).ok() {
		return fmt.Errorf("invalid branch name %q (see 'git help check-ref-format')", branch)
	}
	return nil
}

// originURL is the configured (not insteadOf-rewritten) URL of origin.
func (r repo) originURL() (string, error) {
	return r.git(remoteOriginURL()).text()
}

// isGitHubRepo reports whether origin points at github.com, and the reason when
// it does not.
func (r repo) isGitHubRepo() (bool, string) {
	url, err := r.originURL()
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
func (r repo) isCleanTree() (clean, ok bool) {
	out, err := r.git(status()).text()
	if err != nil {
		return false, false
	}
	return out == "", true
}

// defaultBranch resolves the repo's default branch, preferring origin/HEAD and
// falling back to the conventional names.
func (r repo) defaultBranch() (string, bool) {
	if name, err := r.git(originHeadRef()).text(); err == nil && name != "" {
		return strings.TrimPrefix(name, "origin/"), true
	}
	for _, candidate := range []string{"main", "master"} {
		if r.git(originBranchExists(candidate)).ok() {
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
func (r repo) branchLocation(branch string) string {
	if r.git(localBranchExists(branch)).ok() {
		return "locally"
	}
	if r.git(originBranchExists(branch)).ok() {
		return "on origin"
	}
	return ""
}

// resolveBase prefers the remote-tracking ref so new branches start from what
// origin has, not from a stale local branch.
func (r repo) resolveBase(defaultBranch string) string {
	if r.git(originBranchExists(defaultBranch)).ok() {
		return "origin/" + defaultBranch
	}
	return defaultBranch
}

// headBranch is the checked-out branch, and whether HEAD is on a branch at all.
// symbolic-ref fails on a detached HEAD, which is the distinction callers need.
func (r repo) headBranch() (string, bool) {
	name, err := r.git(currentHeadRef()).text()
	if err != nil || name == "" {
		return "", false
	}
	return name, true
}

// branchAhead reports whether branch carries commits that base does not, and
// whether the question could be answered at all. This is what decides that a
// repo has something to open a PR for.
func (r repo) branchAhead(base, branch string) (ahead, ok bool) {
	out, err := r.git(commitsAhead(base, branch)).text()
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
func (r repo) restore(startBranch, branch string) {
	_ = r.git(checkoutBranch(startBranch)).toLog().run()
	// The "Deleted branch" line is noise, but errors belong in the capture.
	_ = r.git(deleteBranch(branch)).errToLog().run()
}
