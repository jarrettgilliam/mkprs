package mkprs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// openPR is the last step, and can end only two ways. base is the repo's own
// default branch, the same one the working branch was cut from, so the PR's
// diff is exactly the commit this run just made.
func (a *app) openPR(repoPath, base string, c *capture) outcome {
	url, err := a.prs.open(repoPath, pullRequest{
		Base:      base,
		Head:      a.cfg.branch,
		Title:     a.cfg.title,
		Body:      a.cfg.body,
		Reviewers: a.cfg.reviewers,
		Draft:     a.cfg.draft,
	}, c)
	if err != nil {
		return fail(err.Error(), c)
	}
	return success(url)
}

// prep is what the pre-flight filters hand to the rest of the run: three branch
// names that are all read from the repo before it is touched.
type prep struct {
	// defaultBranch is the repo's own default, which the PR targets.
	defaultBranch string
	// startBranch is whatever was checked out when the run reached this repo,
	// and where it gets put back. Bookkeeping only -- it never contributes to
	// what the run produces.
	startBranch string
	// base is the fork point the working branch is cut from, and what the
	// commit count is measured against. Normally origin/<defaultBranch>.
	base string
}

// preflight runs every check that can end a repo before its command does, and
// reads the names the rest of processRepo needs. A non-nil outcome means stop;
// prep is meaningless in that case.
//
// This is the one place where nil-means-carry-on lives. Six filters share a
// single such call site here rather than each becoming a helper with its own.
func preflight(repoPath, branch string, c *capture) (prep, outcome) {
	if ok, note := isGitHubRepo(repoPath); !ok {
		return prep{}, skip(note)
	}

	if !isCleanTree(repoPath) {
		return prep{}, skip("working tree not clean")
	}
	// Fetch before any decision that reads a ref. --prune here is what clears
	// refs/remotes/origin/<branch> after a PR is merged and its branch deleted
	// upstream; checking first would skip the repo on a ref that is only stale.
	fetchOrigin(repoPath, filepath.Base(repoPath), c)

	defaultBranch, ok := getDefaultBranch(repoPath)
	if !ok {
		return prep{}, skip("could not determine default branch")
	}

	// Whatever is checked out now is where the repo is put back, so it has to be
	// recorded before the branch is cut. A detached HEAD has no name to record,
	// which is the one state worth skipping over.
	startBranch, ok := headBranch(repoPath)
	if !ok {
		return prep{}, skip("not on a branch (detached HEAD)")
	}

	if where := branchLocation(repoPath, branch); where != "" {
		return prep{}, skip(fmt.Sprintf("branch '%s' already exists %s", branch, where))
	}

	return prep{
		defaultBranch: defaultBranch,
		startBranch:   startBranch,
		base:          resolveBase(repoPath, defaultBranch),
	}, nil
}

// expandCommand substitutes the repo's path for every argument that is exactly
// {}, leaving all others untouched. The result is a new slice: the config's
// command is shared by every repo in the run.
func expandCommand(command []string, abs string) []string {
	expanded := make([]string, 0, len(command))
	for _, arg := range command {
		if arg == "{}" {
			expanded = append(expanded, abs)
		} else {
			expanded = append(expanded, arg)
		}
	}
	return expanded
}

// cleanup returns the repo to startBranch and deletes the working branch. It
// runs deferred, from the point the branch exists onward -- including the
// success path, after the PR is opened.
//
// Two cases opt out, and both mean "leave it alone" rather than "delete the
// branch but stay put": cleanup is all or nothing, because checking out
// startBranch drags any uncommitted edits along with it.
//
// A failure is left exactly as it broke, so nothing is lost to a problem the
// user has not seen yet: a failed push means origin has no copy of the commit,
// and a command that failed halfway leaves edits worth reading. The repo
// sitting on mkprs's branch is also the signal that it needs attention.
func (a *app) cleanup(res outcome, repoPath, startBranch string, c *capture) {
	if a.cfg.keepBranch {
		return
	}
	if _, broke := res.(outcomeFailed); broke {
		return
	}
	restoreRepo(repoPath, startBranch, a.cfg.branch, c)
}

// runCommand runs the user's command in the repo, with both streams going to
// the capture. It is the one step whose behaviour mkprs does not define, so all
// this owns is the boundary: the working directory, the two variables the
// command is promised, and a failure reported the way a shell would.
//
// stdin is deliberately left unset -- the command runs against /dev/null, so it
// cannot consume input meant for mkprs itself.
func (a *app) runCommand(repoPath string, c *capture) error {
	abs := resolvePath(repoPath)
	expanded := expandCommand(a.cfg.command, abs)

	cmd := exec.Command(expanded[0], expanded[1:]...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "REPO="+abs, "REPO_NAME="+filepath.Base(repoPath))
	cmd.Stdout = c
	cmd.Stderr = c

	if err := cmd.Run(); err != nil {
		// The command's own output is already in the capture and gets replayed
		// under the failure, so the error carries only the status.
		return fmt.Errorf("command exited %d", exitCode(err))
	}
	return nil
}

// commitAndPush turns whatever the command left behind into a branch on origin,
// ready for a pull request. A nil outcome means it did: everything else ends the
// repo here.
//
// This is the mutating half of a repo's run, and the only part that can end it
// either way -- "the command changed nothing" is a skip, not a failure -- which
// is what it needs an outcome rather than an error to say.
func (a *app) commitAndPush(repoPath string, p prep, c *capture) outcome {
	branch := a.cfg.branch

	// mkprs cut this branch and everything below assumes it: staging and
	// committing act on whatever is checked out, so a command that wandered off
	// would commit to a branch mkprs does not own. A branch the command created
	// survives -- cleanup only ever deletes mkprs's own.
	work, ok := headBranch(repoPath)
	if !ok {
		return fail("command left the repo with a detached HEAD", c)
	}
	if work != branch {
		return fail(fmt.Sprintf("command left the repo on '%s', not '%s'", work, branch), c)
	}

	if err := gitTo(repoPath, c, "add", "-A"); err != nil {
		return fail("could not stage changes", c)
	}

	// --quiet exits 0 when there is nothing staged. Nothing staged is not the
	// same as nothing done: the command may have committed its own work.
	if !gitOK(repoPath, "diff", "--cached", "--quiet") {
		if err := gitTo(repoPath, c, "commit", "-q", "-m", a.cfg.message); err != nil {
			return fail("could not commit", c)
		}
	}

	// base is the fork point the branch was cut from, so this counts exactly the
	// commits this run is responsible for -- whoever made them. Asking the branch
	// rather than the index is what keeps a command's own commit from being read
	// as a no-op and then deleted by the deferred cleanup.
	//
	// A command that commits and then reverts leaves the branch ahead with an
	// empty diff, and will open an empty PR. That is visible and harmless; a
	// silently deleted commit is neither.
	ahead, ok := branchAhead(repoPath, p.base, branch)
	if !ok {
		return fail(fmt.Sprintf("could not compare '%s' to %s", branch, p.base), c)
	}
	if !ahead {
		return skip("command made no changes")
	}

	if err := gitTo(repoPath, c, "push", "-u", "origin", branch, "--quiet"); err != nil {
		return fail(fmt.Sprintf("unable to push to origin/%s", branch), c)
	}

	return nil
}

// processRepo runs one repo to a conclusion: the pre-flight filters, the
// user's command, the commit and push, then the pull request.
//
// The result is named because cleanup is deferred and has to see it: a repo
// that failed is left exactly as it broke.
func (a *app) processRepo(repoPath string, c *capture) (res outcome) {
	p, res := preflight(repoPath, a.cfg.branch, c)
	if res != nil {
		return res
	}

	if err := gitTo(repoPath, c, "checkout", "-b", a.cfg.branch, p.base, "--quiet"); err != nil {
		return fail("could not create branch", c)
	}
	// The branch exists from here on, so the repo needs restoring however we leave.
	defer func() { a.cleanup(res, repoPath, p.startBranch, c) }()

	if err := a.runCommand(repoPath, c); err != nil {
		return fail(err.Error(), c)
	}

	// Assigning through the named result rather than shadowing it: the deferred
	// cleanup reads res, and a shadowed copy would be easy to mistake for
	// something it never sees.
	if res = a.commitAndPush(repoPath, p, c); res != nil {
		return res
	}

	return a.openPR(repoPath, p.defaultBranch, c)
}

// resolvePath mirrors realpath: absolute, with symlinks resolved. This matters
// on macOS, where temp dirs live under a symlinked /var.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// exitCode extracts a shell-style status from a failed exec.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 127 // not found / could not start, as the shell would report it
}
