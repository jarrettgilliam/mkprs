# mkprs — open feature work

Formerly `git-patch-apply.sh`, then `mkprs.sh`. The patch-specific design was
replaced with "run an arbitrary command in each repo"; the items that only made
sense for patching (`--source` selection, patch-hash branch names, `--3way`
apply) have been dropped rather than carried forward. The bash implementation
was ported to Go at strict parity and deleted, `test.sh` was replaced by
`go test ./...`, and `gh` now sits behind the `prOpener` interface so it can be
mocked and, later, swapped for direct API calls.

Completed items are deleted rather than marked done — `git log` is the record.
Entries are unnumbered so nothing has to be renumbered as they come and go.

## Scale & operability

- **Start from any branch, and return to it.** Today a repo not on its default
  branch is skipped (`run.go:58`). That guard exists only because cleanup hard-codes
  where to go back to — but the working branch is cut from `resolveBase`, i.e.
  origin's default, so whatever was checked out at the start never contributed to
  it. The starting branch is bookkeeping, not input.

  Two changes:

  - Drop the `head != defaultBranch` skip. Keep the `!ok` arm above it: a detached
    HEAD has no branch name to record. (Restoring one by SHA would work, but it is
    a rare enough state that skipping is the honest answer.)
  - Pass the recorded `head` to `restoreRepo` instead of `defaultBranch`, renaming
    its `dflt` parameter accordingly. It is already deferred at the one point where
    the branch starts existing, so there is no new lifetime to reason about.

  Unchanged, and worth stating so the diff stays small: the clean-tree
  requirement, `resolveBase(repoPath, defaultBranch)` as the fork point, the PR's
  base, and the guard that the command must end on mkprs's branch. Deleting only
  mkprs's own branch already means the starting branch survives to be checked out.

  Also: the `branchLocation` skip already covers a repo sitting on a branch named
  the same as `cfg.branch`, so restore cannot collide with the branch being deleted.

  Touches the *A repo must start on its default branch* design note below (delete
  it), `usageTail` in `cli.go` (it lists this among the skip reasons), and
  `TestRunRequiresDefaultBranch`, which inverts: run from a feature branch, assert
  the PR still targets the default branch and the repo ends back on the feature
  branch.

- **`-i, --interactive` review gate.** Pause in each repo after the command has
  run but before anything is staged, committed, pushed or opened, print the
  diffstat, and ask. This is `git add -p` for a batch operation, and it should
  borrow that key vocabulary rather than invent one:

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
  - **stdin is already free.** `cmd.Stdin` is never set (`run.go:103`), so the
    command runs against `/dev/null` and cannot swallow keystrokes meant for the
    prompt. Keep it that way.
  - **`n` is destructive.** Skipping falls through to the existing
    `defer restoreRepo`, which checks out the default branch and deletes the
    working branch — the command's work is gone. Say so at the prompt, and treat
    `--keep-branch` as the escape hatch for "I want to look at this properly".
  - **`a` is just a latch**, a bool that suppresses later prompts. `q` needs to
    stop the loop cleanly, letting the current repo's cleanup run.
  - Skips are reported as normal (`⏭️`), so the summary still adds up.

  ### `s` — drop into a shell in the repo

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
  changes" and have its branch deleted; `processRepo` now decides via
  `branchAhead` (`git rev-list --count <base>..<branch>`) instead of the index, so
  a commit counts no matter who made it. Nothing extra is needed for `s` beyond
  re-reading the diff.

  **`git checkout` inside the shell fails the repo**, by the same rule that
  applies to the command itself — and the shell is exactly where someone would
  reach for it. The failure is safe (the branch and its commits survive), but the
  message is written for a command, not for someone standing in a prompt. Worth
  either a clearer message under `-i` or an explicit note at the prompt that the
  branch must stay put.

  **This supersedes `--preview`**, which was going to run the command in a
  throwaway `git worktree`, print a diffstat and discard the result. That is
  strictly worse: it pays the full cost of running the command and then throws
  the work away, so a run you approve of has to be done twice. Pausing on the
  real thing gives the same look at the same diff, and lets you continue.

  **It deliberately does not touch `--max-repos` or `--timeout`.** Prompting on
  the repo count would turn a circuit breaker into a reflexive `y`, and the
  friction of re-running is what makes you read the number. Prompting on a
  timeout would replace "fail and move on" with "block forever", which is the
  failure that flag exists to prevent.

- **`--list` to preview the repo set.** Print which repos pass the filters
  (GitHub remote, clean tree, branch free) and exit without running anything.
  Cheap, and it covers the "what would this touch?" half of the old `--dry-run`
  that `-i` does not, since `-i` still runs the command before it asks.

Both of the next two are safety nets, so **both ship with a default on**. A limit
you have to remember to pass is absent exactly when it matters: nobody types
`--timeout` on the run where the command turns out to hang, because they did not
know it would. Opt-out, not opt-in.

- **Per-repo command timeout, defaulting to 10 minutes.** One hung command
  stalls the whole run with no feedback, and serial execution means it stalls
  every repo behind it. Run under `exec.CommandContext` with
  `context.WithTimeout`; expiry is a normal per-repo failure
  (`command timed out after 10m`), so the run continues and cleanup still fires
  through the existing `defer restoreRepo`. `--timeout <duration>` overrides,
  `--timeout 0` disables. 10 minutes is chosen to sit well clear of a slow-but-real
  `npm ci` or `dotnet restore` while still catching a wedged process the same
  afternoon; tune it once there is evidence, but do not ship it unset.

- **`--max-repos` safety limit, defaulting to 50.** Easy to point this at
  `~/repos` and accidentally open 200 PRs. Hard-fail before any repo is touched
  if discovery returns more than the cap, and make the message the fix:
  `found 84 repositories, above the --max-repos limit of 50; re-run with
  --max-repos 84 to proceed`. 50 clears the ~40-repo runs that are the normal
  case here, so the guard stays invisible until something is genuinely wrong —
  which is the only way a default like this survives contact with daily use. One
  flag away when the large run is intentional, never silent when it is not.
  Nested-repo discovery (see below) is the concrete way this bites: a tree can
  contain far more repos than it appears to.

- **Fail-fast option.** The main loop always continues after a failure. When the
  first repo fails because the command itself is wrong, you want to stop and fix
  it rather than watch 29 more failures scroll by. `--fail-fast`.

  This also repairs `--verbose`, which is currently worse than the default for
  diagnosing a failure: quiet prints the failing repo's output as one contiguous
  block under its `❌`, while verbose streams those bytes live mixed with every
  other repo's and skips the replay. Stopping at the first failure makes that
  moot — one repo's worth of output, and it is the last thing on screen. This is
  the fix for that, rather than teaching verbose to replay; see *Make `--verbose`
  actually verbose* below.

- **Make `--verbose` actually verbose.** Today it streams one thing — the user's
  command's two streams, prefixed `[repo]` — and nothing else. Everything mkprs
  itself does is either suppressed or routed somewhere the console never sees, so
  the flag shows the least interesting half of a run and hides the half that
  explains a skip. The rule to implement against: under `-v`, anything that would
  help when something goes wrong is on screen, as it happens.

  The gaps, in the order they bite:

  - **`git`/`gitOK` bypass the capture entirely.** `cmd.Output()` sends stdout to
    a buffer and stderr into the error, so neither stream can reach the console at
    any verbosity. That is `originURL`, `isCleanTree`, `getDefaultBranch`,
    `branchLocation`, `resolveBase`, `headBranch`, `branchAhead`, and the
    `diff --cached --quiet` check at `run.go:133` — i.e. every filter that decides
    a repo's fate. Their stdout *is* the return value, which is the argument for
    swallowing it, but it is also the answer the user wants: `status --porcelain`
    is precisely the list of files that made a repo skip as "working tree not
    clean", and today that list is unobtainable without re-running git by hand.
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

  ### Mark the lines that only exist because of `-v`

  Two kinds of line, kept visually distinct, so a reader can tell mkprs's
  narration from the output it is narrating:

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
  everything live; reprinting it under the `❌` would double it. `--fail-fast`
  above is the answer to "the failing repo's output is scattered among the
  others", and it is a better one than a replay.

  **Echo `gh` too**, with its full argv — and when the REST `prOpener` lands,
  the method and URL in its place. That inherits the redaction constraint from
  *Replace `gh`*: the trace is printed verbatim, so an `Authorization` header
  must never be built into anything traceable. Worth a test that fixes this at
  the seam rather than a rule to remember.

  **Sequencing**: after *`processRepo` is 127 lines* below, since the echo lands
  next to every step in that function's spine.

- **`--tracked-only` staging.** `git add -A` stages everything the command left
  behind, including new files. That is the right default (tools like
  `dotnet outdated -u` and scaffolders create files), but a command that drops
  build artifacts in a repo with a thin `.gitignore` will commit them.
  `--tracked-only` stages with `git add -u` instead.

## Typed skip reasons

`outcome` is a sealed sum type built by constructors that require their data, so
a skip without a reason will not compile. The reason itself is still a free-form
string.

- Replace `skip(reason string)` with a closed set, so the six skip sites name a
  constant rather than writing prose that could drift.
- Two reasons carry runtime detail — `branchLocation`'s "locally"/"on origin"
  and the offending remote URL — so a detail field has to survive alongside the
  constant. That is the wrinkle that makes this more than a mechanical swap.
- Nothing renders a status word any more — `outcome.String()` went with `--log`,
  and each variant now renders itself through `report` — so a closed set is now
  purely about keeping the reasons themselves from drifting.
- Would not close the one residual gap, which is inherent to Go: in-package code
  can still write `outcomeSkipped{}` with an empty reason. Dispatch closed the
  gap that mattered — an unhandled variant is now a compile error, where the old
  `default:` arm caught it at runtime.
- The only way left to hand `run()` a broken outcome is an explicit `return nil`
  from `processRepo`, which compiles because `nil` is a valid value of any
  interface type. `run()` substitutes a failure for it, so that mistake prints an
  internal error rather than panicking mid-run. Value receivers rule out the
  subtler version of this — a non-nil interface holding a nil pointer, where
  `res != nil` is true but the call still dereferences nil.

## Replace `gh` with direct GitHub API calls

`gh` is the last external binary mkprs needs, which undercuts the reason it was
written in Go: download one file and run it, on any platform. A user without the
GitHub CLI gets `'gh' (GitHub CLI) is not installed` and no PRs.

Opening a pull request is one `POST`, so this costs a `net/http` call and an auth
story — no new dependencies. **The seam already exists.** `ghCLI` implements
`prOpener` (`internal/mkprs/pr.go`); a `restAPI` implementing the same interface
drops in without touching `openPR`. `TestGhArgs` covers the translation for
the CLI path and end-to-end tests inject `fakePR`, so neither needs rewriting.

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

Keep both implementations and choose at startup: token found → `restAPI`; else
`gh` on `PATH` → `ghCLI`; else fail. Existing users notice nothing, users without
`gh` start working, and `ghCLI` can be deleted later once the API path has proven
itself.

## Design notes (decided, not TODO)

- **No `-c "shell string"` mode.** The command is argv after `--`, executed
  directly — no `eval`, no re-parsing. When a pipe or glob is needed, write
  `-- bash -c '...'` explicitly so the eval boundary is visible at the call site.
- **`{}` substitution over `-I`-style configurable placeholders.** `{}` matches
  `find` muscle memory, and since the command is always last there is no need for
  find's `\;` terminator.
- **CWD is the repo root** (`find -execdir` semantics, not `-exec`), so relative
  paths in commands behave the way they would if you had cd'd in yourself.
- **PRs always target the repo's own default branch.** No `--pr-base` override:
  the base and the branch's fork point have to agree for the PR's diff to be
  exactly the commit the run made. This holds even when the command supplies its
  own head branch, below.
- **A repo must start on its default branch, or it is skipped.** mkprs cuts its
  branch from there and checks it back out during cleanup, so running from a
  feature branch would silently move the user off their own branch. The skip
  names both branches; a detached HEAD skips too. *Superseded — see "Start from
  any branch, and return to it" above; this note goes when that lands.*
- **The command must leave the repo on the branch mkprs created.** Staging and
  committing act on whatever HEAD points at, so a command that runs
  `git checkout` would have its work committed to a branch mkprs does not own —
  or, if it landed on the default branch, pushed straight to `main`. Any switch,
  and a detached HEAD, fails that repo — and since a failure is not cleaned up,
  everything it created survives, mkprs's own branch included.

- **A failed repo is not cleaned up at all.** Cleanup is all or nothing: the
  checkout back to the default branch is what makes deleting the branch safe, and
  on its own it is actively harmful. mkprs's branch is cut from the default
  branch, so nothing conflicts and `git checkout` carries the command's
  uncommitted edits across with it — they end up stranded as dirty state on the
  default branch while the branch that explains them is deleted. Measured, not
  assumed: a modified file and an untracked one both follow the checkout.

  So a failure leaves branch, commits and working tree exactly as they were. The
  push failure is the case that proves it — origin has no copy, so deleting the
  branch there is the one path that genuinely destroys work — but the rule is
  uniform across every failure rather than one rule per step. The repo sitting on
  mkprs's branch is also the signal that it needs attention.

  Skips still clean up: "command made no changes" leaves nothing worth keeping.

- **`-k` skips cleanup entirely, and there is no second flag for staying on the
  branch.** Keeping the branch but checking out the default one was considered
  and dropped for the reason above: the checkout drags uncommitted edits with it,
  so "keep the branch, restore the repo" is not a coherent halfway house. Not
  having to `git checkout` is most of the point anyway — the branch is on origin
  once the PR is open, so `git checkout -b <branch> origin/<branch>` recovers a
  deleted one nearly as cheaply as `-k` avoids it, and a flag that only means
  something alongside another flag would be earning very little.

- **No `--label`, `--assignee` or `--milestone` passthrough.** `gh pr create`
  supports all three and each is a one-line addition to `ghArgs`, but they are
  not used here — so they would be flags, config fields, `pullRequest` fields and
  test rows carried indefinitely for nobody, and a REST `prOpener` would owe each
  of them a second or third API call (see *API notes*). `-r` stays because review
  requests are the one piece of PR metadata a batch run actually sets.

- **No "I did manual work, just open the PR" mode.** Considered — adopt the
  branch the repo is already on, skip branch creation, allow no command, skip
  cleanup — and dropped. `gh pr create` already infers base and head and fills
  title and body from the commits, so a shell loop over `gh pr create --fill`
  does the whole job. Inside mkprs it would cost a conditional `-b`, a new
  per-repo source for the commit message and PR title (both currently derive from
  the command text), and a second path through `processRepo` that every later
  feature would have to reason about. An implicit trigger is also a trap: a batch
  run would silently adopt a repo left on an old branch and open a PR mixing
  unrelated work. Manual edits do not scale to forty repos, which is where mkprs
  earns its keep, so the case is weakest exactly where the cost is highest.
- **No mutation testing.** Considered as a way to grade the suite, and rejected
  on the state of the tooling rather than the idea. It is a niche practice in Go
  — no large project runs it, and the most visible effort (mutation testing the
  stdlib's crypto assembly for Go 1.26) bypassed the frameworks and patched
  `cmd/asm` instead. The best available option, Gremlins, is pre-1.0 and sat
  dormant for 27 months between releases. Revisit only if something reaches 1.0
  with real adoption.
- **Execution stays serial.** `-j/--jobs` over `errgroup` was long treated as the
  headline feature — it was the stated reason for leaving bash — and has been
  dropped anyway. Most batch commands are fast enough that the wall-clock saving
  does not pay for the complexity, and the tool is worth more correct than quick
  while its behaviour is still being validated. It also costs less than it looks:
  serial runs are what let a failure replay its whole capture as one contiguous
  block, keep result lines one-per-line without a mutex, and make `--verbose`
  readable at all. Revisit only with evidence of a run that is genuinely too
  slow, not on principle.

## Lower-priority polish

- **There is no `README.md`.** The repo has `LICENSE`, `go.mod`, `main.go` and
  this file — someone landing on it from GitHub gets no idea what mkprs is, and
  `todo.md` is the closest thing to documentation, which reads as a backlog
  rather than an introduction.

  Worth covering, roughly in this order:

  - **What it is, in two sentences**, with one example that shows the whole
    shape. The `dotnet outdated -u` one from `usageTail` earns its place: run a
    command across every repo under a directory, commit, open a PR each.
  - **Install.** `go install github.com/jarrettgilliam/mkprs@latest`, plus the
    `gh` prerequisite and `gh auth login` — today a user without the GitHub CLI
    finds out by watching every repo fail. (The *Replace `gh`* section above
    removes that requirement; until it lands the README has to state it.)
  - **How it behaves per repo**: cut a branch from the default branch, run the
    command, `git add -A`, commit, push, open the PR against the default branch,
    then restore and delete the branch. The skip and failure conditions belong
    here too — that is the part `--help` states tersely and a reader actually
    needs prose for.
  - **The `{}` / `$REPO` / `$REPO_NAME` contract** and the no-shell rule.

  **Do not restate the flag table.** It lives in `usageHead` and pflag's
  generated `FlagUsages`, and a copy in the README will drift the first time a
  flag is added — every item under *Pull requests* above adds one. Link to
  `mkprs --help` and keep the README to the parts that are stable.

  Same risk applies to the examples, which is an argument for using few and
  choosing ones tied to behaviour that will not change.

  **Write this last.** Almost every open item above changes something the README
  would have to state: the flags under *Pull requests* and *Scale & operability*
  each add a line, *Start from any branch* rewrites the skip list, and *Replace
  `gh`* removes the install prerequisite entirely. Documenting the tool before
  those land means writing prose with a known expiry date. The cost of having no
  README is borne by strangers arriving from GitHub, which is not yet the
  audience; the cost of a stale one is borne by them too, and is worse, because
  it is believed.

- **Spell out names that outlive their line.** Single letters are fine for a
  local whose declaration is visible from its use. They are not fine for struct
  fields, package-level declarations, or parameters, where the reader meets the
  name far from anything that explains it. A good name deletes the comment that
  would have explained it.

  Receivers stay short (`a`, `c`, `o`, `r`) — that is Go convention, not
  laziness, and `func (c *capture)` reads fine because the type is right there.
  `Write(p []byte)` keeps its parameter name to match `io.Writer`.

  The actual offenders, all `internal/mkprs`:

  | where | now | suggested |
  |---|---|---|
  | `outcome.go` field | `outcomeFailed.c` | `output` |
  | `mkprs.go` field | `app.errw` | `errOut` |
  | `git.go` params | `gitTo(…, w io.Writer)`, `gitErrTo(…, w)`, `fetchOrigin(…, w)`, `restoreRepo(…, w)` | `log` |
  | `git.go` params | `resolveBase(repoPath, dflt)`, `restoreRepo(repoPath, dflt, …)` | `defaultBranch` |
  | `run.go` params | `processRepo(…, c *capture)`, `openPR(…, c *capture)` | `output` |
  | `cli.go` param | `printUsage(w io.Writer, fs *pflag.FlagSet)` | `out`, `flags` |

  `outcomeFailed.c` is the one that proves the point: it carries a three-line
  comment whose first job is saying what the field *is*. Named `output`, only the
  non-obvious half needs to survive — that it is still being written to when the
  outcome is built, because the deferred `restoreRepo` runs before the caller
  reports.

  `config`'s fields, the `usage*` constants and the `exit*` constants are already
  fine; this is not a sweep of everything short.

  Sequencing: `restoreRepo`'s `dflt` is also touched by *Start from any branch*
  above, which renames it for a different reason (it stops being the default
  branch at all). Do that one first and this row disappears.

- **Every git helper takes `repoPath` first**, which is a method receiver
  wearing a disguise. A `repo` type with `r.git(…)` would delete that parameter
  from fourteen signatures in `git.go` alone. Attractive, and much larger than it
  looks — worth doing only if the file grows again.

- **`processRepo` is 127 lines** (`run.go:34`). It is the only function in the
  package over 100; `parseArgs` (52, `cli.go:84`) and `app.run` (41,
  `mkprs.go:55`) are the next two and both are fine. So this is one function, not
  a sweep.

  What makes it long is that it is a pipeline of a dozen steps, each of which can
  end the repo, and the early returns are the control flow. Extraction fights
  that: a helper that can stop the pipeline has to return an `outcome`, and
  `nil`-means-carry-on reintroduces exactly the meaningful nil the *Typed skip
  reasons* section above is glad to be rid of — `run()` already has to defend
  against `processRepo` returning nil.

  So split only where a piece has one exit:

  - **`expandCommand(command []string, repoPath string) []string`** — the `{}`
    substitution loop. Pure, no outcome, and currently only reachable through a
    full end-to-end run; as a function it is a table test.
  - **The pre-flight filters** (remote, clean tree, fetch, default branch, head,
    branch free) are a contiguous block that ends either "skip" or "here is the
    base to cut from". One `outcome` return, one data return — the nil question
    confined to a single call site instead of spread across six.
  - **The deferred cleanup closure** is ~20 lines of comment and predicate now
    that failures opt out. As `func (a *app) cleanup(res outcome, …)` it can be
    tested directly rather than through a repo that fails on purpose.

  That leaves the commit → push → open spine linear, which is the part worth
  reading top to bottom.

  **Sequencing**: do this before `-i` and `--timeout`, not after. Both edit the
  middle of this function — the prompt lands between the command and the staging,
  the timeout wraps the command — and both are easier to review against a spine
  than against a 127-line body. The *Spell out names* table above also renames
  this function's `c` parameter; whichever lands second inherits a smaller diff.

- **`cli_test.go` tests one form per flag, not both.** `TestParseArgsFlagForms`
  exercises every short flag (`-b -m -t -B -r -d -k -v`) but only two long ones,
  `--branch=` and `--draft`. `--message`, `--title`, `--body`, `--reviewer`,
  `--keep-branch` and `--verbose` are never parsed by their long names in any
  test.

  What that would actually catch is narrow — a typo in the long name passed to
  `StringVarP`, or a short/long pair wired to different fields — since pflag does
  the parsing and needs no help being trusted.

  **Write it as one row per flag, not one row per form.** Each row carries the
  pair and how to read the value back, and the test generates the forms from it:
  `-b x`, `-b=x`, `--branch x`, `--branch=x` for a string; `-d`, `--draft`,
  `--draft=true` for a bool. Forgetting to cover both spellings of a flag stops
  being possible, and adding a flag costs one row instead of four, which is what
  keeps the table from turning into ceremony as the flags keep coming.

  ```go
  tests := []struct {
      long, short string
      value       string // "" for a bool
      get         func(*config) any
  }{
      {"branch", "b", "my-branch", func(c *config) any { return c.branch }},
      {"draft", "d", "", func(c *config) any { return c.draft }},
      ...
  }
  ```

  **Then close the last gap with `fs.VisitAll`.** A row per flag still has to be
  written, so a newly added flag can still arrive untested. Walking the flag set
  that `parseArgs` returns and failing on any flag with no row makes that
  impossible too — the suite breaks the moment a flag is registered without one.
  `--help` needs an explicit exemption (it returns `pflag.ErrHelp` and a nil
  config, so it cannot be driven through the same path), and that exemption list
  is the thing to keep honest: it is where a check like this quietly rots.

- **Trailing-flag robustness**: `mkprs tgt -b -- true` silently takes `--` as the
  branch name, then fails with "no command specified" because the terminator was
  consumed. pflag has no required-value guard for this; an explicit check that no
  flag value starts with `--` would give a better message.
- **Nested repo discovery**: nested repositories *are* discovered. Pruning stops
  descent into the `.git` directory itself, not into the rest of the tree, so a
  repo inside another repo is visited and both are processed. Verified against
  `~/Code`, where `CSharp/TestProjects` is a repo containing 10 nested repos and
  all 11 are returned. In practice the outer repo then skips as "working tree not
  clean", since the nested checkouts are untracked content — but it is still
  discovered, and a run against a large tree can process far more repos than
  expected. This is the concrete argument for `--max-repos` defaulting to on.

  Submodules *are* excluded, because only `.git` *directories* count: a
  submodule's `.git` is a file holding a `gitdir:` pointer. `discoverRepos`
  (`internal/mkprs/discover.go`) keys off `d.IsDir()` for exactly this reason.
