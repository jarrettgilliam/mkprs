package mkprs

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/pflag"
)

// Exit codes
const (
	exitOK      = 0 // every repository succeeded or was skipped, or --help was used
	exitUsage   = 1 // usage: bad arguments, an unusable target, or more repos than --max-repos -- no repository was touched
	exitFailure = 2 // the run happened and at least one repository failed, with or without -s
)

// app is one run's wiring: what to do, where to write, how to open PRs. It
// exists so that nothing below Run reaches for os.Stdout, os.Stderr, or the gh
// binary directly.
type app struct {
	cfg    config
	out    io.Writer
	errOut io.Writer
	prs    prOpener
}

// Run is the whole program: parse, discover, process every repo, report. It
// returns the exit code rather than calling os.Exit, so that main is the only
// place that can end the process.
func Run(args []string, stdout, stderr io.Writer) int {
	cfg, fs, err := parseArgs(args)

	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			printUsage(stdout, fs)
			return exitOK
		}

		fmt.Fprintf(stderr, "Error: %v\n", err)
		printUsage(stderr, fs)
		return exitUsage
	}

	a := &app{cfg: cfg, out: stdout, errOut: stderr, prs: ghCLI{}}

	return a.run()
}

func (a *app) run() int {
	if err := validateBranchName(a.cfg.branch); err != nil {
		fmt.Fprintf(a.errOut, "Error: %v\n", err)
		return exitUsage
	}

	repos, err := a.collectRepos()
	if err != nil {
		fmt.Fprintf(a.errOut, "Error: %v\n", err)
		return exitUsage
	}

	if len(repos) == 0 {
		fmt.Fprintln(a.errOut, "No target repositories found.")
		return exitOK
	}

	if err := a.checkRepoCount(len(repos)); err != nil {
		fmt.Fprintf(a.errOut, "Error: %v\n", err)
		return exitUsage
	}

	return a.processAll(repos)
}

// collectRepos expands every target into the deduplicated list of repos to
// process, accounting for whatever it discards along the way. A target that
// cannot be interpreted stops the run before the first repo is touched.
func (a *app) collectRepos() ([]string, error) {
	var repos []string
	var barren []string
	for _, dir := range a.cfg.targetDirs {
		before := len(repos)

		var err error
		if repos, err = discoverRepos(dir, repos); err != nil {
			return nil, err
		}

		// A target that holds no repos is ordinary -- `~/repos/*` sweeps up a
		// notes/ folder alongside them -- but counting it keeps a glob that
		// matched nothing useful from looking like a run with nothing to do.
		if len(repos) == before {
			barren = append(barren, dir)
		}
	}
	a.reportIgnored(barren, "target with no repositories", "targets with no repositories")

	// Overlapping targets are an argument mistake with unambiguous intent, so
	// this deduplicates rather than refusing -- but reports what it dropped.
	repos, dropped := dedupeRepos(repos)
	a.reportIgnored(dropped, "duplicate repository", "duplicate repositories")

	return repos, nil
}

// checkRepoCount is the guard between a mistyped target and a hundred pull
// requests in other people's repos. It runs before the first repo is touched,
// because a pull request is not something Ctrl-C takes back. The message names
// the value that proceeds, so the large run that was meant is one flag away.
func (a *app) checkRepoCount(found int) error {
	if a.cfg.maxRepos <= 0 || found <= a.cfg.maxRepos {
		return nil
	}
	return fmt.Errorf(
		"found %d %s, above the --max-repos limit of %d; re-run with --max-repos %d to proceed",
		found, plural(found, "repository", "repositories"), a.cfg.maxRepos, found)
}

// processAll runs every repo in turn, prints the closing summary, and returns
// the run's exit code.
func (a *app) processAll(repos []string) int {
	rep := newReporter(a.out, repos)

	for i, repoPath := range repos {
		name := filepath.Base(repoPath)
		r := newRepo(a.cfg, a.prs, repoPath, newCapture(name, a.cfg.verbose, a.out))

		res := r.process()
		r.output.flush()

		// Prevent panic from calling method on nil outcome
		if res == nil {
			res = r.fail("internal error: process returned no outcome")
		}
		res.report(rep, name)

		if res.failed() && a.cfg.stopOnFailure {
			if left := len(repos) - i - 1; left > 0 {
				fmt.Fprintln(a.out, "Stopped at the first failure.")
				rep.notProcessed = left
			}
			break
		}
	}

	rep.summary()

	if rep.failed > 0 {
		return exitFailure
	}

	return exitOK
}

// reportIgnored accounts for arguments the run discarded. Naming each of them
// is noise on a run where forty repos sit beside three strays, so the count is
// always printed and the paths only under --verbose.
func (a *app) reportIgnored(paths []string, one, many string) {
	n := len(paths)
	if n == 0 {
		return
	}

	fmt.Fprintf(a.errOut, "Ignored %d %s.\n", n, plural(n, one, many))
	if a.cfg.verbose {
		for _, path := range paths {
			fmt.Fprintf(a.errOut, "    %s\n", path)
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
