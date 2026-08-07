package mkprs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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

// prep holds the branch names read from a repo before it is touched.
type prep struct {
	// defaultBranch is the repo's own default, which the PR targets.
	defaultBranch string
	// startBranch is whatever was checked out when the run reached this repo,
	// and where it gets put back.
	startBranch string
	// base is the fork point the working branch is cut from, and what the
	// commit count is measured against. Normally origin/<defaultBranch>.
	base string
}

// preflight runs every check that can end a repo before its command does, and
// reads the names the rest of processRepo needs. A non-nil outcome means stop;
// prep is meaningless in that case.
func preflight(repoPath, branch string, c *capture) (prep, outcome) {
	if ok, note := isGitHubRepo(repoPath); !ok {
		return prep{}, skip(note)
	}

	clean, ok := isCleanTree(repoPath)
	if !ok {
		return prep{}, fail("could not read the working tree status", c)
	}
	if !clean {
		return prep{}, fail("working tree not clean", c)
	}

	// Fetch before any decision that reads a ref. --prune clears
	// refs/remotes/origin/<branch> after a PR is merged and its branch deleted
	// upstream; checking first would skip the repo on a ref that is only stale.
	if err := gitTo(repoPath, c, "fetch", "origin", "--quiet", "--prune"); err != nil {
		return prep{}, fail("could not fetch origin", c)
	}

	defaultBranch, ok := getDefaultBranch(repoPath)
	if !ok {
		return prep{}, fail("no default branch on origin; set it with 'git remote set-head origin -a'", c)
	}

	startBranch, ok := headBranch(repoPath)
	if !ok {
		return prep{}, fail("not on a branch (detached HEAD)", c)
	}

	// A name is all mkprs has here: the branch may be a previous run's PR, a
	// colleague's, or a different command's, and nothing local tells them
	// apart. So this is a guess that the work is still wanted, not a
	// determination that it is not -- which is what --update is meant to settle.
	if where := branchLocation(repoPath, branch); where != "" {
		return prep{}, fail(fmt.Sprintf("branch '%s' already exists %s", branch, where), c)
	}

	return prep{
		defaultBranch: defaultBranch,
		startBranch:   startBranch,
		base:          resolveBase(repoPath, defaultBranch),
	}, nil
}

// expandCommand substitutes the repo's path for every argument that is exactly
// {}. The result is a new slice: the config's command is shared by every repo.
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

// cleanup returns the repo to startBranch and deletes the working branch.
// Both opt-outs mean "leave the repo alone" rather than "delete the branch but
// stay put" -- see restoreRepo for why that is all or nothing.
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
// the capture.
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
		return fmt.Errorf("command exited %d", exitCode(err))
	}
	return nil
}

// commitAndPush turns whatever the command left behind into a branch on origin,
// ready for a pull request. A nil outcome means it did; anything else ends the
// repo here. It returns an outcome rather than an error because "the command
// changed nothing" is a skip, not a failure.
func (a *app) commitAndPush(repoPath string, p prep, c *capture) outcome {
	branch := a.cfg.branch

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

	// Asking the branch rather than the index keeps a command's own commit from
	// being read as a no-op and then deleted by the deferred cleanup. A command
	// that commits and then reverts opens an empty PR, which is visible and
	// harmless; a silently deleted commit is neither.
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
// user's command, the commit and push, then the pull request. The result is
// named because the deferred cleanup has to see it.
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

	// Assigned through the named result, not a shadowing one: the deferred
	// cleanup reads res.
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

// exitCommandNotFound is what a shell reports for a command it could not start.
const exitCommandNotFound = 127

// exitCode extracts a shell-style status from a failed exec.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return exitCommandNotFound
}
