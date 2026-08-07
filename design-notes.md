# mkprs — design notes

The non-obvious rules the code keeps. Mostly what mkprs deliberately does *not*
do, so a settled question is not reopened.

Standing truths, not a changelog. These drive the backlog, which drives the code
— completing a feature is not a reason to edit this file. A truth changing is.

## Discovery finds only outermost, non-linked repos

Repos that contain a `.git` *folder*. Submodules and worktrees are deliberately
excluded — `dedupeRepos` keys on path, so a worktree of a repo already found
would not dedupe: two branches, two PRs, one GitHub repo. Repos nested inside the
outermost repo are skipped, but naming one directly still works.

## PRs always target the repo's own default branch

No `--pr-base`. Base and fork point must agree for the diff to be exactly the
commit the run made.

## The command must leave the repo on mkprs's branch

Committing acts on HEAD, so a `git checkout` in the command commits to a branch
mkprs does not own — or pushes to `main`. Any switch, and a detached HEAD, fails
the repo.

## mkprs never moves a branch to a commit it did not create

No `pull`, `merge`, `rebase` or `reset` in `internal/mkprs`. A stale local `main`
is read around, never updated. Repositioning a ref destroys silently and
unattended across forty repos, so it is off the table — which is why `--update`
tests equality, not ancestry.

## A failure is a repo you will have to run again for

That is the whole distinction between the three outcomes. **Success** is a pull
request. A repo is **skipped** only when mkprs determined there was nothing to
do — the target is not a GitHub repo, or the command ran and changed nothing.
Skip is earned by that determination; it is not the fallback for any condition
mkprs can explain. Not being able to tell whether there was anything to do is
itself a failure.

So everything else that stops a repo is a **failure**, including the ones that
touch nothing: an unreachable origin, a dirty tree, a detached HEAD, no
discoverable default branch, a branch already present. None of those mean the
work is not still wanted.

`✅` and `⏭️` therefore both mean the repo needs nothing further, and `❌` means
come back once something is fixed — so a failure's reason line says what would
unblock it. The exit code restates this rather than deciding it, and
`--stop-on-failure` stops for it: a wrong command and a wrong starting state
both cost a second run, and neither is worth thirty more repos to discover.

## A failed repo is not cleaned up at all

Cleanup is all or nothing: checking out the default branch drags the command's
uncommitted edits along with it. So failures leave branch, commits and working
tree as they are; skips still clean up. Same reason there is nothing between `-k`
and full cleanup. This is about failures after the branch is cut — the ones that
stop a repo before it has anything to restore never reach cleanup at all.

## Targets name repos, not directories within them

A target resolves to the repo containing it, and a subfolder target stops the
run. Per-directory work would make the directory the unit of work instead of the
repo — the spine of `outcome`, reporting and commits. `-- bash -c 'cd src && ...'`
costs nothing. Revisit only with a repo where that is painful.

## No "I did manual work, just open the PR" mode

Easily done with `gh pr create` in a loop, bypasses most of what `mkprs` does,
pushes code it did not create, and would add a lot of complexity for little gain.

## Tests use real `git`, not a faked one

No `gitRunner` interface. `git` is not a boundary but where mkprs's behavior
lives, and a fake written from the same understanding as the code passes on the
same wrong understanding. `prOpener` earns its seam because `gh` needs network,
credentials, and eventually multiple implementations.

## stdout is the report; stderr is everything else

stdout carries what mkprs concluded: the `✅`/`⏭️`/`❌` lines, the summary, and
`--help` when it was asked for. Errors, discarded-target notices, command capture,
 and all of `--verbose` go to stderr.

## The capture holds the user's workflow, not mkprs's bookkeeping

mkprs automates a manual routine — fetch, branch, run the command, stage, commit,
push, open the PR — and what those emit is what the user would have seen doing it
by hand, so it is captured and replayed when one of them fails a repo. What mkprs
runs to *decide* what to do stays out: the user never typed it, and its failures
are answers rather than errors. The rule is intent, not a list, so a step added
later classifies itself. `--verbose` is where both appear, since asking to watch
mkprs work means watching the bookkeeping too.

## Execution stays serial

No `-j/--jobs`. Serial is what lets a failure replay its capture as one
contiguous block, keeps result lines one-per-line without a mutex, and makes
`--verbose` readable. Most batch commands are fast enough that the wall-clock
saving does not pay for the complexity. Revisit only with evidence of a genuinely
slow run.
