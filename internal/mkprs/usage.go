package mkprs

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"
)

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

The --max-repos limit is checked before any repo is touched, so a mistyped target
costs nothing; the message says which value proceeds.

Branches are cut from, and PRs opened against, each repo's own default branch,
regardless of which branch the repo happens to be on. A repo is skipped only
when there is nothing to do: origin is not a GitHub remote, or the command
leaves no changes behind. Anything else that stops a repo fails it, because the
work is still wanted there -- a dirty working tree, a detached HEAD, a branch
that already exists, an unreachable origin -- and the reason line says what to
fix before running again. The command must leave the repo on the branch mkprs
created -- one that switches branches fails that repo, leaving its work in place.

A repo that succeeds or is skipped is returned to the branch it started on and
mkprs's branch is deleted; -k opts out. A repo that fails is never cleaned up:
its branch, commits and any uncommitted edits stay exactly as the failure left
them, so nothing is lost and the repo itself shows which ones need attention.
The run carries on after a failed repo unless -s is given, which leaves the
remaining repos untouched.

Exit codes:
  0  every repository succeeded or was skipped, or --help was passed
  1  usage: bad arguments, an unusable target, or more repos than --max-repos --
     no repository was touched
  2  the run happened and at least one repository failed, with or without -s

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

func printUsage(out io.Writer, flags *pflag.FlagSet) {
	fmt.Fprint(out, usageHead)
	fmt.Fprint(out, flags.FlagUsages())
	fmt.Fprint(out, usageTail)
}
