# mkprs — open feature work

Items left from the design review. The top 5 footguns (dirty-tree check,
fetch-before-branch, default-branch detection, existing-branch handling,
push-failure cleanup) are done.

Formerly `git-patch-apply.sh`, then `mkprs.sh`. The patch-specific design was
replaced with "run an arbitrary command in each repo"; the items that only made
sense for patching (`--source` selection, patch-hash branch names, `--3way`
apply) have been dropped rather than carried forward. The bash implementation
was ported to Go at strict parity and deleted; the suite in `test.sh` is the
same one that verified the port.

## Implicit-behavior surprises

### 1. PR base branch is hardcoded to `main`

`createPR` (`internal/mkprs/pr.go`) passes `gh pr create --base main`. Repos with
`develop`/`trunk`/etc. as their default get the wrong base.

- Reuse `defaultBranch` from the target repo as the default `--base`.
  `runCommand` already computes it — thread it through to `createPR` rather
  than recomputing.
- Add `--pr-base <branch>` flag to override.

### 2. `-r` accepts only one reviewer

`gh` itself accepts comma-separated reviewers and supports `--label`,
`--assignee`, `--milestone`, `--draft`.

- Allow `-r alice,bob` (already passes through if comma-separated — verify, document).
- Add `--label`, `--assignee`, `--milestone`, `--draft` passthrough flags.

## Output & logging

### 3. Clean default output — one line per repo ✅ DONE

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

### 4. `--log <dir>` for verbose logs and an audit trail ✅ DONE

When a 30-repo run goes sideways you want to know which repo got which SHA, which
PR URL, which failure — and to read the full output that the default view elides.

```
<dir>/
├── summary.tsv
├── acme-web.log
└── acme-cli.log
```

- `<repo>.log` — everything: the resolved command, its full output, git and gh
  output, the outcome. This is the temp-file capture from #3 written to a real
  path instead of discarded.
- `summary.tsv` — one tab-separated record per repo:
  `repo_path<TAB>status<TAB>branch<TAB>commit_sha<TAB>pr_url<TAB>notes`.
  Greppable in aggregate (`awk -F'\t' '$2=="failed"'`) in a way that a directory
  of per-repo logs is not.
- Created with `mkdir -p` only when the flag is passed. No default location and
  no writes to `~/.local/state` — absent `--log`, nothing touches the disk.
- Watch the basename collision: two repos with the same directory name under
  different targets would clobber each other's `.log`. Suffix on conflict.

### 5. `--verbose` ✅ DONE

Streams command output live, prefixed with the repo name, instead of buffering it:

```
[acme-cli] npm ERR! code ELIFECYCLE
[acme-cli] npm ERR! lint failed
❌ acme-cli   command exited 1
```

- Composes with `--log` — stream *and* write.
- Interleaves unreadably once #6 runs repos in parallel. That's expected, and is
  the reason this is a flag rather than the default.

## Scale & operability

### 6. Serial execution

Across N repos with network round-trips this is painfully slow.

- `golang.org/x/sync/errgroup` with `SetLimit(N)` over `processRepo`, exposed as
  `-j/--jobs <N>`. This is the item that motivated leaving bash.
- Depends on #3: per-repo output capture is a prerequisite, since live
  interleaved stderr is unreadable at more than one job. `capture` already
  buffers per repo, so results stay coherent; only `--verbose` interleaves.
- Result lines must be emitted from a single goroutine to stay one-per-line.

### 7. `--keep-branch` / `--no-cleanup`

`cleanupBranch` always deletes the local branch after the PR is opened.
Sometimes you want it for follow-up edits.

- Add `--keep-branch` to skip the `git branch -D` step.

### 8. Preview mode

`--dry-run` was removed along with patching: without a patch to test-apply there
was nothing meaningful to preview short of running the command.

- Add `--preview`, which runs the command in a throwaway `git worktree`, prints
  the resulting diffstat, and discards it. Genuinely useful, and honest that it
  does execute the command.
- A cheaper `--list` that only prints which repos pass the filters (GitHub
  remote, clean tree, branch free) may cover most of the old `-d` use anyway.

### 9. Per-repo command timeout

One hung command currently stalls an entire 30-repo run with no feedback.

- Run the command with `exec.CommandContext` under a `context.WithTimeout`;
  treat expiry as a normal per-repo failure.
- Expose via `--timeout <seconds>`, unset by default.

### 10. `--max-repos` safety limit

Easy to point this at `~/repos` and accidentally open 200 PRs.

- Add `--max-repos <N>` (default unlimited or e.g. 25 to be safe).
- Hard-fail before any work begins if the discovered set exceeds the cap.

### 11. Fail-fast option

The main loop always continues to the next repo after a failure. When the first
repo fails because the command itself is wrong, you usually want to stop and fix
it rather than watch 29 more failures scroll by.

- Add `--fail-fast` to abort the run on the first per-repo failure.

## Smaller items

### 12. `--tracked-only` staging

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
  is the concrete argument for #10 (`--max-repos`).

  Submodules *are* excluded, because only `.git` *directories* count: a
  submodule's `.git` is a file holding a `gitdir:` pointer. `discoverRepos`
  (`internal/mkprs/discover.go`) keys off `d.IsDir()` for exactly this reason,
  and `test_discovers_nested_repos` pins
  them.
