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

// processRepo runs one repo to a conclusion: the pre-flight filters, the
// user's command, the commit and push, then the pull request.
//
// The result is named because cleanup is deferred and has to see it: a repo
// that failed is left exactly as it broke.
func (a *app) processRepo(repoPath string, c *capture) (res outcome) {
	cfg := a.cfg
	repoName := filepath.Base(repoPath)

	if ok, note := isGitHubRepo(repoPath); !ok {
		return skip(note)
	}

	if !isCleanTree(repoPath) {
		return skip("working tree not clean")
	}
	// Fetch before any decision that reads a ref. --prune here is what clears
	// refs/remotes/origin/<branch> after a PR is merged and its branch deleted
	// upstream; checking first would skip the repo on a ref that is only stale.
	fetchOrigin(repoPath, repoName, c)

	defaultBranch, ok := getDefaultBranch(repoPath)
	if !ok {
		return skip("could not determine default branch")
	}

	// Whatever is checked out now is where the repo is put back, so it has to be
	// recorded before the branch is cut. It is bookkeeping and nothing more: the
	// working branch comes from resolveBase below, so the starting branch never
	// contributes to what this run produces. A detached HEAD has no name to
	// record, which is the one state worth skipping over.
	head, ok := headBranch(repoPath)
	if !ok {
		return skip("not on a branch (detached HEAD)")
	}

	if where := branchLocation(repoPath, cfg.branch); where != "" {
		return skip(fmt.Sprintf("branch '%s' already exists %s", cfg.branch, where))
	}

	base := resolveBase(repoPath, defaultBranch)
	abs := resolvePath(repoPath)

	// Substitute {} with the repo path, leaving every other argument untouched.
	expanded := make([]string, 0, len(cfg.command))
	for _, arg := range cfg.command {
		if arg == "{}" {
			expanded = append(expanded, abs)
		} else {
			expanded = append(expanded, arg)
		}
	}

	if err := gitTo(repoPath, c, "checkout", "-b", cfg.branch, base, "--quiet"); err != nil {
		return fail("could not create branch", c)
	}
	// The branch exists from here on, so the repo needs restoring however we
	// leave -- including the success path, where this runs after the PR is
	// opened. Two cases opt out, and both mean "leave it alone" rather than
	// "delete the branch but stay put": cleanup is all or nothing, because
	// checking out head drags any uncommitted edits along with it.
	//
	// A failure is left exactly as it broke, so nothing is lost to a problem the
	// user has not seen yet: a failed push means origin has no copy of the
	// commit, and a command that failed halfway leaves edits worth reading. The
	// repo sitting on mkprs's branch is also the signal that it needs attention.
	defer func() {
		if cfg.keepBranch {
			return
		}
		if _, broke := res.(outcomeFailed); broke {
			return
		}
		restoreRepo(repoPath, head, cfg.branch, c)
	}()

	cmd := exec.Command(expanded[0], expanded[1:]...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "REPO="+abs, "REPO_NAME="+repoName)
	cmd.Stdout = c
	cmd.Stderr = c
	if err := cmd.Run(); err != nil {
		return fail(fmt.Sprintf("command exited %d", exitCode(err)), c)
	}

	// mkprs cut this branch and everything below assumes it: staging and
	// committing act on whatever is checked out, so a command that wandered off
	// would commit to a branch mkprs does not own. A branch the command created
	// survives -- cleanup only ever deletes mkprs's own.
	work, ok := headBranch(repoPath)
	if !ok {
		return fail("command left the repo with a detached HEAD", c)
	}
	if work != cfg.branch {
		return fail(fmt.Sprintf("command left the repo on '%s', not '%s'", work, cfg.branch), c)
	}

	if err := gitTo(repoPath, c, "add", "-A"); err != nil {
		return fail("could not stage changes", c)
	}

	// --quiet exits 0 when there is nothing staged. Nothing staged is not the
	// same as nothing done: the command may have committed its own work.
	if !gitOK(repoPath, "diff", "--cached", "--quiet") {
		if err := gitTo(repoPath, c, "commit", "-q", "-m", cfg.message); err != nil {
			return fail("could not commit", c)
		}
	}

	// base is the fork point the branch was cut from, so this counts exactly the
	// commits this run is responsible for -- whoever made them. Asking the branch
	// rather than the index is what keeps a command's own commit from being read
	// as a no-op and then deleted by the deferred restoreRepo.
	//
	// A command that commits and then reverts leaves the branch ahead with an
	// empty diff, and will open an empty PR. That is visible and harmless; a
	// silently deleted commit is neither.
	ahead, ok := branchAhead(repoPath, base, cfg.branch)
	if !ok {
		return fail(fmt.Sprintf("could not compare '%s' to %s", cfg.branch, base), c)
	}
	if !ahead {
		return skip("command made no changes")
	}

	if err := gitTo(repoPath, c, "push", "-u", "origin", cfg.branch, "--quiet"); err != nil {
		return fail(fmt.Sprintf("unable to push to origin/%s", cfg.branch), c)
	}

	return a.openPR(repoPath, defaultBranch, c)
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
