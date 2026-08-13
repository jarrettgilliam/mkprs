package mkprs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// gitArgs is one git invocation's arguments. Every git command mkprs runs is
// named at the foot of this file, so a call site reads as intent rather than as
// git flags.
type gitArgs []string

// git prepares one git invocation in dir. An empty dir runs it wherever mkprs
// itself is running, which is what the questions that belong to no repository
// need.
func git(dir string, args gitArgs) *gitRun {
	return &gitRun{dir: dir, args: args}
}

type gitRun struct {
	dir            string
	args           gitArgs
	stdout, stderr io.Writer
}

// to routes the command's output. A nil out discards that stream; a nil errOut
// keeps git's complaint for the error instead, which is what an unrouted run
// gets -- see run.
func (g *gitRun) to(out, errOut io.Writer) *gitRun {
	g.stdout, g.stderr = out, errOut
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
	cmd.Dir = g.dir
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

// gitError replaces the error with git's own complaint whenever there is one:
// an *exec.ExitError reports "exit status 128" and nothing else, which tells a
// reader nothing, and the code itself is not worth carrying into a reason line.
// Redirected stderr leaves the buffer empty and the error alone -- repeating a
// complaint the capture already holds would print it twice.
func gitError(err error, stderr string) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if msg := strings.TrimSpace(stderr); msg != "" {
			return errors.New(msg)
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
	// check-ref-format reads only its argument, so there is no repository for
	// this to run in.
	if strings.HasPrefix(branch, "-") || !git("", checkFormat(branch)).ok() {
		return fmt.Errorf("invalid branch name %q (see 'git help check-ref-format')", branch)
	}
	return nil
}

// =============================================================================
// The commands
// =============================================================================

func fetch() gitArgs { return gitArgs{"fetch", "origin", "--quiet", "--prune"} }

func createBranch(name, base string) gitArgs {
	return gitArgs{"checkout", "-b", name, base, "--quiet"}
}

func checkoutBranch(name string) gitArgs { return gitArgs{"checkout", name, "--quiet"} }

func deleteBranch(name string) gitArgs { return gitArgs{"branch", "-D", name} }

func stageAll() gitArgs { return gitArgs{"add", "-A"} }

// nothingStaged succeeds when the index holds no changes: --quiet makes diff
// exit 0 for an empty diff and 1 otherwise.
func nothingStaged() gitArgs { return gitArgs{"diff", "--cached", "--quiet"} }

func commit(message string) gitArgs { return gitArgs{"commit", "-q", "-m", message} }

func push(branch string) gitArgs { return gitArgs{"push", "-u", "origin", branch, "--quiet"} }

// remoteOriginURL reads the configured URL rather than the effective one:
// `remote get-url` applies url.<base>.insteadOf rewrites, which would report a
// GitHub remote as whatever the user rewrote it to.
func remoteOriginURL() gitArgs { return gitArgs{"config", "--get", "remote.origin.url"} }

func status() gitArgs { return gitArgs{"status", "--porcelain"} }

func originHeadRef() gitArgs {
	return gitArgs{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"}
}

func currentHeadRef() gitArgs { return gitArgs{"symbolic-ref", "--short", "HEAD"} }

func localBranchExists(branch string) gitArgs { return verifyRef("refs/heads/" + branch) }

func originBranchExists(branch string) gitArgs {
	return verifyRef("refs/remotes/origin/" + branch)
}

func verifyRef(ref string) gitArgs { return gitArgs{"rev-parse", "--verify", "--quiet", ref} }

func commitsAhead(base, branch string) gitArgs {
	return gitArgs{"rev-list", "--count", base + ".." + branch}
}

func repoRoot() gitArgs { return gitArgs{"rev-parse", "--show-toplevel"} }

func checkFormat(branch string) gitArgs {
	return gitArgs{"check-ref-format", "refs/heads/" + branch}
}
