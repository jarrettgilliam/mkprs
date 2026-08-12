package mkprs

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/pflag"
)

// config is everything the command line can set.
type config struct {
	targetDirs    []string
	branch        string
	message       string
	title         string
	body          string
	reviewers     string
	draft         bool
	keepBranch    bool
	verbose       bool
	stopOnFailure bool
	maxRepos      int
	command       []string
}

// defaultMaxRepos is a safety net, so it ships on: nobody passes --max-repos on
// the run where the target turns out to be wrong. Set well above an ordinary
// run, so the guard stays invisible until something is genuinely wrong.
const defaultMaxRepos = 50

// checkFlagValues rejects a flag that has swallowed the `--` separator. pflag
// takes whatever follows a flag as its value, so `mkprs tgt -b -- true` sets
// the branch to "--", leaves the command empty, and reports "no command
// specified" -- a message about the far end of the line from the mistake.
//
// The test is equality, not a `--` prefix: only `--` exactly can be the
// separator, and a value that merely begins with one is a value.
func checkFlagValues(fs *pflag.FlagSet) error {
	var err error
	// Visit covers only the flags actually set, in the order they were given,
	// so with more than one offender the message is at least deterministic.
	fs.Visit(func(f *pflag.Flag) {
		if err != nil || f.Value.Type() == "bool" {
			return
		}
		if f.Value.String() == "--" {
			err = fmt.Errorf(`-%s/--%s needs an argument: "--" is the command separator, not a value`, f.Shorthand, f.Name)
		}
	})
	return err
}

// parseArgs builds the config from argv, returning the flag set alongside it so
// the caller can render usage. Problems come back as errors -- deciding which
// stream to print on, and whether to exit, is the command's job. -h/--help
// comes back as pflag.ErrHelp.
func parseArgs(args []string) (*config, *pflag.FlagSet, error) {
	cfg := &config{}

	fs := pflag.NewFlagSet("mkprs", pflag.ContinueOnError)
	fs.SortFlags = false
	// ContinueOnError still prints the error and usage on its own way out;
	// discarding that leaves parseArgs a pure function of its arguments.
	fs.SetOutput(io.Discard)

	fs.StringVarP(&cfg.branch, "branch", "b", "", "Branch `name` to create in each repo (required)")
	fs.StringVarP(&cfg.message, "message", "m", "", "Commit `msg` (default: the command text)")
	fs.StringVarP(&cfg.title, "title", "t", "", "PR `title` (default: first line of commit message)")
	fs.StringVarP(&cfg.body, "body", "B", "", "PR `body` description (default: empty)")
	fs.StringVarP(&cfg.reviewers, "reviewer", "r", "", "Comma-separated `users` or org/team handles to request review from")
	fs.BoolVarP(&cfg.draft, "draft", "d", false, "Open the pull requests as drafts")
	fs.BoolVarP(&cfg.keepBranch, "keep-branch", "k", false, "Leave each repo checked out on the branch instead of deleting it")
	fs.BoolVarP(&cfg.verbose, "verbose", "v", false, "Stream command output live, prefixed by repo name")
	fs.BoolVarP(&cfg.stopOnFailure, "stop-on-failure", "s", false, "Stop the run at the first repository that fails")
	fs.IntVar(&cfg.maxRepos, "max-repos", defaultMaxRepos, "Refuse to run against more than `n` repositories (0 disables)")
	help := fs.BoolP("help", "h", false, "Show this help message")

	if err := fs.Parse(args); err != nil {
		return nil, fs, err
	}

	if *help {
		return nil, fs, pflag.ErrHelp
	}

	if err := checkFlagValues(fs); err != nil {
		return nil, fs, err
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
