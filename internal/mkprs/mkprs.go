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

// app is one run's wiring: what to do, where to write, how to open PRs.
//
// It exists so that nothing below Run reaches for os.Stdout, os.Stderr, or the
// gh binary directly, which is what makes the package testable in-process.
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
	lg, err := newLogger(a.cfg.logDir, a.errw)
	if err != nil {
		fmt.Fprintf(a.errw, "Error: %v\n", err)
		return exitUsage
	}
	defer lg.close()

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

	width := nameWidth(repos)
	var processed, failed, skipped int

	for _, repoPath := range repos {
		name := filepath.Base(repoPath)
		c := newCapture(name, a.cfg.verbose, a.out)

		r := a.processRepo(repoPath, c)
		c.flush()

		switch o := r.outcome.(type) {
		case outcomeSuccess:
			resultOK(a.out, width, name, o.prURL)
			processed++
		case outcomeSkipped:
			resultSkip(a.out, width, name, o.reason)
			skipped++
		case outcomeFailed:
			resultFail(a.out, width, name, o.reason, c)
			failed++
		default:
			// Unreachable: attempt returns one of the three above. Report it
			// rather than panic -- a programmer error should not take down a
			// run that is 20 repos deep.
			resultFail(a.out, width, name, fmt.Sprintf("internal error: unhandled outcome %T", o), c)
			failed++
		}

		lg.record(a.cfg, r, c)
	}

	printSummary(a.out, processed, failed, skipped)
	return exitOK
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
