# mkprs — open feature work

Formerly `git-patch-apply.sh`, then `mkprs.sh`. The patch-specific design was
replaced with "run an arbitrary command in each repo"; the items that only made
sense for patching (`--source` selection, patch-hash branch names, `--3way`
apply) have been dropped rather than carried forward. The bash implementation
was ported to Go at strict parity and deleted, `test.sh` was replaced by
`go test ./...`, and `gh` now sits behind the `prOpener` interface so it can be
mocked and, later, swapped for direct API calls.

Completed items are deleted rather than marked done — `git log` is the record.
Each item is a heading so the list can be skimmed from GitHub's outline, and
nothing is numbered, so items can come and go without renumbering.

Decisions already settled — including the ones about what mkprs deliberately does
*not* do — live in [`design-notes.md`](design-notes.md) and are not repeated here.

## Priorities

Every item carries a band on the line under its heading. The point is to decide
the order once, with the whole list in view, rather than re-argue it at the start
of each session — picking the next item ad hoc reliably favours whatever is most
interesting that day, which is not the same as whatever is most useful.

The sections below group by subject, not by urgency, and that stays: a flag
belongs with the other flags whatever it costs. The band is the second axis.

**The rules, in the order they are applied.**

1. **Bugs before features.** Something that behaves wrongly today outranks
   something that does not exist yet, and it outranks it regardless of size —
   wrong behaviour is being relied on while it goes unfixed, and every feature
   built over it inherits the fault. This is why a one-line argument-parsing fix
   sits above the tool's most valuable feature.
2. **Prerequisites above what they unblock.** An item another item is waiting on
   moves up to meet it. Doing them in the other order means building the
   dependent item twice, or building a workaround that then has to be deleted.
3. **Value against effort, for everything left.** Cheap and useful first;
   expensive and marginal last. The two ends are easy; the middle is judgement,
   and the one line under each heading is where that judgement is recorded so it
   can be disagreed with.
4. **Safety nets are worth more than their size suggests.** They are cheap to
   build and they bound a failure that lands in other people's repositories,
   which is not a cost this tool gets to absorb quietly.
5. **Documentation last.** Prose written against a moving tool is prose with an
   expiry date, and a stale README is worse than none because it is believed.

**The bands.**

- **P1** — bugs, safety nets, and cheap work another item is waiting on.
  Everything here is small, and none of it should sit behind a feature. This band
  is meant to stay short and to be emptied.
- **P2** — the features. Most of the tool's remaining worth is here, and so is
  most of the remaining work.
- **P3** — high cost against low reward, plus work deliberately parked. Not
  "never", but doing any of it ahead of P2 would be a mistake rather than merely
  early.

**Maintaining the bands.**

Within a band the order is loose — the band is the commitment. Two orderings are
fixed, and both are stated on the items themselves: `--fail-fast` precedes *Make
`--verbose` actually verbose*, and the README is written after everything that
would change what it says.

A band is not permanent. Finishing an item can promote another by clearing its
prerequisite, and a P3 can move up on evidence — a real repo that needs it, or a
run that goes wrong in the way it would have prevented. Rebanding is a normal
edit; leaving a band stale because it was written down once is not the intent.

New items get a band when they are added, while the reasoning is fresh. An item
with no band is an item nobody has decided about yet.

## Working agreement

*Priorities* settles which item is next. This settles how it gets built, and it
is binding for the same reason: decided once, in the calm, rather than
renegotiated in the middle of a feature where every rule looks like an obstacle.

**Red-green TDD.** Write the failing test first, run it, watch it fail, and only
then write the code that makes it pass. The order carries the whole value, and it
buys two separate things.

The first is that the test gets tested. A test that has never been seen to fail
has not been shown to test anything: it may assert on the wrong value, exercise a
path it does not reach, or pass vacuously because a helper returned early.
Watching it go red for the reason you predicted is the only evidence that its
green means something later. This is not hypothetical here — several tests turn
on git behaving in a particular way, and a test written around a wrong belief
about git passes both before and after the fix.

The second is that behaviour gets defined before there is an implementation to
shape it. A test written afterwards describes what the code does; a test written
first describes what it should do, and the difference shows up exactly where it
matters, in the edge cases the implementation quietly declined to handle. Most
items above already contain their own test list — the skip conditions, the table
rows, the message text — so the definition work is largely done and the tests
follow it.

**Red has to be red for the right reason.** In Go a test naming a function that
does not exist yet fails to *compile*, which is not the same as failing, and
proves nothing about the assertion. Stub the function first — the signature, and
a body that returns a zero value or panics — so the suite builds and the test
fails on its assertion, with a message that reads the way it should when the
feature later breaks. If the failure message would not tell you what went wrong,
fix it now, while it is on screen.

**One item at a time.** Take a single item, finish it, hand it back. Two items in
one change cannot be reviewed as either, cannot be reverted separately, and blur
which test covers which decision. The temptation is always the adjacent fix
noticed on the way past — the right home for it is a new item here, or a line
added to the item that already owns it. Where an item explicitly carries a
separable piece, splitting it is fine and stays within this rule; the
discarded-`gitError` bug under *Make `--verbose` actually verbose* is the example,
and it says so.

**Ask rather than assume.** When this file and the source do not settle a
question, ask it — a guess that turns out wrong is discovered at review, after
the tests have been written around it. Questions are cheap and the answers are
durable: each one becomes a line here or a note in
[`design-notes.md`](design-notes.md), so the same question is asked once rather
than re-derived by whoever meets it next. Most of the detail in the items above
started as exactly that.

**Finish by updating this file, then stop.** Delete the completed item — `git log`
is the record — and fold back anything the work turned up: a decision worth
keeping goes to [`design-notes.md`](design-notes.md), a follow-up becomes a new
item with a band, and any item whose prerequisite just cleared gets rebanded.
Then stop for review.

**Committing is not part of finishing.** Review, commit and push are the author's,
and they happen between items, not inside them. Leave the work in the tree.

## Flags & UX

### `-i, --interactive` review gate

**P3** — the largest item here, and `--list`, `--fail-fast` and `--update` each
deliver a piece of its value far more cheaply.

Pause in each repo after the command has run but before anything is staged,
committed, pushed or opened, print the diffstat, and ask. This is `git add -p`
for a batch operation, and it should borrow that key vocabulary rather than
invent one:

| key | meaning |
|---|---|
| `y` | commit this repo and open its PR |
| `n` | skip this repo, discarding the changes |
| `d` | show the full diff, then ask again |
| `s` | drop into a shell in the repo; on exit, re-read the diff and ask again |
| `a` | accept this and every remaining repo — stop asking |
| `q` | abort the run; this repo and the rest are left untouched |

Notes that matter for the implementation:

- **Require a TTY.** If stdin is not a terminal, fail at startup with a clear
  message rather than at the first prompt. `mkprs -i ... | tee log` from cron
  must not hang waiting for a keypress it can never receive.
- **stdin is already free.** `cmd.Stdin` is never set in `runCommand`, so the
  command runs against `/dev/null` and cannot swallow keystrokes meant for the
  prompt. Keep it that way.
- **The prompt has a seam to land in.** `processRepo` calls `runCommand` and then
  `commitAndPush`; the pause goes between the two, with nothing to disentangle
  first.
- **`n` is destructive.** Skipping falls through to `a.cleanup`, which checks out
  the branch the repo started on and deletes the working branch — the command's
  work is gone. Say so at the prompt, and treat `--keep-branch` as the escape
  hatch for "I want to look at this properly".
- **`a` is just a latch**, a bool that suppresses later prompts. `q` needs to
  stop the loop cleanly, letting the current repo's cleanup run.
- Skips are reported as normal (`⏭️`), so the summary still adds up.

#### `s` — drop into a shell in the repo

The payoff of the whole feature. A codemod that handles 90% of repos leaves a
handful needing a human, and today that means aborting the run, fixing one repo
by hand, and starting over. This turns those into a detour: land in the repo on
the working branch, fix it, `exit`, carry on.

- **Launch `$SHELL`**, falling back to `/bin/sh`. On Windows `$SHELL` is
  normally unset, so fall back to `%COMSPEC%` (or `powershell`) — the tool
  builds and is smoke-tested there, so this cannot be POSIX-only.
- **Working directory is the repo root**, matching where the command ran.
- **Pass the real terminal through** — `os.Stdin`, `os.Stdout`, `os.Stderr`
  directly. This is the deliberate exception to how everything else runs:
  commands get `/dev/null` on stdin and have their output captured, but a shell
  that cannot see its own tty is useless, and the user's session must not end
  up in the capture that a later failure replays.
- **Export `$REPO` and `$REPO_NAME`** exactly as the command gets them, plus
  something like `$MKPRS_BRANCH` so a prompt can show what is going on. Being
  dropped into a shell with no indication of which repo, or that a branch is
  checked out under you, is disorienting.
- **Ignore the shell's exit code.** People type `exit 1` and hit Ctrl-D out of
  habit; neither means "fail this repo".
- **Re-read the diff afterwards, never cache it.** The entire point is that the
  working tree may have changed, so the prompt that follows must reflect what
  is there now — including the case where the user reverted everything, which
  should then fall through to the usual "command made no changes" skip.

**Hand-typed `git commit` is already handled.** It used to be read as "no
changes" and have its branch deleted; `processRepo` now decides via `branchAhead`
(`git rev-list --count <base>..<branch>`) instead of the index, so a commit counts
no matter who made it. Nothing extra is needed for `s` beyond re-reading the diff.

**`git checkout` inside the shell fails the repo**, by the same rule that applies
to the command itself — and the shell is exactly where someone would reach for
it. The failure is safe (the branch and its commits survive), but the message is
written for a command, not for someone standing in a prompt. Worth either a
clearer message under `-i` or an explicit note at the prompt that the branch must
stay put.

**This supersedes `--preview`**, which was going to run the command in a
throwaway `git worktree`, print a diffstat and discard the result. That is
strictly worse: it pays the full cost of running the command and then throws
the work away, so a run you approve of has to be done twice. Pausing on the
real thing gives the same look at the same diff, and lets you continue.

**It deliberately does not touch `--max-repos` or the timeout.** Prompting on the
count would turn a circuit breaker into a reflexive `y`, and the friction of
re-running is what makes you read the number. Prompting on a timeout would
replace "fail and move on" with "block forever", which is the failure that flag
exists to prevent.

### `--update` to add to an existing branch and PR

**P2** — the band's highest value and its largest change; nothing is waiting on
it, so it follows the P1 items rather than leading them.

Once a run has opened its pull requests, mkprs cannot touch them again: the
branch now exists, so `preflight` skips every repo with `branch '<b>' already
exists on origin`. That reads like a guardrail but is the tool declining to help
— the forgotten file, the second cleanup pass, the fix that occurs to you after
review all have to be done by hand across thirty repos, which is the work mkprs
exists to remove.

`--update` means **create or update**: adopt the branch where it exists, create
it where it does not. A repo that was dirty on the first run and has since been
tidied gets picked up by the same re-run that adds a commit everywhere else,
rather than needing a separate invocation.

This is not the *"I did manual work, just open the PR"* mode in
[`design-notes.md`](design-notes.md), which was dropped because manual edits do
not scale to forty repos. This still runs the command in every repo, so it scales
exactly as well as the primary use case.

**Never implicit.** The flag is the whole safety story — silently adopting
whatever branch happened to match the name is the trap that note warns about.

#### Refuse the default branch, before any repo is touched

mkprs currently only ever writes to branches it created, and that is doing more
work than it looks: `mkprs ~/repos -b main -- <cmd>` is harmless today only
because every repo skips on "branch already exists". Under `--update` the same
typo would commit and push to `main` across the fleet.

This is a startup error, the way an uninterpretable target is — not a per-repo
skip, and not part of the table below, which would happily say "continue" for it.

#### When to continue

Pure git, no query to GitHub. Local and remote here mean `refs/heads/<branch>`
and `refs/remotes/origin/<branch>` after `preflight`'s existing
`fetch --prune`:

| Local | Remote | On the same commit as | Continue? |
|---|---|---|---|
| Y | Y | each other | **Yes** — `checkout <branch>`, commit, update the PR |
| Y | Y | — differ | **No** — skip |
| Y | N | the default branch | **Yes** — `checkout <branch>`, commit, open a PR |
| Y | N | — differ | **No** — skip |
| N | Y | n/a | **Yes** — create the local branch from origin's |
| N | N | n/a | **Yes** — create it from the default branch, as today |

**Every "yes" needs no ref moved; every "no" would.** That is the whole rule, and
it is *mkprs never moves a branch to a commit it did not create* in
[`design-notes.md`](design-notes.md) applied one row at a time. Rows 1 and 3 are
already sitting on the commit the work starts from, so a plain `git checkout` is
enough and the branch only ever advances by mkprs's own commit. Rows 5 and 6
*create* a local branch, which is not a move. Rows 2 and 4 would need
`checkout -B` to drag an existing branch somewhere else, and that is the line.

The equality tests are therefore exact, not conservative approximations. A branch
sitting three commits behind the default branch holds nothing unique and looks
harmless, but reaching the starting commit still means repositioning it — so it
belongs in row 4 with everything else that would have to move. Widening rows 1
and 3 to "is an ancestor of" would quietly reintroduce exactly the `-B` this
table exists to avoid.

Rows 5 and 6 are the common path: mkprs deletes its own local branch on success,
so the state after a normal run is *local absent*. The rows with a local branch
present arise only after `--keep-branch`, a failure, or a hand-made branch.

**Row 2 also covers "local strictly behind remote"**, which is provably safe to
fast-forward and is skipped anyway, for the reason above. The manual `reset
--hard` in the few affected repos is the price of keeping the rule whole. Never
force-push, by the same logic: a reviewer's suggestion committed through the
GitHub UI must not be overwritten.

**Row 4 protects unpushed work as a side effect.** The commits there might be a
squash-merged PR whose branch GitHub then deleted, or genuine local work that was
never pushed — and git cannot tell the two apart, because squashing rewrites the
SHAs. Only the PR's state distinguishes them, and asking for it would mean
`prOpener` reporting state rather than a URL. It never has to: the repo is
already skipped for needing a ref moved, so the harder question is one the table
never asks.

Both skips name the branch and say what would unblock them — delete it, or push
it — since the repo is otherwise silently absent from a run the user expected it
in.

**What the table gives up**, knowingly: a PR that was squash-merged in a repo
that does *not* auto-delete the branch. Local and remote still agree, so row 1
continues and the new PR re-proposes the already-merged commits alongside the new
one. That is a visibly wrong PR rather than destroyed work, it is caught in
review, and closing it costs nothing.

**`branchLocation` cannot answer this**, despite looking like it does. It returns
`"locally"` as soon as the local ref exists and never looks at origin, so rows 1,
3 and 5 are indistinguishable through it. The table needs local and remote
presence as independent facts plus a rev comparison — a new helper, with
`branchLocation` left alone for the cleanup rule below, which only needs the bit
it already reports.

#### Where the branch starts from, by row

`processRepo` checks out `-b <branch> <base>` unconditionally today, and under
`--update` that stops being one thing:

- **Rows 1 and 3** — `checkout <branch>`. It exists locally and is already on the
  commit the work starts from; moving it is what the table forbids.
- **Row 5** — create it from `origin/<branch>`, continuing the pushed branch.
- **Row 6** — create it from `origin/<default>`, exactly as today.

So `prep.base` becomes row-dependent rather than always `resolveBase`'s answer,
and this is the main structural change `--update` makes to `processRepo`.

#### Comparing against pre-command HEAD, not against base

`branchAhead` measures the branch against `origin/<default>`, which on a
follow-up run is already true from last time — so a command that changed nothing
would push and report success on a no-op.

Compare against **HEAD as it stood when the command started** instead. On a fresh
run the branch is cut from base, so the two are identical and nothing changes;
this is a generalization rather than a second path, and it makes the existing
"command made no changes" skip more precise on its own terms.

#### Cleanup restores the branches the repo had

The rule is not "delete the branch mkprs created" but **leave the repo with the
local branches it started with** — which is the same rule, stated so it covers
both modes. A branch that existed only on origin is checked out to do the work
and deleted afterwards; a branch that was already checked out locally stays.

`branchLocation`'s existing answer is enough here — cleanup only needs to know
whether the branch was local when the run arrived, which is exactly the bit it
already reports. (The table above needs more than that; see the note under it.)

Safe on every path: success pushed the commit, a skip pushed nothing but origin's
copy predates the run, and a failure never reaches cleanup at all.

**This makes `-k` a no-op whenever the branch already existed locally**, which is
every row 1 and row 3 run. The branch is kept because it was there to begin with,
not because `--keep-branch` was passed, so the flag only means something on rows
5 and 6 where mkprs created the branch itself. Worth saying out loud: after one
`-k` run, or a manual checkout, the flag stops doing anything in that repo.

It also files down an existing rough edge. `-k` leaves the repo standing *on* the
branch, so a later run has `startBranch == branch`; `restoreRepo` would then
no-op the checkout and try to `git branch -D` the branch it is standing on, which
git refuses. That failure is invisible today because `gitErrTo`'s error is
discarded. Under this rule the delete is never attempted.

#### Opening versus updating the pull request

`openPR` has to become create-or-return-existing — that much, and no more.
`prOpener` keeps returning a URL; the table above deliberately avoids needing PR
*state*, so the interface does not grow a field.

`gh pr create` fails when a PR exists for the head branch, and today that
surfaces as a plain repo failure even though the push succeeded — so it needs
`gh pr view --json url` as a fallback. The `422` handling sketched under *API
notes* is the cleaner version of the same thing, so this gets simpler after
*Replace `gh`* lands, but it does not have to wait for it.

**Leave the title and body alone.** The PR describes the whole effort and each
commit describes its increment, so there is nothing to restate — and a title
edited by a human during review must not be overwritten by one derived from
whatever command this run happened to pass. `--message` already applies per run,
so each update commit gets its own command-derived message for free. `--draft`
and `-r` apply on the create path and have nothing to act on when updating; say
so rather than silently ignoring them.

**Report the two apart**, which costs one field. `outcomeSuccess` holds only
`prURL` and `outcome.go` hardcodes `"PR created"`, so this is an `updated bool`,
a branch on that one string, and a second constructor beside `success`. Keep the
three summary counters: `Succeeded` is true of both, and the per-repo lines
already carry the distinction. If the new-PR count is wanted at a glance,
`Succeeded: 20 (12 created, 8 updated)` is the form that still sums to the repo
count.

**A missing PR is opened on the way past, but only if this run commits
something.** If an earlier run pushed and then failed at `gh` — no auth, a 403 —
origin has the branch and no PR exists. That is row 5, and create-or-return
opens the missing one. It is not a repair mode: `-- true` would reach the
pre-command HEAD check first and skip as "command made no changes", so nothing
gets opened without a real change to make. Requiring a command stays, and
allowing one to be omitted stays rejected — the PR title derives from the command
text, so there would be nothing to name the PR with. The real fixes for this
state are in *Replace `gh`*: fetching the token early kills the unauthenticated
case before any repo is touched, and backing off on `403` and `5xx` kills the
throttled one.

### `--list` to preview the repo set

**P2, first of the band** — best value per line left: a flag, an early return and
a loop over facts `preflight` already computes.

Print which repos pass the filters (GitHub remote, clean tree, branch free) and
exit without running anything. Cheap, and it covers the "what would this touch?"
half of the old `--dry-run` that `-i` does not, since `-i` still runs the command
before it asks.

### `--fail-fast` to stop at the first failure

**P1** — a flag and a `break`, and *Make `--verbose` actually verbose* depends on
it landing first.

The main loop always continues after a failure. When the first repo fails because
the command itself is wrong, you want to stop and fix it rather than watch 29
more failures scroll by.

This also repairs `--verbose`, which is currently worse than the default for
diagnosing a failure: quiet prints the failing repo's output as one contiguous
block under its `❌`, while verbose streams those bytes live mixed with every
other repo's and skips the replay. Stopping at the first failure makes that
moot — one repo's worth of output, and it is the last thing on screen. This is
the fix for that, rather than teaching verbose to replay; see *Make `--verbose`
actually verbose*.

### Make `--verbose` actually verbose

**P2, after `--fail-fast`** — wide, shallow work at every git call site; the
discarded-`gitError` bullet below is a plain bug, is P1, and can be lifted out
and done alone.

Today it streams one thing — the user's command's two streams, prefixed
`[repo]` — and nothing else. Everything mkprs itself does is either suppressed or
routed somewhere the console never sees, so the flag shows the least interesting
half of a run and hides the half that explains a skip. The rule to implement
against: under `-v`, anything that would help when something goes wrong is on
screen, as it happens.

The gaps, in the order they bite:

- **`git`/`gitOK` bypass the capture entirely.** `cmd.Output()` sends stdout to
  a buffer and stderr into the error, so neither stream can reach the console at
  any verbosity. That is `originURL`, `isCleanTree`, `getDefaultBranch`,
  `branchLocation`, `resolveBase`, `headBranch`, `branchAhead`, and the
  `diff --cached --quiet` check in `commitAndPush` — i.e. every filter that
  decides a repo's fate. Their stdout *is* the return value, which is the
  argument for swallowing it, but it is also the answer the user wants:
  `status --porcelain` is precisely the list of files that made a repo skip as
  "working tree not clean", and today that list is unobtainable without
  re-running git by hand.
- **Those errors are discarded at every call site.** `getDefaultBranch` failing
  becomes `skip("could not determine default branch")`; `branchAhead` failing
  becomes `fail("could not compare …")`. `gitError` now folds git's own words
  into the returned error, but nothing writes it anywhere, so it dies at the
  call site — in quiet mode as well as verbose. This one is a plain bug and
  fixable independently of the rest.
- **`--quiet`/`-q` on the mutating commands.** `checkout -b … --quiet`,
  `commit -q`, `push … --quiet` and `fetch … --quiet` all route through `gitTo`,
  so they would stream — the flag is what silences them. Under `-v` that costs
  commit's "3 files changed, 40 insertions", checkout's "Switched to a new
  branch", and push's `remote:` lines. Pass the quiet flags only when not
  verbose. Do not reach for `--progress` on fetch/push to force the transfer
  meter back: git suppresses it off a tty for good reason, and it is noise
  rather than information.
- **`git branch -D`'s stdout is discarded** by `gitErrTo` as noise, but
  "Deleted branch bump-deps (was abc1234)" carries the only record of what was
  just thrown away. Under `-v` it should print.
- **The command is never echoed.** Output arrives attributed to a repo but not
  to what produced it, and with git's own output added the mixture gets worse,
  not better.

#### Mark the lines that only exist because of `-v`

Two kinds of line, kept visually distinct, so a reader can tell mkprs's narration
from the output it is narrating:

```
[acme-web] $ git checkout -b bump-deps origin/main
[acme-web] Switched to a new branch 'bump-deps'
[acme-web] $ dotnet outdated -u
[acme-web] Analyzing acme-web.csproj...
```

The `$` marker borrows `set -x`, which is the same idea and already familiar.
Echo before running, not after, so a hang is attributable to the line above it.

**Trace lines go straight to `c.out`, never into `c.buf`.** The buffer is what a
failure replays under `❌` in quiet mode, and it must keep holding exactly what
the repo emitted — otherwise the two modes disagree about what happened. That
also answers "which lines exist only because of verbosity" precisely: the
`$` ones, and they never appear in a replay. A `func (c *capture) trace(format
string, args ...any)` that no-ops unless `c.verbose` puts the condition in one
place instead of at every call site.

**Do not add a replay for verbose.** A failure under `-v` already streamed
everything live; reprinting it under the `❌` would double it. `--fail-fast` is
the answer to "the failing repo's output is scattered among the others", and it
is a better one than a replay.

**Echo `gh` too**, with its full argv — and when the REST `prOpener` lands, the
method and URL in its place. That inherits the redaction constraint from
*Replace `gh`*: the trace is printed verbatim, so an `Authorization` header must
never be built into anything traceable. Worth a test that fixes this at the seam
rather than a rule to remember.

Most of the filters listed above now live in `preflight`, which is a contiguous
block rather than six statements scattered through `processRepo` — so the trace
lines land in one place.

### `--tracked-only` staging

**P2, last of the band** — smallest feature left, `-A` against `-u` behind a
bool, but it guards a case that has not actually come up yet.

`git add -A` stages everything the command left behind, including new files. That
is the right default (tools like `dotnet outdated -u` and scaffolders create
files), but a command that drops build artifacts in a repo with a thin
`.gitignore` will commit them. `--tracked-only` stages with `git add -u` instead.

### Validate the branch name before any repo is touched

**P1** — turns one bad `-b` from a failure in every repo into one message at
startup, for a prefix check and a single `git` call.

An invalid `-b` is found by git today, per repo, after that repo has been
fetched: `mkprs ~/repos -b --draft -- true` fetches forty times and fails forty
times with `fatal: '--draft' is not a valid branch name`. Nothing unsafe happens
and the message is git's own, which is a good one — `checkout -b` takes its
argument positionally, so there is no misparse to defend against here. What is
wrong is the arithmetic: one mistake, one message, forty repos.

That makes it a startup error, like *Refuse the default branch* under `--update`
and for the same reason — nothing about the answer varies per repo, so nothing is
learned by asking each one. Exit as a bad invocation, before discovery.

**Delegate the rules to git.** `git check-ref-format refs/heads/<branch>` is a
pure string check needing no repo and no network, and it covers spaces (`-b "my
branch"` is a likelier typo than `--draft`), `..`, a `.lock` suffix, a trailing
`.`, `@{` and control characters. Reimplementing that list here would drift from
whatever git actually enforces, and silently.

**The leading dash has to be checked separately**, which is the one thing
delegation does not get for free: `git check-ref-format refs/heads/--draft` exits
0. Git's "cannot begin with a dash" rule lives in its branch-name path — `git
branch -- -foo` refuses — not in `check-ref-format`. So this is a
`strings.HasPrefix(branch, "-")` test *plus* the exec, and the dash is the case
that motivated the item.

**Not in `parseArgs`.** That is a pure function of its arguments, deliberately;
running git inside it would end that and make `cli_test.go` need a git binary to
test parsing. This goes at startup in `run`, ahead of discovery, where
`--update`'s default-branch refusal will also land.

`checkFlagValues` already rejects `-b --`, which is a different failure — the
separator was swallowed, so the branch name is the least of it — and stays where
it is.

Tests: a valid name passes; a leading dash, an embedded space and `..` each fail
with the branch named, and fail before discovery has walked anything.

## Discovery & safety limits

The timeout and `--max-repos` are both safety nets, so **both ship with a default
on**. A limit you have to remember to pass is absent exactly when it matters:
nobody types `--timeout` on the run where the command turns out to hang, because
they did not know it would. Opt-out, not opt-in.

### Per-repo command timeout, defaulting to 10 minutes

**P1** — one `exec.CommandContext` in `runCommand`, against a run that otherwise
stalls forever with no output.

One hung command stalls the whole run with no feedback, and serial execution
means it stalls every repo behind it. Run under `exec.CommandContext` with
`context.WithTimeout`; expiry is a normal per-repo failure (`command timed out
after 10m`), so the run continues and cleanup still fires through the existing
`defer a.cleanup`. `runCommand` is the whole surface this touches: it already
returns the error `processRepo` turns into that failure. `--timeout <duration>`
overrides, `--timeout 0` disables. 10 minutes is chosen to sit well clear of a
slow-but-real `npm ci` or `dotnet restore` while still catching a wedged process
the same afternoon; tune it once there is evidence, but do not ship it unset.

### `--max-repos` safety limit, defaulting to 50

**P1** — one comparison plus a message, and the only thing between a mistyped
target and 200 pull requests in other people's repos.

Easy to point this at `~/repos` and accidentally open 200 PRs. Hard-fail before
any repo is touched if discovery returns more than the cap, and make the message
the fix: `found 84 repositories, above the --max-repos limit of 50; re-run with
--max-repos 84 to proceed`. 50 clears the ~40-repo runs that are the normal case
here, so the guard stays invisible until something is genuinely wrong — which is
the only way a default like this survives contact with daily use. One flag away
when the large run is intentional, never silent when it is not. Count after
`dedupeRepos`, which `run` already applies, so a repo reached from two targets
does not spend two of the budget.

### Stop discovery depth search at the first repo found

**P1** — a bug that silently inflates the repo set, and the discovery speedup,
in the same small change.

Today pruning stops descent into the `.git` directory itself, not into the rest
of the tree, so a repo nested inside another repo is discovered and both are
processed. Verified against `~/Code`, where `CSharp/TestProjects` is a repo
containing 10 nested repos and all 11 are returned — a tree can contain far more
repos than it appears to.

Git itself discourages the arrangement: the inner repo has to be `.gitignore`d
by hand, and submodules exist for the case. So a nested repo found by accident
is more likely a stray checkout than something anyone wants a pull request
against. Prune the whole repo subtree instead: when a directory holds a `.git`
directory, record it and `fs.SkipDir` the directory rather than the `.git`.

Naming one directly still works, and is not an error — `mkprs outer
outer/vendor/inner` processes both. That already falls out of the current
structure: an inner repo passed as a target is found by the walk as its own
root, so the "target is inside repository" check never runs for it. Worth a
test pinning it, since it is the half of the behaviour that is easy to lose
while implementing the other half.

The `.git`-file check stays as-is, or submodules and linked worktrees start being
walked into — see *Submodules and linked worktrees are both excluded from
discovery* in [`design-notes.md`](design-notes.md).

Independently of nesting, this is the discovery speedup: today every repo's
full working tree is walked looking for more `.git` directories, which means
`node_modules`, `bin/obj`, `.venv`, all of it. Pruning at the root reads one
directory per repo instead. On a tree like `~/Code` that is likely the larger
cost, and it is paid on every run.

Detection has to be a stat of `<dir>/.git` on entering each directory, not a
check of the `.git` entry itself: `fs.SkipDir` returned from a directory skips
*that* directory's contents, so returning it from `.git` cannot prune the
parent. Use `os.Lstat` rather than `os.Stat` — `d.IsDir()` on a `DirEntry` does
not follow symlinks, so a symlinked `.git` is excluded today and `os.Stat`
would quietly start including it.

Reduces the pressure behind `--max-repos` but does not remove it: forty sibling
repos under one target is still forty pull requests.

## Replace `gh` with direct GitHub API calls

**P2** — high work and high reward: it drops the last external binary, and the
README's install section is waiting on it.

`gh` is the last external binary mkprs needs, which undercuts the reason it was
written in Go: download one file and run it, on any platform. A user without the
GitHub CLI gets `'gh' (GitHub CLI) is not installed` and no PRs.

Opening a pull request is one `POST`, so this costs a `net/http` call and an auth
story — no new dependencies. **The seam already exists.** `ghCLI` implements
`prOpener` (`internal/mkprs/pr.go`); a `restAPI` implementing the same interface
drops in without touching `openPR`, and the end-to-end tests inject `fakePR`, so
they need no rewriting either. `ghCLI` then goes — see *Migration* below.

### Authentication

**Discover a token in this order**, first hit wins:

1. `GH_TOKEN`, then `GITHUB_TOKEN`. The CI convention, and what `gh` itself reads
   first. Actions provides `GITHUB_TOKEN` automatically.
2. `gh auth token` (and `gh auth token --hostname <host>` for Enterprise), if
   `gh` is on `PATH`. This inherits gh's keyring/`hosts.yml` without
   reimplementing it, so existing users need no setup — but is a convenience,
   not a requirement.
3. `git credential fill`. Write `protocol=https\nhost=<host>\n\n` to stdin and
   read `password=` back. This reaches the platform credential helper
   (osxkeychain, wincred, libsecret) that the user's `git push` already relies on.
4. Otherwise fail with an actionable message naming all three options, rather
   than a bare 401.

**The token authenticates the API, not `git push`.** This is the sharp edge and
belongs in the docs. mkprs pushes via `git push`, which uses whatever transport
`origin` names — an SSH key or a credential helper — and never sees the token.
So two new failure modes appear that `gh` masked:

- SSH-only user, no token: `push` succeeds, opening the PR fails.
- Token set, no push credentials: the API works but nothing was pushed to open
  a PR *from*.

Fetching the token early and failing before any repo is touched is better than
discovering it after N commits have been pushed.

**Scope**: `repo` for private repos, `public_repo` if only public ones. A
fine-grained PAT needs *Pull requests: write* plus *Contents: read*.

**Never let the token reach disk or `ps`.** Pass it as an `Authorization` header
built at call time — not on a command line, and not into the `capture`. That
last one is sharper than it looks: a failed repo now replays its entire capture
to stdout, so anything the API layer writes there is printed verbatim. A
redaction test is worth having.

**Enterprise**: derive the host from `remote.origin.url` rather than hardcoding
`api.github.com`; GHES lives at `https://<host>/api/v3/`. `originURL` already
reads the *un-rewritten* config value, which is what makes this parseable.

### API notes

- `POST /repos/{owner}/{repo}/pulls` with `base`, `head`, `title`, `body`.
  Response `.html_url` is the line mkprs prints today.
- Reviewers are a **second** call —
  `POST /repos/{owner}/{repo}/pulls/{number}/requested_reviewers` — and labels
  and assignees a third, via the issues API. `gh pr create` hides this behind one
  invocation, so the extra PR fields above get more involved here, and partial
  failure becomes possible: the PR exists but the reviewer was not added. Prefer
  reporting success with a warning over failing the repo.
- `422` usually means a PR already exists for that head. That is a skip, not a
  failure — better than the current opaque `failed to create PR`.
- Back off on `403` secondary rate limits and `5xx`; a 30-repo run trips these.

### Migration

**Replace `ghCLI` outright** — `restAPI` is the only implementation. Delete
`ghArgs` and `TestGhArgs` with it. Nothing needs proving out in parallel; if it
did, that is what a pre-release is for, not a second code path.

Keeping both would serve nobody: token source 2 is `gh auth token`, so every user
with a working `gh` already hands `restAPI` a valid token. The only users left
over are ones where `gh pr create` works but `gh auth token` will not print — and
paying for that with two code paths costs real things. The two would disagree on
behaviour (`422` is a skip and a failed reviewer is a warning on the REST path;
`ghCLI` collapses both into `failed to create PR`), so what a user sees would
depend on their environment. The redaction rule above would need getting right
twice, once for argv and once for method + URL. And a fallback that only fires in
configurations that are hard to reproduce stays untested until the day it fails.

`gh` becomes an optional convenience — a place to find a token — rather than a
requirement, which is the point of this whole item.

**The seam stays.** `prOpener` earns its place from `fakePR` in the end-to-end
tests, not from having two production implementations.

## Polish

### There is no `README.md`

**P3, and last of everything** — almost every open item changes what it would
say; see *Write this last* below.

The repo has `LICENSE`, `go.mod`, `go.sum`, `main.go`, `internal/` and two
markdown files — someone landing on it from GitHub gets no idea what mkprs is,
and `todo.md` is the closest thing to documentation, which reads as a backlog
rather than an introduction.

Worth covering, roughly in this order:

- **What it is, in two sentences**, with one example that shows the whole
  shape. The `dotnet outdated -u` one from `usageTail` earns its place: run a
  command across every repo under a directory, commit, open a PR each.
- **Install.** `go install github.com/jarrettgilliam/mkprs@latest`, plus the
  `gh` prerequisite and `gh auth login` — today a user without the GitHub CLI
  finds out by watching every repo fail. (*Replace `gh`* above removes that
  requirement; until it lands the README has to state it.)
- **How it behaves per repo**: cut a branch from the default branch, run the
  command, `git add -A`, commit, push, open the PR against the default branch,
  then restore and delete the branch. The skip and failure conditions belong
  here too — that is the part `--help` states tersely and a reader actually
  needs prose for.
- **The `{}` / `$REPO` / `$REPO_NAME` contract** and the no-shell rule.

**Do not restate the flag table.** It lives in `usageHead` and pflag's generated
`FlagUsages`, and a copy in the README will drift the first time a flag is added
— several items under *Flags & UX* above add one. Link to `mkprs --help` and keep
the README to the parts that are stable.

Same risk applies to the examples, which is an argument for using few and
choosing ones tied to behaviour that will not change.

**Write this last.** Almost every open item above changes something the README
would have to state: the flags under *Flags & UX* and *Discovery & safety limits*
each add a line, and *Replace `gh`* removes the install prerequisite entirely.
Documenting the tool before those land means writing prose with a known expiry
date. The cost of having no README is borne by strangers arriving from GitHub,
which is not yet the audience; the cost of a stale one is borne by them too, and
is worse, because it is believed.

### Spell out names that outlive their line

**P3** — readability only, and cheapest done after the items that are about to
rewrite these same signatures.

Single letters are fine for a local whose declaration is visible from its use.
They are not fine for struct fields, package-level declarations, or parameters,
where the reader meets the name far from anything that explains it. A good name
deletes the comment that would have explained it.

Receivers stay short (`a`, `c`, `o`, `r`) — that is Go convention, not laziness,
and `func (c *capture)` reads fine because the type is right there.
`Write(p []byte)` keeps its parameter name to match `io.Writer`.

The actual offenders, all `internal/mkprs`:

| where | now | suggested |
|---|---|---|
| `outcome.go` field | `outcomeFailed.c` | `output` |
| `mkprs.go` field | `app.errw` | `errOut` |
| `git.go` params | `gitTo(…, w io.Writer)`, `gitErrTo(…, w)`, `fetchOrigin(…, w)`, `restoreRepo(…, w)` | `log` |
| `git.go` param | `resolveBase(repoPath, dflt)` | `defaultBranch` |
| `run.go` params | `c *capture`, in all six of `processRepo`, `openPR`, `preflight`, `cleanup`, `runCommand`, `commitAndPush` | `output` |
| `cli.go` param | `printUsage(w io.Writer, fs *pflag.FlagSet)` | `out`, `flags` |

`outcomeFailed.c` is the one that proves the point: it carries a three-line
comment whose first job is saying what the field *is*. Named `output`, only the
non-obvious half needs to survive — that it is still being written to when the
outcome is built, because the deferred `restoreRepo` runs before the caller
reports.

`config`'s fields, the `usage*` constants and the `exit*` constants are already
fine; this is not a sweep of everything short.

### A `repo` type for the git helpers

**P3** — parked by its own last sentence: high work, and only worth it if
`git.go` grows again.

Every git helper takes `repoPath` first, which is a method receiver wearing a
disguise. A `repo` type with `r.git(…)` would delete that parameter from fifteen
signatures in `git.go` alone. Attractive, and much larger than it looks — worth
doing only if the file grows again.
