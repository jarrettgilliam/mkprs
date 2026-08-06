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
	if err := a.prepare(); err != nil {
		fmt.Fprintf(a.errw, "Error: %v\n", err)
		return exitUsage
	}

	repos, err := a.collectRepos()
	if err != nil {
		fmt.Fprintf(a.errw, "Error: %v\n", err)
		return exitUsage
	}

	if len(repos) == 0 {
		fmt.Fprintln(a.errw, "No target repositories found.")
		return exitOK
	}

	if err := a.checkRepoCount(len(repos)); err != nil {
		fmt.Fprintf(a.errw, "Error: %v\n", err)
		return exitUsage
	}

	a.processAll(repos)
	return exitOK
}

// prepare checks what can be checked without touching a repo and fills in the
// values the rest of the run assumes are set.
func (a *app) prepare() error {
	// Nothing about this varies per repo, so nothing is learned by asking each
	// one: git would refuse the name in all of them, after each had been walked
	// and fetched.
	if err := validateBranchName(a.cfg.branch); err != nil {
		return err
	}

	if a.cfg.message == "" {
		a.cfg.message = strings.Join(a.cfg.command, " ")
	}
	if a.cfg.title == "" {
		a.cfg.title = firstLine(a.cfg.message)
	}
	return nil
}

// collectRepos expands every target into the deduplicated list of repos to
// process, accounting for whatever it discards along the way.
//
// A target that cannot be interpreted stops the run here, before the first repo
// is touched -- once pull requests exist, Ctrl-C does not take them back, so
// everything checkable is checked up front.
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
		// notes/ folder and a README alongside them -- but counting it keeps a
		// glob that matched nothing useful from looking like a run with nothing
		// to do.
		if len(repos) == before {
			barren = append(barren, dir)
		}
	}
	a.reportIgnored(barren, "target with no repositories", "targets with no repositories")

	// Overlapping targets are an argument mistake with unambiguous intent, so
	// this deduplicates rather than refusing -- but says so, because silently
	// discarding an argument someone typed is dishonestly that erodes trust.
	repos, dropped := dedupeRepos(repos)
	a.reportIgnored(dropped, "duplicate repository", "duplicate repositories")

	return repos, nil
}

// checkRepoCount is the guard between a mistyped target and a hundred pull
// requests in other people's repos. It runs before the first repo is touched,
// because a pull request is not something Ctrl-C takes back.
//
// The count is the deduplicated one -- a repo reached from two targets is one
// repo, and should not spend two of the budget -- and the message is the fix,
// so the large run that was meant is one flag away and the one that was not is
// never silent.
func (a *app) checkRepoCount(found int) error {
	if a.cfg.maxRepos <= 0 || found <= a.cfg.maxRepos {
		return nil
	}
	return fmt.Errorf(
		"found %d %s, above the --max-repos limit of %d; re-run with --max-repos %d to proceed",
		found, plural(found, "repository", "repositories"), a.cfg.maxRepos, found)
}

// processAll runs every repo in turn and prints the closing summary.
func (a *app) processAll(repos []string) {
	rep := newReporter(a.out, repos)

	for i, repoPath := range repos {
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

		// -s is for the run whose command is simply wrong: the first failure is
		// the diagnosis, and the rest is noise. A skip is a normal result and
		// stops nothing.
		if _, broke := res.(outcomeFailed); broke && a.cfg.stopOnFailure {
			// Why the results end here, with the summary's "Not processed"
			// counter carrying how many repos that cost.
			if left := len(repos) - i - 1; left > 0 {
				fmt.Fprintln(a.out, "Stopped at the first failure.")
				rep.notProcessed = left
			}
			break
		}
	}

	rep.summary()
}

// reportIgnored accounts for arguments the run discarded. Discarding one
// silently is a small dishonesty, but naming each of them is noise on a run
// where forty repos sit beside three strays -- so the count is always printed
// and the paths only under --verbose, which is where "why did my glob not match
// what I expected" gets answered.
func (a *app) reportIgnored(paths []string, one, many string) {
	n := len(paths)
	if n == 0 {
		return
	}

	fmt.Fprintf(a.errw, "Ignored %d %s.\n", n, plural(n, one, many))
	if a.cfg.verbose {
		for _, path := range paths {
			fmt.Fprintf(a.errw, "    %s\n", path)
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
