# mkprs — open feature work

Each item is a heading so the list can be skimmed from GitHub's outline, and
nothing is numbered, so items can come and go without renumbering.

Decisions already settled — including the ones about what mkprs deliberately does
*not* do — live in [`design-decisions.md`](design-decisions.md) and are not repeated here.

## Priorities

Every item carries a band on the line under its heading, so the order is decided
once rather than re-argued each session. Sections group by subject; the band is
the second axis.

**The rules, in the order they are applied.**

1. **Security issues and bugs before features**, regardless of size. One written
   inside a feature item still counts: its own band, its own line, done first.
   Security issues go first among those.
2. **Prerequisites above what they unblock.**
3. **Value against effort, for everything left.** The middle is judgment, and
   the line under each heading records it so it can be disagreed with.
4. **Safety nets outrank their size.** They bound a failure that lands in other
   people's repositories.

**The bands.**

- **High** — security issues, bugs, safety nets, and cheap work another item is
  waiting on. Short, meant to be emptied, and never behind a feature. Usually
  small, but size does not buy a bug or a vulnerability its way out.
- **Medium** — the features. Most of the remaining worth and most of the work.
- **Low** — a lot of work for little return, plus work deliberately parked. Not
  "never", but ahead of Medium it would be a mistake rather than merely early.

**Maintaining the bands.** Order within a band is loose; the band is the
commitment. A fixed position is stated on the item's own band line and nowhere
else. Rebanding is a normal edit — finishing an item can promote another by
clearing its prerequisite, and a Low moves up on evidence. New items get a band
when they are added; an item with no band is one nobody has decided about yet.

## Working agreement

- Instead of picking the first item, pick an item from the list using the
  rules from the priorities section above.
- Use red/green test driven development (TDD). Write failing tests first, then
  make them pass. This validates the test works as it should, while also clearly
  defining behavior before writing the implementation.
- Comments earn their place by explaining *why*, and only where a reader would
  otherwise get it wrong — name the plausible alternative and why it lost. Don't
  restate the code, the test name, the assertion below it, or what
  [`design-decisions.md`](design-decisions.md) and the usage text already say. No
  historical narrative: what the code used to do is git's job, not a comment's.
- Work on a single item at a time.
- If you have any questions that this file or source code doesn't clarify, ask.
  Don't make assumptions.
- When finished, update this file — completed items are deleted rather than
  marked done — then stop. I'll review, commit, and push before work starts on
  the next feature.
- Edit [`design-decisions.md`](design-decisions.md) only when a standing truth is
  discovered or changes, or to fix spelling and grammar — completing a feature
  is not a reason, and the file's own header explains why. If a note looks
  wrong, say so rather than quietly rewriting it.

## Flags & UX

### `-i, --interactive` review gate

**Low** — the largest item here, and `--list` and `--update` each
deliver a piece of its value far more cheaply.

Pause in each repo after the command has run but before anything is staged,
committed, pushed or opened, print the diffstat, and ask. This is `git add -p`
for a batch operation, so it takes that prompt's keys where they carry over
rather than inventing a set from scratch:

| key | meaning | in `git add -p` |
|---|---|---|
| `y` | commit this repo and open its PR | stage this hunk |
| `n` | skip this repo, discarding the changes | do not stage this hunk |
| `p` | show the full diff, then ask again | print the current hunk |
| `e` | drop into a shell in the repo; on exit, re-read the diff and ask again | manually edit the current hunk |
| `a` | accept this and every remaining repo — stop asking | stage this hunk and all later ones in the file |
| `q` | abort the run; this repo and the rest are left untouched | quit |

Notes that matter for the implementation:

- **Require a TTY.** If stdin is not a terminal, fail at startup with a clear
  message rather than at the first prompt. `mkprs -i ... | tee log` from cron
  must not hang waiting for a keypress it can never receive.
- **stdin is already free.** `cmd.Stdin` is never set in `runCommand`, so the
  command runs against `/dev/null` and cannot swallow keystrokes meant for the
  prompt. Keep it that way.
- **The prompt has a seam to land in.** `process` calls `runCommand` and then
  `commitAndPush`; the pause goes between the two, with nothing to disentangle
  first.
- **`n` is destructive.** Skipping falls through to `a.cleanup`, which checks out
  the branch the repo started on and deletes the working branch — the command's
  work is gone. Say so at the prompt, and treat `--keep-branch` as the escape
  hatch for "I want to look at this properly".
- **`a` is just a latch**, a bool that suppresses later prompts. `q` needs to
  stop the loop cleanly, letting the current repo's cleanup run.
- **`n` is a skip** (`⏭️`), so the summary still adds up. It is the one skip
  mkprs does not determine for itself — the user answers "no pull request to
  create here" at the prompt — so if this lands, *a failure is a repo you will
  have to run again for* in [`design-decisions.md`](design-decisions.md) needs a line
  saying a skip can be earned that way too.

#### `e` — drop into a shell in the repo

The payoff of the whole feature. A codemod that handles 90% of repos leaves a
handful needing a human, and today that means aborting the run, fixing one repo
by hand, and starting over. This turns those into a detour: land in the repo on
the working branch, fix it, `exit`, carry on.

- **Launch `$SHELL`**, falling back to `/bin/sh`. On Windows `$SHELL` is
  normally unset, so fall back to `%COMSPEC%` (or `powershell`). (This cannot be POSIX-only)
- **Working directory is the repo root**, matching where the command ran.
- **Pass the real terminal through** — `os.Stdin`, `os.Stdout`, `os.Stderr`
  directly. This is the deliberate exception to how everything else runs:
  commands get `/dev/null` on stdin and have their output captured, but a shell
  that cannot see its own tty is useless, and the user's session must not end
  up in the capture that a later failure replays.
- **Export `$REPO` and `$REPO_NAME`** exactly as the command gets them.
- **Ignore the shell's exit code.** People type `exit 1` and hit Ctrl-D out of
  habit; neither means "fail this repo".
- **Re-read the diff afterwards, never cache it.** The entire point is that the
  working tree may have changed, so the prompt that follows must reflect what
  is there now — including the case where the user reverted everything, which
  should then fall through to the usual "command made no changes" skip.

**`git checkout` inside the shell fails the repo**, by the same rule that applies
to the command itself.

**It deliberately does not touch `--max-repos` or the timeout.** Prompting on the
count would turn a circuit breaker into a reflexive `y`, and the friction of
re-running is what makes you read the number. Prompting on a timeout would
replace "fail and move on" with "block forever", which is the failure that flag
exists to prevent.

### `--update` to add to an existing branch and PR

**Medium** — the band's highest value and its largest change; nothing is waiting on
it, so it follows the High items rather than leading them.

Today, once a run has opened its pull requests, mkprs cannot touch them again: the
branch now exists, so `preflight` fails every repo with `branch '<b>' already
exists on origin`. That reads like a guardrail but is the tool declining to help
— the forgotten file, the second cleanup pass, the fix that occurs to you after
review all have to be done by hand across thirty repos, which is the work mkprs
exists to remove.

`--update` means **create or update**: adopt the branch where it exists, create
it where it does not. A repo that was dirty on the first run and has since been
tidied gets picked up by the same re-run that adds a commit everywhere else,
rather than needing a separate invocation.

This is not the *"I did manual work, just open the PR"* mode in
[`design-decisions.md`](design-decisions.md), which was dropped because manual edits do
not scale to forty repos. This still runs the command in every repo, so it scales
exactly as well as the primary use case.

**Never implicit.** The flag is the whole safety story — silently adopting
whatever branch happened to match the name is the trap that note warns about.

#### Refuse the default branch, before any repo is touched

mkprs currently only ever writes to branches it created, and that is doing more
work than it looks: `mkprs ~/repos -b main -- <cmd>` is harmless today only
because every repo fails on "branch already exists". Under `--update` the same
typo would commit and push to `main` across the fleet.

This is a startup error, the way an uninterpretable target is — not a per-repo
failure, and not part of the table below, which would happily say "continue" for
it.

#### When to continue

Pure git, no query to GitHub. Local and remote here mean `refs/heads/<branch>`
and `refs/remotes/origin/<branch>` after `preflight`'s existing
`fetch --prune`:

| Row | Local Branch Exists | Remote Branch Exists | Branches on same commit?      | Continue? |
| --- | ------------------- | -------------------- | ----------------------------- | --------- |
|   1 | Yes                 | Yes                  | Yes                           | Yes       |
|   2 | Yes                 | Yes                  | No                            | No        |
|   3 | Yes                 | No                   | Same as the default branch    | Yes       |
|   4 | Yes                 | No                   | Different from default branch | No        |
|   5 | No                  | Yes                  | N/A                           | Yes       |
|   6 | No                  | No                   | N/A                           | Yes       |

**Every "yes" needs no ref moved; every "no" would.** That is the whole rule, and
it is *mkprs never moves a branch to a commit it did not create* in
[`design-decisions.md`](design-decisions.md) applied one row at a time. Rows 1 and 3 are
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

Row 2 covers the local branch being either behind or ahead of the remote.
If behind, we would have to "pull", which moves the ref (breaking the design rule).
If ahead, `mkprs` could potentially push more work than was intended.
The solution to both is manual intervention. manually push manual work before starting,
or pull commits others have made. Both are safeguards. Don't push work you didn't do
and don't pull work that could break the command you're about to run.

**Row 4 protects un-pushed work as a side effect.** The commits there might be a
squash-merged PR whose branch GitHub then deleted, or genuine local work that was
never pushed — and git cannot tell the two apart, because squashing rewrites the
SHAs. Only the PR's state distinguishes them, and asking for it would mean
`prOpener` reporting state rather than a URL. It never has to: the repo has
already failed for needing a ref moved, so the harder question is one the table
never asks.

**Every "no" row is a failure, not a skip**, by *a failure is a repo you will
have to run again for* in [`design-decisions.md`](design-decisions.md). The work is still
wanted in these repos; mkprs cannot reach it without moving a ref, and the user
has to resolve that and re-run. So each names the branch and says what would
unblock it — delete it, or push it — rather than leaving the repo quietly absent
from a run it was expected in.

A PR squash-merged in a repo that does *not* auto-delete the branch lands in row
1: local and remote still agree, so the table says continue, but neither can be
merged into the default branch any more because they have diverged. `preflight`
should detect that and fail the repo — deleting the stale branch is the manual
step that unblocks it, which is the same shape as rows 2 and 4.

#### Where the branch starts from, by row

`process` checks out `-b <branch> <base>` unconditionally today, and under
`--update` that stops being one thing:

- **Rows 1 and 3** — `checkout <branch>`. It exists locally and is already on the
  commit the work starts from.
- **Row 5** — create it from `origin/<branch>`, continuing the pushed branch.
- **Row 6** — create it from `origin/<default>`, exactly as today.

So `prep.base` becomes row-dependent rather than always `resolveBase`'s answer,
and this is the main structural change `--update` makes to `process`.

#### On `branchAhead` functionality

`branchAhead` measures the branch against `origin/<default>`, which on a
follow-up run is already true from last time — so a command that changed nothing
would push and report success on a no-op.

Instead, we should only report success if `mkprs` pushes code or creates a PR.
If all the code and PR already exist on the server, tell the user that and skip.
If code is pushed or a PR is created, tell the user that and report success.

That skip is an earned one — everything the run would have done is already on the
server, so there is genuinely nothing to do and no reason to come back; see *a
failure is a repo you will have to run again for* in
[`design-decisions.md`](design-decisions.md).

This fills in a functionality gaps. If the user command, stage, and commit all work,
but push or PR creations fails (Due to poor network, auth failure, etc) the user
has an automated path to push and create a PR for all repos after fixing network
and auth issues.

#### Cleanup restores the branches the repo had

The rule is not "delete the branch mkprs created" but **leave the repo with the
local branches it started with** — which is the same rule, stated so it covers
both modes. A branch that existed only on origin is checked out to do the work
and deleted afterwards; a branch that was already checked out locally stays.

`branchLocation`'s existing answer is enough here — cleanup only needs to know
whether the branch was local when the run arrived, which is exactly the bit it
already reports.

**This makes `-k` a no-op whenever the branch already existed locally**, which is
every row 1 and row 3 run. The branch is kept because it was there to begin with,
not because `--keep-branch` was passed, so the flag only means something on rows
5 and 6 where mkprs created the branch itself. Worth saying out loud: after one
`-k` run, or a manual checkout, the flag stops doing anything in that repo.

It also forecloses a hazard `--update` would otherwise introduce. `-k` leaves the
repo standing *on* the branch, so a later run has `startBranch == branch`;
`repo.restore` would then no-op the checkout and try to `git branch -D` the branch
it is standing on, which git refuses — and the refusal would go unseen, since
the `deleteBranch` run's error is discarded. That state is unreachable today only because
`preflight` fails the repo before it gets there ("branch already exists
locally"); `--update` is exactly the change that lets it through. Under this
rule the delete is never attempted.

#### Opening versus updating the pull request

`openPR` has to become create-or-return-existing while also returning to the caller if
the PR was created or already existed, since that along with if anything was
pushed determine the outcome: skip or success.

Along those lines, `pipeline.go`'s `commitAndPush` function should be split into independent
`commit` and `push` functions. `push` will need to return whether the push did
anything at all (separate from err). If git reports, "Everything up-to-date"
and the PR already exists, outcome should be skip. If push actually pushed a
commit, or a PR is creates, outcome should be success.

`gh pr create` fails when a PR exists for the head branch, and today that
surfaces as a plain repo failure even though the push succeeded — so it needs
`gh pr view --json url` as a fallback. The `422` handling sketched under *API
notes* is the cleaner version of the same thing, so this gets simpler after
*Replace `gh`* lands, but it does not have to wait for it.

**Leave the title and body alone.** The PR describes the whole effort and each
commit describes its increment, so there is nothing to restate — and a title
edited by a human during review must not be overwritten by one derived from
whatever command this run happened to pass. `--message` already applies per run,
so each update commit gets its own command-derived message for free. `--title`,
`--body`, `--reviewers`, and `--draft` apply on the create path and have nothing
to act on when updating; say so rather than silently ignoring them.

**Report the two apart**, which costs one field. `outcomeSuccess` holds only
`prURL` and `outcome.go` hardcodes `"PR created"`, so this is an `updated bool`,
a branch on that one string, and a second constructor beside `success`. Keep the
three summary counters: `Succeeded` is true of both, and the per-repo lines
already carry the distinction. If the new-PR count is wanted at a glance,
`Succeeded: 20 (12 created, 8 updated)` is the form that still sums to the repo
count.

### `--list` to preview the repo set

**Medium, first of the band** — best value per line left: a flag, an early return and
a loop over facts `preflight` already computes.

Print which repos pass the filters (GitHub remote, clean tree, branch free) and
exit without running anything. Cheap, and it covers the "what would this touch?"
half of the old `--dry-run` that `-i` does not, since `-i` still runs the command
before it asks.

### Make `--verbose` actually verbose

**High** — wide, shallow work at every git call site.

Today it streams one thing — the user's command's two streams, prefixed
`[repo]` — and nothing else. Everything mkprs itself does is either suppressed or
routed somewhere the console never sees, so the flag shows the least interesting
half of a run and hides the half that explains why a repo failed. The rule to implement
against: under `-v`, anything that would help when something goes wrong is on
screen, as it happens.

The gaps, in the order they bite:

- **`text`/`ok` bypass the capture entirely.** Neither redirects a stream, so
  `run` buffers stdout for the return value and stderr for the error, and
  neither can reach the console at any verbosity. That is `originURL`,
  `isCleanTree`, `defaultBranch`, `branchLocation`, `resolveBase`, `headBranch`,
  `branchAhead`, and the `nothingStaged` check in `commitAndPush` — i.e. every filter that
  decides a repo's fate. Their stdout *is* the return value, which is the
  argument for swallowing it, but it is also the answer the user wants:
  `status --porcelain` is precisely the list of files that failed a repo as
  "working tree not clean", and today that list is unobtainable without
  re-running git by hand.
- **git's stderr belongs on the `-v` stream and nowhere else.** It must never be
  appended to a skip or fail reason. Those lines are read by people who did not
  ask how mkprs works and should not have to know: `could not compare 'b' to
  origin/main` is the message, and `fatal: ambiguous argument 'origin/main..b'`
  under it is a complaint from a `git rev-list` the user never typed, cannot see,
  and gains nothing from. Quiet mode stays plain English about what mkprs
  concluded; `-v` is where the evidence lives, under a `$` line that says which
  command produced it.

  Consequence: **delete `gitError`** as part of this item. Its only purpose is to
  fold git's first stderr line into an error so a reason line can carry it, which
  is the thing being ruled out. No production code consumes it either —
  `defaultBranch` and `branchAhead` return a `bool`, so the error dies inside
  the helper — and once `text` echoes the stderr it already buffers under `-v`,
  stderr has the full text anyway. Keeping it would leave a helper whose only
  reader is a rule against reading it.

  The failing command's stderr that the replay needs — see the next bullet — does
  not bring it back. `gitError` put the text inside `err.Error()`, so every
  consumer carried it whether or not it wanted it. What replaces it is a value
  only the replay reads: `failure` gaining a detail argument, or `run` returning
  the stderr it collected alongside the error.

  **`TestGitErrorCarriesStderr` goes with it.** It is the one thing that reads
  the enriched text: stubbing `gitError` to return `err` unchanged fails that
  test and no other, which is what "nothing consumes it" was measured by.
  `TestLoggedErrorDoesNotRepeatStderr` beside it asserts the half that survives —
  a logged complaint stays out of the error — and becomes the whole rule once
  there is no folding left to switch off. Replace
  it with tests for the two routes that succeed it: git's stderr reaches stderr
  under `-v`, and a failing `push` or `commit` has its stderr replayed under the
  `❌` while the reason line beside it stays plain.
- **`--quiet`/`-q` on the mutating commands.** `createBranch`, `commit`, `push`
  and `fetch` all route through `toLog()`, so they would stream — the flag is
  what silences them. Under `-v` that costs commit's "3 files changed, 40
  insertions", checkout's "Switched to a new branch", and push's `remote:` lines.

  **Stop passing the quiet flags altogether** — they live in those four
  constructors in `gitcmds.go` — and decide the destination in one
  place instead: under `-v` both streams go to stderr, and otherwise both are
  discarded. That is simpler than making four constructors conditional, and it puts
  every internal git command on the same rule as the interrogating terminals above
  — mkprs's own output is either on screen under `-v` or nowhere. `repo.log()` is
  that one place: `toLog` and `errToLog` already route their streams through it, so the
  rule is written once there rather than at each call site.

  **The workflow's output reaches `c.buf`; bookkeeping's does not.** mkprs
  automates a manual routine — fetch, cut a branch, run the command, stage,
  commit, push, open the PR — and those commands' output is output the user would
  have seen doing it by hand. When one of them fails the repo, its stderr is
  replayed under the `❌` beside the user's command's own output.

  Everything mkprs runs to *decide* what to do stays out: `config --get
  remote.origin.url`, `status --porcelain`, `symbolic-ref`, the `rev-parse
  --verify --quiet` calls, `rev-list --count`, `diff --cached --quiet`. The user
  never typed those and would not have, and their failures are usually answers
  rather than errors — `rev-parse --verify --quiet` fails to mean "no such ref",
  `diff --cached --quiet` exits 1 to mean "there are staged changes".

  Stated this way the rule needs no list to be maintained: a step added later
  classifies itself. `--tracked-only`'s `add -u`, `--update`'s `checkout
  <branch>` and its `gh pr view` fallback are all workflow without anyone
  deciding. `repo.restore` is the one mutating exception — it is mkprs tidying up
  after itself rather than anything the user asked for, so it contributes nothing
  here; see *cleanup can fail silently after a successful run*.

  So this bullet narrows what reaches the replay rather than emptying it.
  `toLog` sends both streams to the capture today, and that is worth keeping:
  `unable to push to origin/<branch>` is the same sentence for a protected
  branch, a rejected pre-receive hook, an expired credential, a non-fast-forward
  and an unreachable host, and only `remote: error: GH006: Protected branch
  update failed` separates them. `-v` is not the answer, because it assumes a
  re-run — after a batch that pushed twenty repos the state has moved and the
  evidence is gone.

  **The two questions have different answers.** Under `-v`, everything prints,
  bookkeeping included, because that is what asking to see mkprs work means. In
  the quiet-mode replay, only the workflow, because that is what the user would
  have seen by hand.

  **The reason line stays plain either way.** That is all the `gitError` rule
  above was protecting, and it is untouched — the complaint is replayed as a block
  under the `❌`, never folded into the sentence beside it.

  Do not reach for `--progress` on fetch/push to force the transfer meter back:
  git suppresses it off a tty for good reason, and it is noise rather than
  information.
- **`git branch -D`'s stdout is discarded** by `errToLog` as noise, but
  "Deleted branch bump-deps (was abc1234)" carries the only record of what was
  just thrown away. Under `-v` it should print.
- **The user's command goes to stderr** in --verbose mode only,
  on its own line, prefixed with `$`, immediately before it runs, so the
  output that follows has something naming what produced it. It should be
  indistinguishable from other commands in --verbose mode.

  **`{}` is expanded in what is printed**, so this is the only place the argv
  that actually ran appears. The command as typed is not the command as run, and
  it differs from repo to repo.
- **The capture moves to stderr, in both modes.** The trace lines, the internal
  git streams and the user's command all go there under `-v` — and so does the
  quiet-mode failure replay, which today prints under its `❌` on stdout. Results
  keep stdout to themselves: the `✅`/`⏭️`/`❌` lines and the summary. That makes
  `2>run.log` a complete diagnostic record to read after a failed run, while the
  console keeps only the terse half.

  The replay therefore **switches from indentation to the `[repo]` prefix**, which
  is what keeps it attributable once the streams can be split — so `indented()`
  goes, and the replay reuses the same formatting the live stream already uses.
  `newCapture` takes `a.errOut` and nothing chooses a stream by flag; see *stdout is
  the report; stderr is everything else* in [`design-decisions.md`](design-decisions.md).

#### Mark the lines that only exist because of `-v`

**Every command mkprs runs is printed to stderr, prefixed with `$`, before it
runs** — the internal git invocations, `gh`, and the user's command alike. There
is no category that runs silently under `-v`: if a process is started, the line
that started it is on screen above its output. `gitCmd.run` is the one place
every git invocation is built and started — `ok` and `text` both funnel through
it — so that is where the printing belongs rather than at each call site.

Two kinds of line, kept visually distinct, so a reader can tell mkprs's narration
from the output it is narrating:

```
[acme-web] $ git checkout -b bump-deps origin/main
[acme-web] Switched to a new branch 'bump-deps'
[acme-web] $ dotnet outdated -u
[acme-web] Analyzing acme-web.csproj...
```

The `$` marker is the shell-prompt convention for "this is the command, not its
output". Echo before running, not after, so a hang is attributable to the line
above it.

**Quote the arguments that would otherwise read as several.** Joining argv on
spaces turns `-m` `Fix typo in README` into four arguments on screen, and the
commit message defaults to the command text, so this is the common case rather
than the exotic one. POSIX single quotes are enough: nothing re-parses a trace
line, so the only requirement is that a reader can see where each argument ends.
`{}` expansion happens first — it is the whole reason two repos run different
argv, so tracing the unexpanded form would hide the only part that varies.

**Trace lines go straight to stderr, never into `c.buf`.** The buffer is what a
failure replays under `❌`, and it must keep holding exactly what the user's
command emitted — otherwise the two verbosities disagree about what the command
did. That also answers "which lines exist only because of verbosity" precisely:
the `$` ones, and they never appear in a replay. A `func (c *capture)
trace(format string, args ...any)` that no-ops unless `c.verbose` puts the
condition in one place instead of at every call site.

`capture` currently streams to `a.out`; it takes the writer as a constructor
argument, so this is `newCapture(name, a.cfg.verbose, a.errOut)` and nothing
further. Worth renaming the field off `out` at the same time, since it stops
being the same stream the reporter writes to — and `outcomeFailed` no longer
writes to `r.out` at all, so the replay is the capture's own business rather
than the reporter's.

**Do not add a replay for verbose.** A failure under `-v` already streamed
everything live; reprinting it under the `❌` would double it. `--stop-on-failure` is
the answer to "the failing repo's output is scattered among the others", and it
is a better one than a replay.

**`gh` goes to sdterr with its full argv** — and when the REST `prOpener` lands, the
method and URL in its place. That inherits the redaction constraint from
*Replace `gh`*: the trace is printed verbatim, so an `Authorization` header must
never be built into anything traceable. Worth a test that fixes this at the seam
rather than a rule to remember.

**Echo gh's stdout to the trace under `-v`**, like every other command's output.
`ghCLI.open` withholds it today so the PR URL is not printed twice, and its
comment says so — but that reason expires here. The log becomes stderr while the
URL is a result on stdout, so the two copies land on different streams: `2>`
holds it once as gh's output under its `$` line, `1>` holds it once on the `✅`
line. Only a terminal shows both at once.

gh's stdout also still goes to a buffer, because `lastLine` of it is the return
value. That has nothing to do with verbosity, and the replacement comment should
say that instead.

gh's stderr follows the rule above: to stderr under `-v`, and into the replay when
`failed to create PR` is what fails the repo. `GraphQL: A pull request already
exists` and `HTTP 403: Resource not accessible by integration` are the whole
answer, and that reason line can paraphrase neither.

Which means **`prOpener.open` should take the capture rather than an
`io.Writer`**. Only the implementation knows its own invocation, so only the
implementation can narrate it, and a plain writer gives it no way to distinguish
a trace line from the output it is narrating. Handing it the capture puts the
redaction constraint on the side of the seam that can actually honor it. The
alternative — `openPR` calling `ghArgs` itself to trace before delegating — leaks
gh's argv into the caller that exists to not know about gh.

`repo` now carries the path and the capture together, so this can be
`open(r repo, pr pullRequest) (string, error)` and drop the separate `repoPath`
argument at the same time.

Most of the filters listed above now live in `preflight`, which is a contiguous
block rather than six statements scattered through `process` — so the trace
lines land in one place.

### `--tracked-only` staging

**Medium, last of the band** — smallest feature left, `-A` against `-u`

`git add -A` stages everything the command left behind, including new files. That
is the right default (tools like `dotnet outdated -u` and scaffolders create
files), but a command that drops build artifacts in a repo with a thin
`.gitignore` will commit them. `--tracked-only` stages with `git add -u` instead.

## Repository processing

### Cleanup can fail silently after a successful run

**High, after --verbose** — a repo left somewhere the user did not put it, under a `✅` that says
everything went fine.

`repo.restore` discards both of its errors for `git checkout` and `git branch -D`
and reports success with no indication that something went wrong.

**It stays `outcomeSuccess`.** Everything the user asked for happened — the
command ran, the work was committed and pushed, the pull request exists. Cleanup
is mkprs tidying up after itself, and its failure does not retract any of that.
Nor does it get a summary counter: the three must keep summing to the repo count,
and this is not a fourth kind of outcome.

## Discovery & safety limits

The timeout is a safety net, so it **ships with a default on**, as `--max-repos`
does. A limit you have to remember to pass is absent exactly when it matters:
nobody types `--timeout` on the run where the command turns out to hang, because
they did not know it would. Opt-out, not opt-in.

### Per-repo command timeout, defaulting to 10 minutes

**High, do this first** — one `exec.CommandContext` in `runCommand`, against a run that otherwise
stalls forever with no output.

One hung command stalls the whole run with no feedback, and serial execution
means it stalls every repo behind it. Run under `exec.CommandContext` with
`context.WithTimeout`; expiry is a normal per-repo failure (`command timed out
after 10m`), so the run continues and cleanup still fires through the existing
`defer a.cleanup`. `runCommand` is the whole surface this touches: it already
returns the error `process` turns into that failure. `--timeout <duration>`
overrides, `--timeout 0` disables. 10 minutes is chosen to sit well clear of a
slow-but-real `npm ci` or `dotnet restore` while still catching a wedged process
the same afternoon; tune it once there is evidence, but do not ship it unset.

## Replace `gh` with direct GitHub API calls

**Medium** — high work and high reward: it drops the last external binary

`gh` is the last external binary mkprs needs, which undercuts the reason it was
written in Go: download one file and run it, on any platform. A user without the
GitHub CLI gets `'gh' (GitHub CLI) is not installed` and no PRs.

Opening a pull request is a few rest requests, so this costs some `net/http` calls and
an auth story — no new dependencies. **The seam already exists.** `ghCLI` implements
`prOpener` (`internal/mkprs/pr.go`); a `restAPI` implementing the same interface
drops in without touching `openPR`, and the end-to-end tests inject `fakePR`, so
they need no rewriting either. `ghCLI` then goes — see *Migration* below.

But do add new tests for the new `prOpener` implementation and continue to use
red/green TDD. Use standard golang methods to intercepts the calls to github. Do
not call out to github or create PRs as part of the test.

### Authentication

**Discover a token in this order**, first hit wins:

1. `GH_TOKEN`, then `GITHUB_TOKEN`. The CI convention, and what `gh` itself reads
   first. Actions mints a token per job, but does *not* put it in the
   environment: it is reachable as `${{ github.token }}` / `secrets.GITHUB_TOKEN`
   and the workflow has to map it (`env: GH_TOKEN: ${{ github.token }}`), the
   same line `gh` asks for. Worth stating wherever this gets documented — under
   Actions, an unmapped token means source 1 finds nothing and source 2 finds an
   unauthenticated `gh`.
2. `gh auth token` (and `gh auth token --hostname <host>` for Enterprise), if
   `gh` is on `PATH`. This inherits gh's keyring/`hosts.yml` without
   reimplementing it, so existing users need no setup — but is a convenience,
   not a requirement.
3. `git credential fill`. Write `protocol=https\nhost=<host>\n\n` to stdin and
   read `password=` back. This reaches the platform credential helper
   (osxkeychain, wincred, libsecret) that the user's `git push` already relies on.
4. Otherwise fail with an actionable message naming all three options, rather
   than a bare 401.

**First hit wins even when the API rejects it**, and that is the design rather
than an oversight: falling through to the next source on a 401 would mean a
stale `GH_TOKEN` silently deferring to a `gh` login with different permissions,
so which identity opened a PR would depend on which credential happened to be
expired. One source, chosen up front, is the version a user can reason about.

The cost is that a rejected token looks like a bare "bad credentials" with three
plausible culprits, so **the token's source is carried alongside it and named in
every auth failure** — `token from $GH_TOKEN was rejected (401); unset it to
fall back to 'gh auth token'`. That is one string field beside the token and one
line in the error, and it is the difference between a fixable message and an
afternoon. Name the source in the "no token found" case too, by listing what was
tried in the order it was tried.

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
last one is sharper than it looks: a failed repo replays its entire capture, so
anything the API layer writes there is printed verbatim. A redaction test is
worth having.

**Enterprise**: derive the host from `remote.origin.url` rather than hardcoding
`api.github.com`; GHES lives at `https://<host>/api/v3/`. `originURL` already
reads the *un-rewritten* config value, which is what makes this parseable.

### API notes

- `POST /repos/{owner}/{repo}/pulls` with `base`, `head`, `title`, `body`.
  Response `.html_url` is the line mkprs prints today.
- Reviewers are a **second** call —
  `POST /repos/{owner}/{repo}/pulls/{number}/requested_reviewers` — and labels
  and assignees would each be another, on separate issues-API endpoints
  (`POST /repos/{owner}/{repo}/issues/{number}/labels` and `…/assignees`).
  `gh pr create` hides the fan-out behind one invocation, so the extra PR fields
  get more involved here, and partial failure becomes possible: the PR exists but
  the reviewer was not added. Prefer reporting success with a warning over
  failing the repo.
- `422` usually means a PR already exists for that head. That is a skip, not a
  failure — better than the current opaque `failed to create PR`.
- Back off on `5xx` and on secondary rate limits, which arrive as **`403` or
  `429`** — both, since GitHub uses either. Honor `Retry-After` when it is
  present and fall back to exponential backoff when it is not. A 30-repo run
  trips these.

### Migration

**Replace `ghCLI` outright** — `restAPI` is the only implementation. Delete
`ghArgs` and `TestGhArgs` with it. Nothing needs proving out in parallel; if it
did, that is what a pre-release is for, not a second code path.

Keeping both would serve nobody: token source 2 is `gh auth token`, so every user
with a working `gh` already hands `restAPI` a valid token. The only users left
over are ones where `gh pr create` works but `gh auth token` will not print — and
paying for that with two code paths costs real things. The two would disagree on
behavior (`422` is a skip and a failed reviewer is a warning on the REST path;
`ghCLI` collapses both into `failed to create PR`), so what a user sees would
depend on their environment. The redaction rule above would need getting right
twice, once for argv and once for method + URL. And a fallback that only fires in
configurations that are hard to reproduce stays untested until the day it fails.

`gh` becomes an optional convenience — a place to find a token — rather than a
requirement, which is the point of this whole item.

**The seam stays.** `prOpener` earns its place from `fakePR` in the end-to-end
tests, not from having two production implementations.
