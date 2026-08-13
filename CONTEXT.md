# mkprs

Applies one change across many GitHub repositories: run a command in each, commit
what it leaves behind, and open a pull request. The rules these terms obey live
in `design-decisions.md`; this file only says what each word means.

## Language

**Target**:
A directory to search for repositories, or a repository root named directly. Never
a place inside a repository.
_Avoid_: path, source, directory

**Repository**:
The unit of work. Every branch, commit, pull request and outcome belongs to exactly
one.
_Avoid_: project, package, folder

**Run**:
One invocation of mkprs, from the first target to the closing summary.
_Avoid_: batch, sweep, job

**Command**:
What the user wants done, executed at each repository's root. Its output is the
capture; its text is the default commit message.
_Avoid_: script, task, step

**Working branch**:
The branch mkprs cuts in a repository, commits to, and pushes. The command must
leave the repository on it.
_Avoid_: feature branch, PR branch

**Outcome**:
The closed set of ways a repository can end: success, skip or failure. Nothing
else.
_Avoid_: result, status, state

**Success**:
A pull request was opened.

**Skip**:
mkprs determined definitively that there was no pull request to open. Not being
able to tell is a failure, not a skip.

**Failure**:
A repository you will have to run again for. Its reason says what would unblock
it.
_Avoid_: error, fault

**Capture**:
What the user's workflow emitted in one repository — the command, and the git and
gh steps they would have typed by hand. Replayed under a failure. What mkprs runs
to decide what to do is not part of it.
_Avoid_: log, output, transcript

**Report**:
What mkprs concluded, on stdout: one line per repository, then the summary.
Everything else is stderr.
_Avoid_: summary (that is one part of it), log
