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
  reviewers and supports `--label`, `--assignee`, `--milestone`, `--draft`.
  Allow `-r alice,bob` (verify it already passes through, then document it) and
  add the rest as passthrough flags. Each becomes a field on `pullRequest` plus a
  line in `ghArgs` (`internal/mkprs/pr.go`), so a REST implementation of
  `prOpener` gets them without its own flag plumbing.

## Scale & operability

- **Serial execution.** Across N repos with network round-trips this is
  painfully slow. `golang.org/x/sync/errgroup` with `SetLimit(N)` over
  `processRepo`, exposed as `-j/--jobs <N>` — the item that motivated leaving
  bash. `capture` already buffers per repo so results stay coherent; only
  `--verbose` interleaves. Result lines must be emitted from a single goroutine
  to stay one per line.

- **`--keep-branch` / `--no-cleanup`.** The local branch is always deleted after
  the PR is opened; sometimes you want it for follow-up edits. Skips the
  `git branch -D` in `restoreRepo`.

- **Preview mode.** `--dry-run` was removed along with patching: without a patch
  to test-apply there was nothing to preview short of running the command. Add
  `--preview`, which runs the command in a throwaway `git worktree`, prints the
  resulting diffstat, and discards it — honest that it does execute the command.
  A cheaper `--list`, printing only which repos pass the filters, may cover most
  of the old `-d` use anyway.

- **Per-repo command timeout.** One hung command stalls an entire 30-repo run
  with no feedback. Run under `exec.CommandContext` with `context.WithTimeout`
  and treat expiry as a normal per-repo failure. `--timeout <seconds>`, unset by
  default.

- **`--max-repos` safety limit.** Easy to point this at `~/repos` and
  accidentally open 200 PRs. Hard-fail before any work begins if the discovered
  set exceeds the cap.

- **Fail-fast option.** The main loop always continues after a failure. When the
  first repo fails because the command itself is wrong, you want to stop and fix
  it rather than watch 29 more failures scroll by. `--fail-fast`.

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
- **Keep `String()` collapsing every skip to `"skipped"`.** It feeds the
  `status` column of `summary.tsv` and the `status:` line of each `<repo>.log`;
  widening those values would break the `awk -F'\t' '$2=="failed"'` contract.
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
drops in without touching `processRepo`. `TestGhArgs` covers the translation for
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
built at call time — not on a command line, and not into the `capture` that
`--log` writes out. A redaction test is worth having, since the log deliberately
records everything else.

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
  expected. This is the concrete argument for `--max-repos`.

  Submodules *are* excluded, because only `.git` *directories* count: a
  submodule's `.git` is a file holding a `gitdir:` pointer. `discoverRepos`
  (`internal/mkprs/discover.go`) keys off `d.IsDir()` for exactly this reason.
