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

## Flags & UX

### `-i, --interactive` review gate

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

### `--list` to preview the repo set

Print which repos pass the filters (GitHub remote, clean tree, branch free) and
exit without running anything. Cheap, and it covers the "what would this touch?"
half of the old `--dry-run` that `-i` does not, since `-i` still runs the command
before it asks.

### `--fail-fast` to stop at the first failure

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

Easy to point this at `~/repos` and accidentally open 200 PRs. Hard-fail before
any repo is touched if discovery returns more than the cap, and make the message
the fix: `found 84 repositories, above the --max-repos limit of 50; re-run with
--max-repos 84 to proceed`. 50 clears the ~40-repo runs that are the normal case
here, so the guard stays invisible until something is genuinely wrong — which is
the only way a default like this survives contact with daily use. One flag away
when the large run is intentional, never silent when it is not. Count after
`dedupeRepos`, which `run` already applies, so a repo reached from two targets
does not spend two of the budget.

### Stop discovery at the first repo found

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

Every git helper takes `repoPath` first, which is a method receiver wearing a
disguise. A `repo` type with `r.git(…)` would delete that parameter from fifteen
signatures in `git.go` alone. Attractive, and much larger than it looks — worth
doing only if the file grows again.

### Trailing-flag robustness

`mkprs tgt -b -- true` silently takes `--` as the branch name, then fails with
"no command specified" because the terminator was consumed. pflag has no
required-value guard for this; an explicit check that no flag value starts with
`--` would give a better message.
