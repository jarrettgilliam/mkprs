package mkprs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// outcome is the closed set of ways a repo can end up. A skip is a normal
// result (dirty tree, nothing to do), not an error, hence three states rather
// than an error and its absence.
//
// Callers read the concrete variants through a type switch, so note has no
// callers of its own: it is here to seal the interface, since an unexported
// method cannot be implemented outside this package. Each constructor requires
// the data its variant carries, so a skip without a reason cannot be built.
type outcome interface {
	note() string // the PR URL on success, the reason otherwise
}

type outcomeSuccess struct{ prURL string }
type outcomeSkipped struct{ reason string }
type outcomeFailed struct{ reason string }

func success(prURL string) outcome { return outcomeSuccess{prURL: prURL} }
func skip(reason string) outcome   { return outcomeSkipped{reason: reason} }
func fail(reason string) outcome   { return outcomeFailed{reason: reason} }

func (o outcomeSuccess) note() string { return o.prURL }
func (o outcomeSkipped) note() string { return o.reason }
func (o outcomeFailed) note() string  { return o.reason }

// openPR is the last step, and can end only two ways. base is always the repo's
// own default branch; head is the branch the run actually ended on, which is
// mkprs's own unless the command checked out one of its own.
func (a *app) openPR(repoPath, base, head string, c *capture) outcome {
	url, err := a.prs.open(repoPath, pullRequest{
		Base:     base,
		Head:     head,
		Title:    a.cfg.title,
		Body:     a.cfg.body,
		Reviewer: a.cfg.reviewer,
	}, c)
	if err != nil {
		return fail(err.Error())
	}
	return success(url)
}

// processRepo runs one repo to a conclusion: the pre-flight filters, the
// user's command, the commit and push, then the pull request.
func (a *app) processRepo(repoPath string, c *capture) outcome {
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

	// mkprs cuts its branch from the default branch and checks the default branch
	// back out when it is done, so it only runs on a repo that started there --
	// otherwise the cleanup would move the user off their own branch.
	head, ok := headBranch(repoPath)
	if !ok {
		return skip("not on a branch (detached HEAD)")
	}
	if head != defaultBranch {
		return skip(fmt.Sprintf("not on the default branch (on '%s', want '%s')", head, defaultBranch))
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
		return fail("could not create branch")
	}
	// The branch exists from here on, so restore the repo however we leave --
	// including the success path, where this runs after the PR is opened.
	defer restoreRepo(repoPath, defaultBranch, cfg.branch, c)

	cmd := exec.Command(expanded[0], expanded[1:]...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "REPO="+abs, "REPO_NAME="+repoName)
	cmd.Stdout = c
	cmd.Stderr = c
	if err := cmd.Run(); err != nil {
		return fail(fmt.Sprintf("command exited %d", exitCode(err)))
	}

	// The command may have moved HEAD. `git checkout -b per-repo-name`, or even a
	// bare `git checkout my-feature` over work done by hand earlier, can be the
	// whole point of the run -- so follow it rather than staging blindly onto
	// whatever happens to be checked out.
	work, ok := headBranch(repoPath)
	if !ok {
		return fail("command left the repo with a detached HEAD")
	}
	if work == defaultBranch {
		return fail(fmt.Sprintf("command left the repo on '%s'; a PR needs a branch to open from", defaultBranch))
	}
	if work != cfg.branch {
		fmt.Fprintf(c, "following branch '%s' left by the command\n", work)
	}

	if err := gitTo(repoPath, c, "add", "-A"); err != nil {
		return fail("could not stage changes")
	}

	// --quiet exits 0 when there is nothing staged. Nothing staged is not the
	// same as nothing done: the command may have committed its own work.
	if !gitOK(repoPath, "diff", "--cached", "--quiet") {
		if err := gitTo(repoPath, c, "commit", "-q", "-m", cfg.message); err != nil {
			return fail("could not commit")
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
	ahead, ok := branchAhead(repoPath, base, work)
	if !ok {
		return fail(fmt.Sprintf("could not compare '%s' to %s", work, base))
	}
	if !ahead {
		return skip("command made no changes")
	}

	if err := gitTo(repoPath, c, "push", "-u", "origin", work, "--quiet"); err != nil {
		return fail(fmt.Sprintf("unable to push to origin/%s", work))
	}

	return a.openPR(repoPath, defaultBranch, work, c)
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
