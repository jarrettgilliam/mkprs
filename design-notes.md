# mkprs — design notes

Decisions that are settled, and the reasoning behind them. Most record something
mkprs deliberately does *not* do, so that a question already worked through does
not get reopened from scratch.

Unlike `todo.md`, nothing here is deleted when work completes — these outlive the
backlog, and several will feed the README when it is written.

## Submodules and linked worktrees are both excluded from discovery

Only `.git` *directories* count; a submodule's `.git` and a `git worktree`
checkout's `.git` are each a file holding a `gitdir:` pointer, so one rule covers
both. `discoverRepos` (`internal/mkprs/discover.go`) keys off `d.IsDir()` for
exactly this reason.

Excluding worktrees is deliberate, not a side effect. A worktree is a second
checkout of a repo mkprs would reach anyway, and this tool makes one specific
change against one repo's default branch — making it twice is not meaningful.
Worse, `dedupeRepos` keys on path, so a worktree of an already-discovered repo
would not dedupe: two entries, two branches, two pull requests against the same
GitHub repo.

## A repo inside another repo is not discovered

`discoverRepos` prunes a repo's whole subtree once it finds one, so `~/Code`
yields 76 repos rather than 87 — the 11 sitting inside `CSharp/TestProjects`
collapse to the one that contains them.

Git discourages the arrangement to begin with: the inner repo has to be
`.gitignore`d by hand, and submodules exist for the deliberate case. So one found
by accident is a stray checkout far more often than something anyone wants a pull
request against, and the old behaviour meant a tree could hold many more repos
than it appeared to — which is a batch tool opening pull requests you did not
know were in scope.

**Naming the inner repo directly still works** and is not an error, so nothing is
actually unreachable. It is then the walk's own root, so nothing has pruned it.

The pruning is also the discovery speedup, and the larger half of it in practice:
without it every repo's full working tree is walked looking for more `.git`
directories — `node_modules`, `bin/obj`, `.venv`, all of it — on every run.

## No flag takes `--` as its value

pflag takes whatever follows a flag as its value, with no guard of its own, so
`mkprs tgt -b -- true` sets the branch to `--`, swallows the separator, and then
fails with "no command specified" — a message about the far end of the line from
the mistake. `checkFlagValues` (`internal/mkprs/cli.go`) rejects a non-bool flag
whose value is exactly `--`, naming the flag instead.

**Exactly `--`, not the `--` prefix.** The broader rule catches more — `-b
--draft` is a misparse too — but only `--` can be the separator, and the wider
test would reach past the mistake into the values themselves. `-m`, `-t` and `-B`
are free text, so `-m '-- and then some'` has to keep working; a guard about
argument structure has no business editing what a commit message may say. What
the narrow rule gives up is caught anyway, and earlier than it used to be:
`validateBranchName` rejects a branch named `--draft` at startup, before any repo
is walked.

The cost is that `-m --` is now unspellable, with no escape hatch: pflag records
only a flag's final value, so `-m --` and `-m=--` are indistinguishable
afterwards, and honouring the `=` form would mean parsing argv a second time
here. A commit message of exactly `--` is not worth that.

## No `-c "shell string"` mode

The command is argv after `--`, executed directly — no `eval`, no re-parsing.
When a pipe or glob is needed, write `-- bash -c '...'` explicitly so the eval
boundary is visible at the call site.

## `{}` substitution over `-I`-style configurable placeholders

`{}` matches `find` muscle memory, and since the command is always last there is
no need for find's `\;` terminator.

## CWD is the repo root

`find -execdir` semantics, not `-exec`, so relative paths in commands behave the
way they would if you had cd'd in yourself.

## PRs always target the repo's own default branch

No `--pr-base` override: the base and the branch's fork point have to agree for
the PR's diff to be exactly the commit the run made. This holds even when the
command supplies its own head branch — see the next note.

## The command must leave the repo on the branch mkprs created

Staging and committing act on whatever HEAD points at, so a command that runs
`git checkout` would have its work committed to a branch mkprs does not own — or,
if it landed on the default branch, pushed straight to `main`. Any switch, and a
detached HEAD, fails that repo — and since a failure is not cleaned up,
everything it created survives, mkprs's own branch included.

## mkprs never moves a branch to a commit it did not create

No `pull`, `merge`, `rebase` or `reset` appears anywhere in `internal/mkprs`, and
none should. The git verbs in use are `fetch`, `checkout`, `add`, `commit`,
`push`, `branch`, and the read-only ones — `rev-parse`, `rev-list`,
`symbolic-ref`, `status`, `config`, `remote`, `diff`.

`fetchOrigin` runs `fetch origin --quiet --prune` and stops there. `resolveBase`
then prefers `origin/<default>` over the local branch of the same name, so a run
cuts from what origin actually has by reading *around* a stale local `main`
rather than updating it. Every other question — `branchLocation`, `branchAhead`,
`getDefaultBranch` — is answered by reading refs.

The consequence is worth stating plainly: **a user's local `main` can be
arbitrarily far behind and it neither matters nor changes.** The only branch mkprs
moves is its own, and only by committing to it. Advancing a branch with a new
commit is not repositioning it.

Repositioning is what is off the table, because it destroys silently.
`reset --hard`, or `checkout -B` against a branch that already exists, discards
whatever the old tip pointed at with no record and no prompt — and on a branch
the user made rather than mkprs, that can be unpushed work. A tool that touches
forty repos unattended cannot make that call in any of them.

This is load-bearing rather than incidental. `--update` decides whether to touch
a repo by asking this and nothing else: it proceeds exactly when the branch it
needs is already on the commit the work starts from, or does not exist locally at
all, and skips whenever continuing would mean repositioning a ref. That is why
its checks are equality rather than ancestry — a branch merely *behind* where the
work begins holds nothing unique and looks safe to advance, but advancing it
still needs `checkout -B`, which is the first crack in the invariant. The manual
`reset --hard` in the handful of affected repos is the better price.

## A failed repo is not cleaned up at all

Cleanup is all or nothing: the checkout back to the default branch is what makes
deleting the branch safe, and on its own it is actively harmful. mkprs's branch
is cut from the default branch, so nothing conflicts and `git checkout` carries
the command's uncommitted edits across with it — they end up stranded as dirty
state on the default branch while the branch that explains them is deleted.
Measured, not assumed: a modified file and an untracked one both follow the
checkout.

So a failure leaves branch, commits and working tree exactly as they were. The
push failure is the case that proves it — origin has no copy, so deleting the
branch there is the one path that genuinely destroys work — but the rule is
uniform across every failure rather than one rule per step. The repo sitting on
mkprs's branch is also the signal that it needs attention.

Skips still clean up: "command made no changes" leaves nothing worth keeping.

## `-k` skips cleanup entirely, and there is no second flag for staying on the branch

Keeping the branch but checking out the default one was considered and dropped
for the reason above: the checkout drags uncommitted edits with it, so "keep the
branch, restore the repo" is not a coherent halfway house. Not having to
`git checkout` is most of the point anyway — the branch is on origin once the PR
is open, so `git checkout -b <branch> origin/<branch>` recovers a deleted one
nearly as cheaply as `-k` avoids it, and a flag that only means something
alongside another flag would be earning very little.

## No `--label`, `--assignee` or `--milestone` passthrough

`gh pr create` supports all three and each is a one-line addition to `ghArgs`,
but they are not used here — so they would be flags, config fields,
`pullRequest` fields and test rows carried indefinitely for nobody, and a REST
`prOpener` would owe each of them a second or third API call (see *API notes* in
`todo.md`). `-r` stays because review requests are the one piece of PR metadata a
batch run actually sets.

## Targets name repos, not directories within them

Deferred, with the full design worked out and then removed — `git log` has it if
it comes back.

The idea: let target dirs name the directory the command runs in, so
`mkprs **/package.json -b b -- npm audit fix` handles repos where the manifest is
not at the root. The shell does the finding, which is the right division of
labour, and discovery resolves each target up to its repo root.

What sank it was not the discovery half — that part is cheap. It was that the
unit of work becomes the directory rather than the repo, and `outcome` is the
spine everything else hangs from: result lines, counters, and the capture a
failure replays. Per-directory outcomes meant splitting `report` into render and
count, a `rank()` for aggregating a repo's directories back into one summary
tally, a capture per directory, a reporter that emits N lines per repo instead of
streaming one, `commitAndPush` looping, per-directory commit messages, `{}`
diverging from `$REPO`, a new `$WORKDIR`, and a cap that counts directories
because `**/package.json` expands into every `node_modules` in the tree. Seven of
eight source files, and a partial undo of the work that got `processRepo` from
127 lines to 25.

Against that: a hypothetical. .NET repos keep the solution at the root, npm
workspaces already handle the monorepo case from the root, and
`-- bash -c 'cd src && npm audit fix'` costs nothing today — the no-shell rule
was written with exactly that escape hatch in mind.

**Revisit only with a real repo where the `cd` workaround is genuinely painful**,
not on the strength of the design being interesting. What survived the discussion
was shipped separately: a subfolder target now names the repo it is inside and
stops the run, rather than silently finding nothing, which was a bug on its own
terms.

## No "I did manual work, just open the PR" mode

Considered — adopt the branch the repo is already on, skip branch creation, allow
no command, skip cleanup — and dropped. `gh pr create` already infers base and
head and fills title and body from the commits, so a shell loop over
`gh pr create --fill` does the whole job. Inside mkprs it would cost a
conditional `-b`, a new per-repo source for the commit message and PR title (both
currently derive from the command text), and a second path through `processRepo`
that every later feature would have to reason about. An implicit trigger is also
a trap: a batch run would silently adopt a repo left on an old branch and open a
PR mixing unrelated work. Manual edits do not scale to forty repos, which is
where mkprs earns its keep, so the case is weakest exactly where the cost is
highest.

## No mutation testing

Considered as a way to grade the suite, and rejected on the state of the tooling
rather than the idea. It is a niche practice in Go — no large project runs it,
and the most visible effort (mutation testing the stdlib's crypto assembly for Go
1.26) bypassed the frameworks and patched `cmd/asm` instead. The best available
option, Gremlins, is pre-1.0 and sat dormant for 27 months between releases.
Revisit only if something reaches 1.0 with real adoption.

## Tests use real `git`, not a faked one

`git` runs for real throughout the suite, against fixture repos built in
`t.TempDir()` with bare repos standing in for GitHub. That is deliberate, not a
gap in the mocking — the obvious next step is a `gitRunner` interface mirroring
`prOpener`, and it should not be taken.

`prOpener` earns its seam because `gh` needs network and credentials, which makes
it unrunnable in a test, and because the boundary is genuinely thin: one
`pullRequest` in, one URL out, and `fakePR` is 25 lines that lose nothing. `git`
is neither of those. It needs no network and no auth, and it is not a boundary
but the place nearly all of mkprs's behaviour lives.

A fake would have to model refs existing locally versus on origin, `--prune`
clearing a merged branch's tracking ref, `diff --cached --quiet` exiting 0 on an
empty index, `symbolic-ref` failing on a detached HEAD, `rev-list --count`, and
`origin/HEAD` resolution with its fallbacks — a reimplementation of git's object
model. And that is the argument that settles it: the bugs mkprs can actually have
*are* those semantics. `getDefaultBranch` is nine lines of choosing git commands;
the comment in `commitAndPush` about `--quiet` exiting 0 on an empty index is a
claim about git, not about mkprs. Tested against a fake written from the same
understanding as the code, a wrong understanding passes. The result would be a
fast suite that confirms mkprs matches our beliefs about git, having deleted the
tests that check the beliefs.

The costs it avoids are small. The suite runs in ~6s; `isolateGit` in
`helper_test.go` detaches every invocation from the developer's own config, which
was the one way real git could make results nondeterministic; `t.TempDir()` scopes
and cleans up all filesystem work. Mixing tests that touch the real world in with
the rest is also the stdlib's own practice rather than a compromise — `os/exec`
spawns real subprocesses (via the re-exec-the-test-binary trick `helper_test.go`
borrows), `net/http` binds real sockets. Go's conventional split is build tags for
tests needing a *provisioned* dependency — a database, cloud credentials — and
`testing.Short()` for what is merely slow. `git` clears that bar the same way the
filesystem does, so the only `testing.Short()` skip is the `go build` in
`smoke_test.go`.

## Execution stays serial

`-j/--jobs` over `errgroup` was long treated as the headline feature — it was the
stated reason for leaving bash — and has been dropped anyway. Most batch commands
are fast enough that the wall-clock saving does not pay for the complexity, and
the tool is worth more correct than quick while its behaviour is still being
validated. It also costs less than it looks: serial runs are what let a failure
replay its whole capture as one contiguous block, keep result lines
one-per-line without a mutex, and make `--verbose` readable at all. Revisit only
with evidence of a run that is genuinely too slow, not on principle.
