package mkprs

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/pflag"
)

// config is everything the command line can set.
type config struct {
	targetDirs []string
	branch     string
	message    string
	title      string
	body       string
	reviewer   string
	logDir     string
	verbose    bool
	command    []string
}

const usageHead = `Usage: mkprs <target-dir> [<target-dir> ...] -b <branch> [OPTIONS] -- <command> [args...]

Run a command in every repository found under the target directories, then commit
the result and open a pull request for each repo that changed.

Arguments:
  <target-dir>...      One or more directories to search for repositories
  <command> [args...]  Everything after -- is the command to run in each repo

Options:
`

const usageTail = `
Short and long options accept either a space (-b my-branch) or equals (-b=my-branch) separator.

The command:
  * runs with the current directory set to the repository root, so relative
    paths work as they would if you had cd'd into the repo yourself
  * is executed directly, not through a shell -- no globbing, pipes, or
    redirection. Use ` + "`-- bash -c '...'`" + ` when you need those.
  * has any argument that is exactly {} replaced with the repo's absolute path
  * can read $REPO (absolute path) and $REPO_NAME (basename) from the environment

A repo is skipped when its working tree is dirty, the branch already exists, the
command leaves no changes behind, or its origin remote does not point at
github.com.

Output is one line per repo: ✅ and the PR URL on success, ❌ and the reason on
failure (followed by the tail of the command's output), ⏭️ when the repo was
skipped. Command output is otherwise captured and discarded.

--log <dir> keeps that captured output instead. The directory holds one
<repo>.log per repository -- the resolved command, its full output, the outcome
-- plus a summary.tsv of one tab-separated record each:

  repo_path  status  branch  commit_sha  pr_url  notes

Absent --log nothing is written to disk.

--verbose streams that output live instead of buffering it, each line prefixed
with the repo it came from. It composes with --log: stream and write.

Examples:
  # Bump NuGet dependencies everywhere
  ./mkprs ~/repos -b bump-deps -- dotnet outdated -u

  # Fix a typo, with an explicit commit message
  ./mkprs ~/repos -b fix-typo -m "Fix typo in README" -- sed -i '' 's/teh/the/g' README.md

  # Apply a patch file
  ./mkprs ~/repos -b apply-x -- git apply /tmp/x.patch

  # Anything needing a shell goes through bash -c
  ./mkprs ~/repos -b lint -- bash -c 'npm ci && npm run lint:fix'

  # A tool that insists on an explicit path
  ./mkprs ~/repos -b scan -- some-tool --root {}
`

func printUsage(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprint(w, usageHead)
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprint(w, usageTail)
}

// parseArgs builds the config from argv, returning the flag set alongside it so
// the caller can render usage. Problems come back as errors -- deciding which
// stream to print on, and whether to exit, is the command's job.
//
// -h/--help comes back as pflag.ErrHelp. It is not a failure: the caller prints
// usage to stdout and exits 0. Every other error is a bad invocation.
func parseArgs(args []string) (*config, *pflag.FlagSet, error) {
	cfg := &config{}

	fs := pflag.NewFlagSet("mkprs", pflag.ContinueOnError)
	fs.SortFlags = false
	// ContinueOnError still prints the error and usage on its own way out.
	// Discarding that leaves parseArgs a pure function of its arguments.
	fs.SetOutput(io.Discard)

	fs.StringVarP(&cfg.branch, "branch", "b", "", "Branch `name` to create in each repo (required)")
	fs.StringVarP(&cfg.message, "message", "m", "", "Commit `msg` (default: the command text)")
	fs.StringVarP(&cfg.title, "title", "t", "", "PR `title` (default: first line of commit message)")
	fs.StringVarP(&cfg.body, "body", "B", "", "PR `body` description (default: empty)")
	fs.StringVarP(&cfg.reviewer, "reviewer", "r", "", "GitHub `user` to request review from (optional)")
	fs.StringVar(&cfg.logDir, "log", "", "Write per-repo logs and summary.tsv to `dir`")
	fs.BoolVarP(&cfg.verbose, "verbose", "v", false, "Stream command output live, prefixed by repo name")
	// pflag handles an undeclared --help itself, but only a declared one shows
	// up in the Options block. Declaring it and raising pflag's own sentinel
	// keeps both the listing and the standard signal.
	help := fs.BoolP("help", "h", false, "Show this help message")

	// pflag's own wording ("unknown flag: --bogus") is kept as-is.
	if err := fs.Parse(args); err != nil {
		return nil, fs, err
	}

	if *help {
		return nil, fs, pflag.ErrHelp
	}

	// Everything before `--` is a target dir; everything after is the command.
	// ArgsLenAtDash is -1 when no `--` was given at all.
	rest := fs.Args()
	if n := fs.ArgsLenAtDash(); n >= 0 {
		cfg.targetDirs = rest[:n]
		cfg.command = rest[n:]
	} else {
		cfg.targetDirs = rest
	}

	switch {
	case len(cfg.targetDirs) < 1:
		return nil, fs, errors.New("Must specify at least one target dir")
	case cfg.branch == "":
		return nil, fs, errors.New("-b/--branch is required")
	case len(cfg.command) == 0:
		return nil, fs, errors.New("no command specified (everything after -- is the command to run)")
	}

	return cfg, fs, nil
}
