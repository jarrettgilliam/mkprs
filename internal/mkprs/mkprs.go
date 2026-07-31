package mkprs

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
)

// Exit codes. A run that merely had failing repos still exits 0: the per-repo
// result lines and the closing summary carry that information, and scripts that
// drive mkprs over many repos should not have to treat "one repo's command
// failed" as a fatal error.
const (
	exitOK    = 0
	exitUsage = 1
)

// app is one run's wiring: what to do, where to write, how to open PRs. It
// exists so that nothing below Run reaches for os.Stdout, os.Stderr, or the gh
// binary directly, which is what makes the package testable in-process.
type app struct {
	cfg  *config
	out  io.Writer
	errw io.Writer
	prs  prOpener
}

// Run is the whole program: parse, discover, process every repo, report.
//
// It returns the process exit code rather than calling os.Exit, so that main is
// the only place that can end the process.
func Run(args []string, stdout, stderr io.Writer) int {
	cfg, fs, err := parseArgs(args)
	if err != nil {
		// Help is a successful request for usage, not a failure, so it goes to
		// stdout and exits 0. Everything else is a bad invocation.
		if errors.Is(err, pflag.ErrHelp) {
			printUsage(stdout, fs)
			return exitOK
		}

		fmt.Fprintf(stderr, "Error: %v\n", err)
		printUsage(stderr, fs)
		return exitUsage
	}

	a := &app{cfg: cfg, out: stdout, errw: stderr, prs: ghCLI{}}
	return a.run()
}

func (a *app) run() int {
	if a.cfg.message == "" {
		a.cfg.message = strings.Join(a.cfg.command, " ")
	}
	if a.cfg.title == "" {
		a.cfg.title = firstLine(a.cfg.message)
	}

	var repos []string
	for _, dir := range a.cfg.targetDirs {
		repos = discoverRepos(dir, repos, a.errw)
	}

	if len(repos) == 0 {
		fmt.Fprintln(a.errw, "No target repositories found.")
		return exitOK
	}

	rep := newReporter(a.out, repos)

	for _, repoPath := range repos {
		name := filepath.Base(repoPath)
		c := newCapture(name, a.cfg.verbose, a.out)

		res := a.processRepo(repoPath, c)
		c.flush()

		// Unreachable: every path through processRepo ends in a constructor.
		// But `return nil` would compile, and calling a method on a nil
		// interface panics -- a programmer error should not take down a run
		// that is 20 repos deep. Substituting a failure rather than skipping
		// keeps the repo in the output and the summary adding up.
		if res == nil {
			res = fail("internal error: processRepo returned no outcome", c)
		}
		res.report(rep, name)
	}

	rep.summary()
	return exitOK
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
