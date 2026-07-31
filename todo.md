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

## Pull requests

- **`-r` accepts only one reviewer.** `gh` itself takes comma-separated
  reviewers and supports `--label`, `--assignee`, `--milestone`.
  Allow `-r alice,bob` (verify it already passes through, then document it) and
  add the rest as passthrough flags. Each becomes a field on `pullRequest` plus a
  line in `ghArgs` (`internal/mkprs/pr.go`), so a REST implementation of
  `prOpener` gets them without its own flag plumbing.

## Scale & operability

- **`--keep-branch` / `--no-cleanup`.** The local branch is always deleted after
  the PR is opened; sometimes you want it for follow-up edits. Skips the
  `git branch -D` in `restoreRepo`.

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
  moot — one repo's worth of output, and it is the last thing on screen.

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
  and a detached HEAD, fails that repo. Cleanup still deletes only mkprs's own
  branch, so whatever the command created survives with its commits for the user
  to pick up.

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
  | `git.go` params | `gitTo(…, w io.Writer)`, `fetchOrigin(…, w)`, `restoreRepo(…, w)` | `log` |
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
