# mkprs — open feature work

Each item is a heading so the list can be skimmed from GitHub's outline, and
nothing is numbered, so items can come and go without renumbering.

Decisions already settled — including the ones about what mkprs deliberately does
*not* do — live in [`design-notes.md`](design-notes.md) and are not repeated here.

## Priorities

Every item carries a band on the line under its heading, so the order is decided
once rather than re-argued each session. Sections group by subject; the band is
the second axis.

**The rules, in the order they are applied.**

1. **Bugs before features**, regardless of size. A bug written inside a feature
   item is still a bug: its own band, its own line, done first.
2. **Prerequisites above what they unblock.**
3. **Value against effort, for everything left.** The middle is judgement, and
   the line under each heading records it so it can be disagreed with.
4. **Safety nets outrank their size.** They bound a failure that lands in other
   people's repositories.

**The bands.**

- **P1** — bugs, safety nets, and cheap work another item is waiting on. Short,
  meant to be emptied, and never behind a feature. Usually small, but size does
  not buy a bug its way out.
- **P2** — the features. Most of the remaining worth and most of the work.
- **P3** — high cost against low reward, plus work deliberately parked. Not
  "never", but ahead of P2 it would be a mistake rather than merely early.

**Maintaining the bands.** Order within a band is loose; the band is the
commitment. A fixed position is stated on the item's own band line and nowhere
else. Rebanding is a normal edit — finishing an item can promote another by
clearing its prerequisite, and a P3 moves up on evidence. New items get a band
when they are added; an item with no band is one nobody has decided about yet.

## Working agreement

- Instead of picking the first item, pick an item from the list using the
  rules from the priorities section above.
- Use red/green test driven development (TDD).test Write failing tests first,
  then make them pass. This validates the test works as it should, while also
  clearly defining behavior before writing the implementation.
- Work on a single item at a time.
- If you have any questions that this file or source code doesn't clarify, ask.
  Don't make assumptions.
- When finished, update this file. Completed items are deleted rather than marked
  done then stop. I'll review, commit, and push before work starts on the next feature.
- Do not add to [`design-notes.md`](design-notes.md). These notes are
 architectural decisions that drive this TODO list. It's not a changelog.

## Flags & UX

### `-i, --interactive` review gate

**P3** — the largest item here, and `--list`, `--fail-fast` and `--update` each
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

**P2** — the band's highest value and its largest change; nothing is waiting on
it, so it follows the P1 items rather than leading them.

Today, once a run has opened its pull requests, mkprs cannot touch them again: the
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

| Row | Local Branch Exists | Remote Branch Exists | Branches on same commit?   | Continue? |
| --- | ------------------- | -------------------- | -------------------------- | --------- |
|   1 | Y                   | Y                    | Yes                        | Yes       |
|   2 | Y                   | Y                    | No                         | No        |
|   3 | Y                   | N                    | Same as the default branch | Yes       |
|   4 | Y                   | N                    | Different from default     | No        |
|   5 | N                   | Y                    | N/A                        | Yes       |
|   6 | N                   | N                    | N/A                        | Yes       |

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
`prOpener` reporting state rather than a URL. It never has to: the repo is
already skipped for needing a ref moved, so the harder question is one the table
never asks.

Both skips name the branch and say what would unblock them — delete it, or push
it — since the repo is otherwise silently absent from a run the user expected it
in.

Let's also talk about a PR that was squash-merged in a repo that does *not*
auto-delete the branch. Local and remote still agree, but neither can be
merged into the default branch, because they've diverged. `mkprs` should
check for this scenario in the `preflight` function of run.go and "skip"
if that's the case.

#### Where the branch starts from, by row

`processRepo` checks out `-b <branch> <base>` unconditionally today, and under
`--update` that stops being one thing:

- **Rows 1 and 3** — `checkout <branch>`. It exists locally and is already on the
  commit the work starts from.
- **Row 5** — create it from `origin/<branch>`, continuing the pushed branch.
- **Row 6** — create it from `origin/<default>`, exactly as today.

So `prep.base` becomes row-dependent rather than always `resolveBase`'s answer,
and this is the main structural change `--update` makes to `processRepo`.

#### On `branchAhead` functionality

`branchAhead` measures the branch against `origin/<default>`, which on a
follow-up run is already true from last time — so a command that changed nothing
would push and report success on a no-op.

Instead, we should only report success if `mkprs` pushes code or creates a PR.
If all the code and PR already exist on the server, tell the user that and skip.
If code is pushed or a PR is created, tell the user that and report success.
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
`restoreRepo` would then no-op the checkout and try to `git branch -D` the branch
it is standing on, which git refuses — and the refusal would go unseen, since
`gitErrTo`'s error is discarded. That state is unreachable today only because
`preflight` skips the repo before it gets there ("branch already exists
locally"); `--update` is exactly the change that stops skipping it. Under this
rule the delete is never attempted.

#### Opening versus updating the pull request

`openPR` has to become create-or-return-existing while also returning to the caller if
the PR was created or already existed, since that along with if anything was
pushed determine the outcome: skip or success.

Along those lines, run.go's `commitAndPush` function should be split into independent
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

**P1, after `--fail-fast`** — wide, shallow work at every git call site.

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
- **git's stderr goes to the `-v` console and nowhere else.** It must never be
  appended to a skip or fail reason. Those lines are read by people who did not
  ask how mkprs works and should not have to know: `could not compare 'b' to
  origin/main` is the message, and `fatal: ambiguous argument 'origin/main..b'`
  under it is a complaint from a `git rev-list` the user never typed, cannot see,
  and gains nothing from. Quiet mode stays plain English about what mkprs
  concluded; `-v` is where the evidence lives, under a `$` line that says which
  command produced it.

  Consequence: **delete `gitError`** as part of this item. Its only purpose is to
  fold git's first stderr line into an error so a reason line can carry it, which
  is the thing being ruled out. Nothing consumes it today either — `getDefaultBranch`
  and `branchAhead` return a `bool`, so the error dies inside the helper — and
  once `git` buffers stderr to echo it under `-v`, the console has the full text
  anyway. Keeping it would leave a helper whose only reader is a rule against
  reading it.
- **`--quiet`/`-q` on the mutating commands.** `checkout -b … --quiet`,
  `commit -q`, `push … --quiet` and `fetch … --quiet` all route through `gitTo`,
  so they would stream — the flag is what silences them. Under `-v` that costs
  commit's "3 files changed, 40 insertions", checkout's "Switched to a new
  branch", and push's `remote:` lines. Pass the quiet flags only when not
  verbose. If that becomes too cumbersome, stop passing `--quiet` altogether
  and only stream the output in --verbose mode. Otherwise ignore stdout and stderr.
  Do not reach for `--progress` on fetch/push to force the transfer
  meter back: git suppresses it off a tty for good reason, and it is noise
  rather than information.
- **`git branch -D`'s stdout is discarded** by `gitErrTo` as noise, but
  "Deleted branch bump-deps (was abc1234)" carries the only record of what was
  just thrown away. Under `-v` it should print.
- **Echo the user's command.** in the same way that `git` commands are, preceded by a `$`
  This makes the output consistent for commands in verbose mode.
- All `--verbose` output should go to stderr. That way debug logs can be redirected
  to a file for researching later if a run failed, while Keeping the console output terse.
  The redirected is optional of course, but a nice feature.

#### Mark the lines that only exist because of `-v`

Two kinds of line, kept visually distinct, so a reader can tell mkprs's narration
from the output it is narrating:

```
[acme-web] $ git checkout -b bump-deps origin/main
[acme-web] Switched to a new branch 'bump-deps'
[acme-web] $ dotnet outdated -u
[acme-web] Analyzing acme-web.csproj...
```

The `$` marker is the shell-prompt convention for "this is the command, not its
output". Echo before running, not after, so a hang is attributable to the line above it.

**Quote the arguments that would otherwise read as several.** Joining argv on
spaces turns `-m` `Fix typo in README` into four arguments on screen, and the
commit message defaults to the command text, so this is the common case rather
than the exotic one. POSIX single quotes are enough: nothing re-parses a trace
line, so the only requirement is that a reader can see where each argument ends.
`{}` expansion happens first — it is the whole reason two repos run different
argv, so tracing the unexpanded form would hide the only part that varies.

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

Which means **`prOpener.open` should take the capture rather than an
`io.Writer`**. Only the implementation knows its own invocation, so only the
implementation can narrate it, and a plain writer gives it no way to distinguish
a trace line from the output it is narrating. Handing it the capture puts the
redaction constraint on the side of the seam that can actually honour it. The
alternative — `openPR` calling `ghArgs` itself to trace before delegating — leaks
gh's argv into the caller that exists to not know about gh.

Most of the filters listed above now live in `preflight`, which is a contiguous
block rather than six statements scattered through `processRepo` — so the trace
lines land in one place.

### `--tracked-only` staging

**P2, last of the band** — smallest feature left, `-A` against `-u`

`git add -A` stages everything the command left behind, including new files. That
is the right default (tools like `dotnet outdated -u` and scaffolders create
files), but a command that drops build artifacts in a repo with a thin
`.gitignore` will commit them. `--tracked-only` stages with `git add -u` instead.

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

## Replace `gh` with direct GitHub API calls

**P2** — high work and high reward: it drops the last external binary

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
  and assignees are a **third and a fourth**, on separate issues-API endpoints
  (`POST /repos/{owner}/{repo}/issues/{number}/labels` and `…/assignees`), which
  is one more reason the note in [`design-notes.md`](design-notes.md) declines
  all three. `gh pr create` hides the fan-out behind one invocation, so the extra
  PR fields get more involved here, and partial failure becomes possible: the PR
  exists but the reviewer was not added. Prefer reporting success with a warning
  over failing the repo.
- `422` usually means a PR already exists for that head. That is a skip, not a
  failure — better than the current opaque `failed to create PR`.
- Back off on `5xx` and on secondary rate limits, which arrive as **`403` or
  `429`** — both, since GitHub uses either. Honour `Retry-After` when it is
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
