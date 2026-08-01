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
	reviewers  string
	draft      bool
	keepBranch bool
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
The command runs from the repository root, executed directly rather than through
a shell -- use ` + "`-- bash -c '...'`" + ` for globs, pipes or redirection. An argument
that is exactly {} becomes the repo's absolute path, also available as $REPO
along with $REPO_NAME.

Branches are cut from, and PRs opened against, each repo's own default branch. A
repo is skipped when it is not on that branch, its working tree is dirty, the
branch already exists, the command leaves no changes behind, or origin is not a
GitHub remote. The command must leave the repo on the branch mkprs created -- one
that switches branches fails that repo, leaving its work in place.

A repo that succeeds or is skipped is returned to the branch it started on and
mkprs's branch is deleted; -k keeps that branch and leaves the repo on it. A repo
that fails is never cleaned up -- its branch, commits and any uncommitted edits
stay exactly as the failure left them, so nothing is lost and the repo itself
shows which ones need attention.

Examples:
  # Bump NuGet dependencies everywhere
  mkprs ~/repos -b bump-deps -- dotnet outdated -u

  # Fix a typo, with an explicit commit message
  mkprs ~/repos -b fix-typo -m "Fix typo in README" -- sed -i '' 's/teh/the/g' README.md

  # Apply a patch file -- give an absolute path, the command runs in the repo
  mkprs ~/repos -b apply-fix -- git apply /tmp/fix.patch

  # Anything needing a shell goes through bash -c
  mkprs ~/repos -b lint -- bash -c 'npm ci && npm run lint:fix'

  # A tool that insists on an explicit path
  mkprs ~/repos -b scan -- cp /example/file {}
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
	fs.StringVarP(&cfg.reviewers, "reviewer", "r", "", "Comma-separated `users` or org/team handles to request review from")
	fs.BoolVarP(&cfg.draft, "draft", "d", false, "Open the pull requests as drafts")
	fs.BoolVarP(&cfg.keepBranch, "keep-branch", "k", false, "Leave each repo checked out on the branch instead of deleting it")
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
		return nil, fs, errors.New("must specify at least one target dir")
	case cfg.branch == "":
		return nil, fs, errors.New("-b/--branch is required")
	case len(cfg.command) == 0:
		return nil, fs, errors.New("no command specified (everything after -- is the command to run)")
	}

	return cfg, fs, nil
}
