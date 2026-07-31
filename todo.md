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

- **`-d, --draft` to open draft PRs.** The natural default for a batch run: 30
  PRs opened by a script are exactly the case where you want to look before
  anyone is notified. A bool field on `pullRequest` plus one line in `ghArgs`
  (`internal/mkprs/pr.go`) — `gh pr create --draft` takes no value.

  `-d` is free (taken: `-b -m -t -B -r -v -h`).

- **`-r` accepts only one reviewer.** `gh` itself takes comma-separated
  reviewers and supports `--label`, `--assignee`, `--milestone`.
  Allow `-r alice,bob` (verify it already passes through, then document it) and
  add the rest as passthrough flags. Each becomes a field on `pullRequest` plus a
  line in `ghArgs` (`internal/mkprs/pr.go`), so a REST implementation of
  `prOpener` gets them without its own flag plumbing.

## Scale & operability

- **Bug: a command that commits its own work has that work deleted.** Live
  today, no `-i` required. `attemptRunCommand` stages with `git add -A`, then
  treats an empty index as "nothing happened":

  ```go
  if gitOK(repoPath, "diff", "--cached", "--quiet") {
      return skip("command made no changes")
  }
  ```

  A command that runs `git commit` itself leaves a clean tree and an empty
  index, so this reads as a no-op skip — and the `defer restoreRepo` then runs
  `git branch -D`, discarding the commit. `-- bash -c '... && git commit -m x'`
  is a perfectly reasonable thing to ask for, and it silently loses the work
  (recoverable from the reflog, but only if you know to look).

  Same fix as the interactive shell case below: ask whether the **branch moved**,
  not whether the index is dirty. Found by code reading, not reproduced — worth
  a failing test first.

- **`--keep-branch` / `--no-cleanup`.** The local branch is always deleted after
  the PR is opened; sometimes you want it for follow-up edits. Skips the
  `git branch -D` in `restoreRepo`.

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

  **Handle the hand-committed case by asking the branch, not the index.** If the
  user runs `git commit` inside the shell, the staging step that follows finds
  nothing to stage, skips the repo as "no changes", and `restoreRepo` then
  deletes the branch that commit was on — silently destroying deliberate work.
  This is the most damaging thing the feature can do, so **decided**: after the
  shell, test whether the working branch has moved ahead of its base
  (`git rev-list --count <base>..<branch>`, or `git diff --quiet <base>`), and if
  it has, go straight to push and PR rather than through staging. A commit is a
  commit no matter who made it; "did the tree change?" is the wrong question, and
  "did the branch move?" is the right one. This also covers a command that
  commits on its own — the same bug exists today, without `-i`.

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
  and `mkprs.go`'s type switch reads the concrete variants — so a closed set is
  now purely about keeping the reasons themselves from drifting.
- Would not close the residual gaps, which are inherent to Go: in-package code
  can still write `outcomeSkipped{}` with an empty reason, or leave the interface
  nil. The `default:` arm of the type switch in `mkprs.go` reports those loudly.

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
  exactly the commit the run made.
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
