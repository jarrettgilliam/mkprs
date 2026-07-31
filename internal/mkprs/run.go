package mkprs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// outcome is the closed set of ways a repo can end up. A skip is a normal
// result (dirty tree, nothing to do), not an error, which is why there are
// three of these rather than an error and its absence.
//
// The unexported note method seals the interface: only the three types below
// can implement it, and each constructor requires the data its variant carries,
// so there is no way to build a skip without a reason. Outcomes are returned,
// never assigned after the fact -- see attempt.
type outcome interface {
	fmt.Stringer  // "success" | "skipped" | "failed" -- the summary.tsv contract
	note() string // the PR URL on success, the reason otherwise
}

type outcomeSuccess struct{ prURL string }
type outcomeSkipped struct{ reason string }
type outcomeFailed struct{ reason string }

func success(prURL string) outcome { return outcomeSuccess{prURL: prURL} }
func skip(reason string) outcome   { return outcomeSkipped{reason: reason} }
func fail(reason string) outcome   { return outcomeFailed{reason: reason} }

func (outcomeSuccess) String() string { return "success" }
func (outcomeSkipped) String() string { return "skipped" }
func (outcomeFailed) String() string  { return "failed" }

func (o outcomeSuccess) note() string { return o.prURL }
func (o outcomeSkipped) note() string { return o.reason }
func (o outcomeFailed) note() string  { return o.reason }

// repoResult is everything worth recording about one repository.
type repoResult struct {
	path            string
	outcome         outcome
	commitSHA       string
	resolvedCommand string
}

// processRepo takes one repo from discovery through to an open pull request.
func (a *app) processRepo(repoPath string, c *capture) *repoResult {
	r := &repoResult{path: repoPath}
	r.outcome = a.attemptRunCommand(repoPath, r, c)
	return r
}

// openPR is the last step, and can end only two ways.
func (a *app) openPR(repoPath string, r *repoResult, c *capture) outcome {
	// Read the SHA while the branch still exists; cleanup deletes it after.
	r.commitSHA = shortSHA(repoPath, a.cfg.branch)

	// The base is still hardcoded to main, matching the shell version; making it
	// follow the repo's default branch is tracked separately.
	url, err := a.prs.open(repoPath, pullRequest{
		Base:     "main",
		Head:     a.cfg.branch,
		Title:    a.cfg.title,
		Body:     a.cfg.body,
		Reviewer: a.cfg.reviewer,
	}, c.raw())
	if err != nil {
		return fail(err.Error())
	}
	return success(url)
}

// attemptRunCommand runs one repo to a conclusion: the pre-flight filters, the user's
// command, the commit and push, then the pull request.
//
// The signature is the guarantee. Every path has to return an outcome -- Go
// will not compile a function that falls off the end -- so no repo can finish
// unclassified. r is here only to collect the metadata the log wants
// (resolvedCommand, commitSHA), which is legitimately empty on early exits.
func (a *app) attemptRunCommand(repoPath string, r *repoResult, c *capture) outcome {
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
	r.resolvedCommand = strings.Join(expanded, " ")

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

	if err := gitTo(repoPath, c, "add", "-A"); err != nil {
		return fail("could not stage changes")
	}

	// --quiet exits 0 when there is nothing staged.
	if gitOK(repoPath, "diff", "--cached", "--quiet") {
		return skip("command made no changes")
	}

	if err := gitTo(repoPath, c, "commit", "-q", "-m", cfg.message); err != nil {
		return fail("could not commit")
	}

	if err := gitTo(repoPath, c, "push", "-u", "origin", cfg.branch, "--quiet"); err != nil {
		return fail(fmt.Sprintf("unable to push to origin/%s", cfg.branch))
	}

	return a.openPR(repoPath, r, c)
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
