package mkprs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// outcome is where a repo ended up. The three states are deliberate: a skip is
// a normal result (dirty tree, nothing to do), not an error.
type outcome int

const (
	outcomeSuccess outcome = iota
	outcomeFailed
	outcomeSkipped
)

func (o outcome) String() string {
	switch o {
	case outcomeSuccess:
		return "success"
	case outcomeSkipped:
		return "skipped"
	default:
		return "failed"
	}
}

// repoResult is everything worth recording about one repository.
type repoResult struct {
	path            string
	outcome         outcome
	note            string
	commitSHA       string
	resolvedCommand string
}

func (r *repoResult) skip(note string) *repoResult {
	r.outcome, r.note = outcomeSkipped, note
	return r
}

func (r *repoResult) fail(note string) *repoResult {
	r.outcome, r.note = outcomeFailed, note
	return r
}

// processRepo takes one repo from discovery through to an open pull request.
func processRepo(cfg *config, repoPath string, c *capture) *repoResult {
	r := &repoResult{path: repoPath}

	if ok, note := isGitHubRepo(repoPath); !ok {
		return r.skip(note)
	}

	if done := runCommand(cfg, repoPath, r, c); done {
		return r
	}

	// Read the SHA while the branch still exists; cleanup deletes it below.
	r.commitSHA = shortSHA(repoPath, cfg.branch)

	url, err := createPR(cfg, repoPath, c)
	if err != nil {
		r.fail(err.Error())
	} else {
		r.outcome, r.note = outcomeSuccess, url
	}

	cleanupBranch(cfg, repoPath, c)
	return r
}

// runCommand runs the user's command and pushes the result. It reports true
// when the repo is finished (skipped or failed) and needs no PR.
func runCommand(cfg *config, repoPath string, r *repoResult, c *capture) bool {
	repoName := filepath.Base(repoPath)

	if !isCleanTree(repoPath) {
		r.skip("working tree not clean")
		return true
	}
	if branchExists(repoPath, cfg.branch) {
		r.skip(fmt.Sprintf("branch '%s' already exists", cfg.branch))
		return true
	}

	dflt, ok := defaultBranch(repoPath)
	if !ok {
		r.skip("could not determine default branch")
		return true
	}

	fetchOrigin(repoPath, repoName, c)
	base := resolveBase(repoPath, dflt)
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
		r.fail("")
		return true
	}

	cmd := exec.Command(expanded[0], expanded[1:]...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "REPO="+abs, "REPO_NAME="+repoName)
	cmd.Stdout = c
	cmd.Stderr = c
	if err := cmd.Run(); err != nil {
		r.fail(fmt.Sprintf("command exited %d", exitCode(err)))
		restoreRepo(repoPath, dflt, cfg.branch, c)
		return true
	}

	if err := gitTo(repoPath, c, "add", "-A"); err != nil {
		r.fail("")
		restoreRepo(repoPath, dflt, cfg.branch, c)
		return true
	}

	// --quiet exits 0 when there is nothing staged.
	if gitOK(repoPath, "diff", "--cached", "--quiet") {
		r.skip("command made no changes")
		restoreRepo(repoPath, dflt, cfg.branch, c)
		return true
	}

	if err := gitTo(repoPath, c, "commit", "-q", "-m", cfg.message); err != nil {
		r.fail("")
		restoreRepo(repoPath, dflt, cfg.branch, c)
		return true
	}

	if err := gitTo(repoPath, c, "push", "-u", "origin", cfg.branch, "--quiet"); err != nil {
		r.fail(fmt.Sprintf("unable to push to origin/%s", cfg.branch))
		restoreRepo(repoPath, dflt, cfg.branch, c)
		return true
	}

	return false
}

func cleanupBranch(cfg *config, repoPath string, c *capture) {
	dflt, ok := defaultBranch(repoPath)
	if !ok {
		dflt = "main"
	}
	restoreRepo(repoPath, dflt, cfg.branch, c)
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
