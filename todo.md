# mkprs — open feature work

Items left from the design review. The top 5 footguns (dirty-tree check,
fetch-before-branch, default-branch detection, existing-branch handling,
push-failure cleanup) are done — though "fetch-before-branch" was only true in
the sense that a fetch existed; it ran *after* the branch check until #3.

Formerly `git-patch-apply.sh`, then `mkprs.sh`. The patch-specific design was
replaced with "run an arbitrary command in each repo"; the items that only made
sense for patching (`--source` selection, patch-hash branch names, `--3way`
apply) have been dropped rather than carried forward. The bash implementation
was ported to Go at strict parity and deleted. `test.sh` — the conformance suite
that verified that port — has since been replaced by `go test ./...`, and `gh`
now sits behind the `prOpener` interface so it can be mocked and, later, swapped
for direct API calls.

## Implicit-behavior surprises

### 1. PR base branch is hardcoded to `main`

`processRepo` (`internal/mkprs/run.go`) fills `pullRequest.Base` with the literal
`"main"`. Repos with `develop`/`trunk`/etc. as their default get the wrong base.

- Reuse `defaultBranch` from the target repo as the default `Base`.
  `runCommand` already computes it — thread it through rather than recomputing.
- Add `--pr-base <branch>` flag to override.
- Now a one-field change: `Base` is already a `pullRequest` field that `ghArgs`
  passes through, and `TestGhArgs` already covers a non-`main` base.

### 2. `-r` accepts only one reviewer

`gh` itself accepts comma-separated reviewers and supports `--label`,
`--assignee`, `--milestone`, `--draft`.

- Allow `-r alice,bob` (already passes through if comma-separated — verify, document).
- Add `--label`, `--assignee`, `--milestone`, `--draft` passthrough flags.
- These become fields on `pullRequest` plus a line each in `ghArgs`
  (`internal/mkprs/pr.go`). A REST implementation of `prOpener` would then get
  them for free rather than needing its own flag plumbing.

### 3. Stale remote-tracking refs caused phantom "branch already exists" skips ✅ DONE

Merge the PRs from a run, let GitHub delete the branches, then run again with the
same `-b` name and every repo skipped:

```
⏭️  mkprs-test1 skipped: branch 'create_license' already exists
⏭️  mkprs-test2 skipped: branch 'create_license' already exists
```

The branch existed nowhere — not locally (`cleanupBranch` deletes it), not on
origin (GitHub deleted it on merge). What existed was the remote-tracking ref
`refs/remotes/origin/<branch>`, which survives until something prunes it.
`runCommand` checked the branch *before* calling `fetchOrigin`, whose `--prune`
was the very thing that would have cleared the ref.

- Move `fetchOrigin` above both `defaultBranch` and the branch check, so every
  decision reads refs that were just refreshed. `isCleanTree` stays first, so a
  dirty repo still skips without touching the network.
- A clean repo that is about to skip for branch-exists now pays a fetch. That is
  unavoidable — the remote's state cannot be known without asking — and its
  output lands in the repo's `--log` file, which is an improvement.
- `fetchOrigin` remains non-fatal: offline it warns and falls back to local refs,
  so a stale skip is still possible with no network. Accepted tradeoff.
- `branchExists` became `branchLocation`, returning *where* the branch was found,
  so the skip reads `already exists locally` or `already exists on origin`. The
  local case is the one reordering cannot fix — a run killed mid-flight (or, once
  #8 lands, `--keep-branch`) leaves a local branch that otherwise skips forever
  with no hint why.
- `test_stale_remote_branch_is_pruned` pins the regression; it fails against the
  pre-fix binary. `test_skips_branch_existing_on_origin` covers the split message.

## Output & logging

### 4. Clean default output — one line per repo ✅ DONE

Today every repo prints a `=== Processing: X ===` banner, the command's own
stdout/stderr streams straight through uncaptured, and `[SUCCESS] Created PR for
X` omits the one thing worth reading — the PR URL. A 30-repo `npm`/`dotnet` run
buries its result under thousands of lines.

Target:

```
✅ acme-web   PR created  https://github.com/me/acme-web/pull/42
✅ acme-api   PR created  https://github.com/me/acme-api/pull/17
❌ acme-cli   command exited 1
    npm ERR! code ELIFECYCLE
    npm ERR! lint failed
⏭️  acme-docs  skipped: working tree not clean
```

- Capture the command's stdout+stderr to a temp file instead of letting it
  through — redirect `"${expanded[@]}"` in `run_command`. Discard on success;
  on failure print the reason line, then the tail of the capture, indented.
- Drop the `=== Processing: X ===` banner. The per-repo result line replaces it,
  as do the `[WARN]`/`[FAIL]`/`[SKIP]` prefixes.
- Capture `gh pr create`'s stdout rather than `>/dev/null` — it prints the PR
  URL, which is what the success line needs.
- Three states, not two: success (✅), failure (❌), and skipped (⏭️) for the
  pre-flight filters (non-GitHub remote, dirty tree, existing branch, no-op
  command) that aren't really errors.
- Emoji markers rather than ANSI color, so the status reads the same piped to a
  file as it does on a terminal and there is no TTY/`NO_COLOR` branch to test.
- `print_summary` stays as the closing tally.

### 5. `--log <dir>` for verbose logs and an audit trail ✅ DONE

When a 30-repo run goes sideways you want to know which repo got which SHA, which
PR URL, which failure — and to read the full output that the default view elides.

```
<dir>/
├── summary.tsv
├── acme-web.log
└── acme-cli.log
```

- `<repo>.log` — everything: the resolved command, its full output, git and gh
  output, the outcome. This is the temp-file capture from #4 written to a real
  path instead of discarded.
- `summary.tsv` — one tab-separated record per repo:
  `repo_path<TAB>status<TAB>branch<TAB>commit_sha<TAB>pr_url<TAB>notes`.
  Greppable in aggregate (`awk -F'\t' '$2=="failed"'`) in a way that a directory
  of per-repo logs is not.
- Created with `mkdir -p` only when the flag is passed. No default location and
  no writes to `~/.local/state` — absent `--log`, nothing touches the disk.
- Watch the basename collision: two repos with the same directory name under
  different targets would clobber each other's `.log`. Suffix on conflict.

### 6. `--verbose` ✅ DONE

Streams command output live, prefixed with the repo name, instead of buffering it:

```
[acme-cli] npm ERR! code ELIFECYCLE
[acme-cli] npm ERR! lint failed
❌ acme-cli   command exited 1
```

- Composes with `--log` — stream *and* write.
- Interleaves unreadably once #7 runs repos in parallel. That's expected, and is
  the reason this is a flag rather than the default.

## Scale & operability

### 7. Serial execution

Across N repos with network round-trips this is painfully slow.

- `golang.org/x/sync/errgroup` with `SetLimit(N)` over `processRepo`, exposed as
  `-j/--jobs <N>`. This is the item that motivated leaving bash.
- Depends on #4: per-repo output capture is a prerequisite, since live
  interleaved stderr is unreadable at more than one job. `capture` already
  buffers per repo, so results stay coherent; only `--verbose` interleaves.
- Result lines must be emitted from a single goroutine to stay one-per-line.

### 8. `--keep-branch` / `--no-cleanup`

`cleanupBranch` always deletes the local branch after the PR is opened.
Sometimes you want it for follow-up edits.

- Add `--keep-branch` to skip the `git branch -D` step.

### 9. Preview mode

`--dry-run` was removed along with patching: without a patch to test-apply there
was nothing meaningful to preview short of running the command.

- Add `--preview`, which runs the command in a throwaway `git worktree`, prints
  the resulting diffstat, and discards it. Genuinely useful, and honest that it
  does execute the command.
- A cheaper `--list` that only prints which repos pass the filters (GitHub
  remote, clean tree, branch free) may cover most of the old `-d` use anyway.

### 10. Per-repo command timeout

One hung command currently stalls an entire 30-repo run with no feedback.

- Run the command with `exec.CommandContext` under a `context.WithTimeout`;
  treat expiry as a normal per-repo failure.
- Expose via `--timeout <seconds>`, unset by default.

### 11. `--max-repos` safety limit

Easy to point this at `~/repos` and accidentally open 200 PRs.

- Add `--max-repos <N>` (default unlimited or e.g. 25 to be safe).
- Hard-fail before any work begins if the discovered set exceeds the cap.

### 12. Fail-fast option

The main loop always continues to the next repo after a failure. When the first
repo fails because the command itself is wrong, you usually want to stop and fix
it rather than watch 29 more failures scroll by.

- Add `--fail-fast` to abort the run on the first per-repo failure.

## Dependencies

### 13. Replace `gh` with direct GitHub API calls

`gh` is the last external binary mkprs needs, which undercuts the reason it was
written in Go: download one file and run it, on any platform. A user without the
GitHub CLI gets `'gh' (GitHub CLI) is not installed` and no PRs.

Opening a pull request is one `POST`, so this costs a `net/http` call and an auth
story — no new dependencies.

**The seam already exists.** `ghCLI` implements `prOpener`
(`internal/mkprs/pr.go`); a `restAPI` implementing the same interface drops in
without touching `processRepo`. `TestGhArgs` covers the translation for the CLI
path, and end-to-end tests inject `fakePR`, so neither needs rewriting.

#### Authentication

**Discover a token in this order**, first hit wins:

1. `GH_TOKEN`, then `GITHUB_TOKEN`. The CI convention, and what `gh` itself reads
   first. Actions provides `GITHUB_TOKEN` automatically.
2. `gh auth token` (and `gh auth token --hostname <host>` for Enterprise), if
   `gh` is on `PATH`. This inherits gh's keyring/`hosts.yml` without
   reimplementing it, so existing users need no setup — but is a convenience,
   not a requirement.
3. `git credential fill`. Write `protocol=https\nhost=<host>\n\n` to stdin and
   read `password=` back. This reaches the platform credential helper
   (osxkeychain, wincred, libsecret) that the user's `git push` already relies
   on.
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
`api.github.com`; GHES lives at `https://<host>/api/v3/`. Note that `originURL`
already reads the *un-rewritten* config value, which is what makes this parseable.

#### API notes

- `POST /repos/{owner}/{repo}/pulls` with `base`, `head`, `title`, `body`.
  Response `.html_url` is the line mkprs prints today.
- Reviewers are a **second** call —
  `POST /repos/{owner}/{repo}/pulls/{number}/requested_reviewers` — and labels
  and assignees a third, via the issues API. `gh pr create` hides this behind one
  invocation, so #2 gets more involved here, and partial failure becomes possible:
  the PR exists but the reviewer was not added. Prefer reporting success with a
  warning over failing the repo.
- `422` usually means a PR already exists for that head. That is a skip, not a
  failure — better than the current opaque `failed to create PR`.
- Back off on `403` secondary rate limits and `5xx`; a 30-repo run trips these.

#### Migration

Keep both implementations and choose at startup: token found → `restAPI`; else
`gh` on `PATH` → `ghCLI`; else fail. Existing users notice nothing, users without
`gh` start working, and `ghCLI` can be deleted later once the API path has
proven itself.

## Smaller items

### 14. `--tracked-only` staging

`git add -A` stages everything the command left behind, including new files.
That is the right default (tools like `dotnet outdated -u` and scaffolders
create files), but a command that drops build artifacts in a repo with a thin
`.gitignore` will commit them.

- Add `--tracked-only` to stage with `git add -u` instead.

## Design notes (decided, not TODO)

- **No `-c "shell string"` mode.** The command is argv after `--`, executed
  directly — no `eval`, no re-parsing. When a pipe or glob is needed, write
  `-- bash -c '...'` explicitly so the eval boundary is visible at the call site.
- **`{}` substitution over `-I`-style configurable placeholders.** `{}` matches
  `find` muscle memory, and since the command is always last there is no need for
  find's `\;` terminator.
- **CWD is the repo root** (`find -execdir` semantics, not `-exec`), so relative
  paths in commands behave the way they would if you had cd'd in yourself.

## Lower-priority polish

- **Trailing-flag robustness**: `mkprs tgt -b -- true` silently takes `--` as the
  branch name, then fails with "no command specified" because the terminator was
  consumed. pflag has no required-value-guard for this; an explicit check that
  no flag value starts with `--` would give a better message. Still reproducible
  after the Go port.
- **Nested repo discovery** (corrected — the earlier note here was wrong):
  nested repositories *are* discovered. Pruning stops descent into the `.git`
  directory itself, not into the rest of the tree, so a repo inside another repo
  is visited and both are processed. Verified against `~/Code`, where
  `CSharp/TestProjects` is a repo containing 10 nested repos and all 11 are
  returned. In practice the outer repo then skips as "working tree not clean",
  since the nested checkouts are untracked content — but it is still discovered,
  and a run against a large tree can process far more repos than expected. This
  is the concrete argument for #11 (`--max-repos`).

  Submodules *are* excluded, because only `.git` *directories* count: a
  submodule's `.git` is a file holding a `gitdir:` pointer. `discoverRepos`
  (`internal/mkprs/discover.go`) keys off `d.IsDir()` for exactly this reason,
  and `test_discovers_nested_repos` pins
  them.
