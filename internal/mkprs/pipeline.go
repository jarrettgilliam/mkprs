package mkprs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// fail ends this repo, replaying its output under the reason.
func (r *repo) fail(reason string) outcome { return failure(reason, r.output) }

func (r *repo) openPR(base string) outcome {
	url, err := r.prs.open(r.path, pullRequest{
		Base:      base,
		Head:      r.cfg.branch,
		Title:     r.cfg.title,
		Body:      r.cfg.body,
		Reviewers: r.cfg.reviewers,
		Draft:     r.cfg.draft,
	}, r.output)
	if err != nil {
		return r.fail(err.Error())
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
// reads the names the rest of process needs. A non-nil outcome means stop;
// prep is meaningless in that case.
func (r *repo) preflight() (prep, outcome) {
	branch := r.cfg.branch

	if ok, note := r.isGitHubRepo(); !ok {
		return prep{}, skip(note)
	}

	clean, err := r.isCleanTree()
	if err != nil {
		return prep{}, r.fail(fmt.Sprintf("could not read the working tree status: %v", err))
	}
	if !clean {
		return prep{}, r.fail("working tree not clean")
	}

	// Fetch before any decision that reads a ref. --prune clears
	// refs/remotes/origin/<branch> after a PR is merged and its branch deleted
	// upstream; checking first would skip the repo on a ref that is only stale.
	if err := git(r.path, fetch()).to(r.log(), r.log()).run(); err != nil {
		return prep{}, r.fail("could not fetch origin")
	}

	defaultBranch, ok := r.defaultBranch()
	if !ok {
		return prep{}, r.fail("no default branch on origin; set it with 'git remote set-head origin -a'")
	}

	startBranch, ok := r.headBranch()
	if !ok {
		return prep{}, r.fail("not on a branch (detached HEAD)")
	}

	// A name is all mkprs has here: the branch may be a previous run's PR, a
	// colleague's, or a different command's, and nothing local tells them
	// apart. So this is a guess that the work is still wanted, not a
	// determination that it is not -- which is what --update is meant to settle.
	if where := r.branchLocation(branch); where != "" {
		return prep{}, r.fail(fmt.Sprintf("branch '%s' already exists %s", branch, where))
	}

	return prep{
		defaultBranch: defaultBranch,
		startBranch:   startBranch,
		base:          r.resolveBase(defaultBranch),
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
// stay put" -- see repo.restore for why that is all or nothing.
func (r *repo) cleanup(res outcome, startBranch string) {
	if r.cfg.keepBranch {
		return
	}
	// The nil check is not redundant: every path that reaches the deferred
	// cleanup assigns a result, but a future one that does not would panic
	// inside a defer rather than skip a cleanup.
	if res != nil && res.failed() {
		return
	}
	r.restore(startBranch, r.cfg.branch)
}

// runCommand runs the user's command in the repo, with both streams going to
// the capture.
//
// stdin is deliberately left unset -- the command runs against /dev/null, so it
// cannot consume input meant for mkprs itself.
func (r *repo) runCommand() error {
	abs := resolvePath(r.path)
	expanded := expandCommand(r.cfg.command, abs)

	cmd := exec.Command(expanded[0], expanded[1:]...)
	cmd.Dir = r.path
	cmd.Env = append(os.Environ(), "REPO="+abs, "REPO_NAME="+filepath.Base(r.path))
	cmd.Stdout = r.output
	cmd.Stderr = r.output

	if err := cmd.Run(); err != nil {
		return commandError(err)
	}
	return nil
}

// commitAndPush turns whatever the command left behind into a branch on origin,
// ready for a pull request. A nil outcome means it did; anything else ends the
// repo here. It returns an outcome rather than an error because "the command
// changed nothing" is a skip, not a failure.
func (r *repo) commitAndPush(p prep) outcome {
	branch := r.cfg.branch

	work, ok := r.headBranch()
	if !ok {
		return r.fail("command left the repo with a detached HEAD")
	}
	if work != branch {
		return r.fail(fmt.Sprintf("command left the repo on '%s', not '%s'", work, branch))
	}

	if err := git(r.path, stageAll()).to(r.log(), r.log()).run(); err != nil {
		return r.fail("could not stage changes")
	}

	// --quiet exits 0 when there is nothing staged. Nothing staged is not the
	// same as nothing done: the command may have committed its own work.
	if !git(r.path, nothingStaged()).ok() {
		if err := git(r.path, commit(r.cfg.message)).to(r.log(), r.log()).run(); err != nil {
			return r.fail("could not commit")
		}
	}

	// Asking the branch rather than the index keeps a command's own commit from
	// being read as a no-op and then deleted by the deferred cleanup. A command
	// that commits and then reverts opens an empty PR, which is visible and
	// harmless; a silently deleted commit is neither.
	ahead, err := r.branchAhead(p.base, branch)
	if err != nil {
		return r.fail(fmt.Sprintf("could not compare '%s' to %s: %v", branch, p.base, err))
	}
	if !ahead {
		return skip("command made no changes")
	}

	if err := git(r.path, push(branch)).to(r.log(), r.log()).run(); err != nil {
		return r.fail(fmt.Sprintf("unable to push to origin/%s", branch))
	}

	return nil
}

// process runs one repo to a conclusion: the pre-flight filters, the user's
// command, the commit and push, then the pull request. The result is named
// because the deferred cleanup has to see it.
func (r *repo) process() (res outcome) {
	p, res := r.preflight()
	if res != nil {
		return res
	}

	if err := git(r.path, createBranch(r.cfg.branch, p.base)).to(r.log(), r.log()).run(); err != nil {
		return r.fail("could not create branch")
	}
	// The branch exists from here on, so the repo needs restoring however we leave.
	defer func() { r.cleanup(res, p.startBranch) }()

	if err := r.runCommand(); err != nil {
		return r.fail(err.Error())
	}

	// Assigned through the named result, not a shadowing one: the deferred
	// cleanup reads res.
	if res = r.commitAndPush(p); res != nil {
		return res
	}

	return r.openPR(p.defaultBranch)
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

// commandError turns a failed exec into the line that fails the repo. Only an
// *exec.ExitError means the command ran: everything else -- a binary that is not
// on PATH, a cmd.Dir that does not exist, a fork that failed -- happens before
// there is a process, so there is no status to report and the error text is the
// only account of it.
func commandError(err error) error {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return fmt.Errorf("could not run command: %w", err)
	}
	if code := ee.ExitCode(); code >= 0 {
		return fmt.Errorf("command exited %d", code)
	}
	// ExitCode is -1 for a signalled process. ProcessState names the signal
	// ("signal: killed") without a syscall import that will not build on
	// Windows, where this branch is unreachable anyway.
	return fmt.Errorf("command was killed (%s)", ee.ProcessState)
}
