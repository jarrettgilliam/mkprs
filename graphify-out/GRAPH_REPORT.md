# Graph Report - mkprs  (2026-08-12)

## Corpus Check
- 27 files · ~31,120 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 286 nodes · 811 edges · 11 communities (10 shown, 1 thin omitted)
- Extraction: 67% EXTRACTED · 32% INFERRED · 0% AMBIGUOUS · INFERRED: 263 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `11b71419`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- testing.T
- app
- Design Notes and Roadmap
- capture
- helper_test.go
- parseArgs
- gitArgs
- TestDiscoverRepos
- End-to-End Smoke Tests
- Module Entry Point
- pr.go

## God Nodes (most connected - your core abstractions)
1. `newFixture()` - 48 edges
2. `helperCmd()` - 28 edges
3. `run()` - 27 edges
4. `gitCmd()` - 24 edges
5. `gitArgs` - 21 edges
6. `repo` - 16 edges
7. `writeFile()` - 16 edges
8. `at()` - 15 edges
9. `capture` - 14 edges
10. `parseArgs()` - 14 edges

## Surprising Connections (you probably didn't know these)
- `branchAhead rework — success only when something was pushed or created` --semantically_similar_to--> `PRs always target the repo's own default branch`  [INFERRED] [semantically similar]
  todo.md → design-decisions.md
- `Delete gitError (and TestGitErrorCarriesStderr)` --semantically_similar_to--> `stdout is the report; stderr is everything else`  [INFERRED] [semantically similar]
  todo.md → design-decisions.md
- `Refuse the default branch as the working branch` --semantically_similar_to--> `A target is a repo or a place to find them`  [INFERRED] [semantically similar]
  todo.md → design-decisions.md
- `Per-repo command timeout, defaulting to 10 minutes` --conceptually_related_to--> `A failed repo is not cleaned up at all`  [AMBIGUOUS]
  todo.md → design-decisions.md
- `main()` --calls--> `Run()`  [EXTRACTED]
  main.go → internal/mkprs/mkprs.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Replacing gh with the REST API** — todo_replace_gh_with_api, todo_token_discovery, todo_token_redaction, todo_api_notes, todo_gh_migration, design_notes_propener_seam [EXTRACTED 1.00]
- **Output stream discipline: report, capture, trace** — design_notes_stdout_is_the_report, design_notes_capture_holds_user_workflow, todo_make_verbose_verbose, todo_trace_lines, todo_delete_giterror, todo_token_redaction [EXTRACTED 1.00]
- **--update branch-state decision flow** — todo_update_flag, todo_when_to_continue_table, todo_branch_start_by_row, todo_cleanup_restores_branches, todo_opening_versus_updating_pr, design_notes_never_moves_a_branch [EXTRACTED 1.00]

## Communities (11 total, 1 thin omitted)

### Community 0 - "testing.T"
Cohesion: 0.17
Nodes (51): testing.T, TestBranchAhead(), TestBranchLocation(), TestDefaultBranch(), TestGitErrorCarriesStderr(), TestHeadBranch(), TestIsCleanTree(), TestIsCleanTreeUnreadable() (+43 more)

### Community 1 - "app"
Cohesion: 0.17
Nodes (12): stepRun(), firstLine(), plural(), commandError(), expandCommand(), newRepoRun(), resolvePath(), TestExpandCommand() (+4 more)

### Community 2 - "Design Notes and Roadmap"
Cohesion: 0.10
Nodes (44): mkprs Design Notes, The capture holds the user's workflow, not mkprs's bookkeeping, The command must leave the repo on mkprs's branch, Discovery finds only outermost, non-linked repos, Execution stays serial, A failed repo is not cleaned up at all, A failure is a repo you will have to run again for, mkprs never moves a branch to a commit it did not create (+36 more)

### Community 3 - "capture"
Cohesion: 0.08
Nodes (24): bytes.Buffer, io.Writer, newCapture(), TestCaptureIndented(), TestCaptureStreaming(), failure(), skip(), success() (+16 more)

### Community 4 - "helper_test.go"
Cohesion: 0.16
Nodes (10): testing.M, fileURL(), helperGit(), helperGitOK(), isolateGit(), runHelper(), TestMain(), writeLines() (+2 more)

### Community 5 - "parseArgs"
Cohesion: 0.15
Nodes (19): github.com/spf13/pflag.FlagSet, checkFlagValues(), parseArgs(), flagForms(), TestParseArgsFlagForms(), TestParseArgsFlagFormsCoversEveryFlag(), TestParseArgsFlagOrder(), TestParseArgsHelp() (+11 more)

### Community 6 - "gitArgs"
Cohesion: 0.12
Nodes (24): gitError(), TestGitRunStreams(), validateBranchName(), checkFormat(), checkoutBranch(), commit(), commitsAhead(), createBranch() (+16 more)

### Community 7 - "TestDiscoverRepos"
Cohesion: 0.39
Nodes (7): assertEqualSlice(), dedupeRepos(), discoverRepos(), mustDiscover(), TestDedupeRepos(), TestDiscoverRepos(), TestDiscoverReposRejectsBadTargets()

### Community 8 - "End-to-End Smoke Tests"
Cohesion: 0.62
Nodes (6): buildBinary(), exitCodeOf(), moduleRoot(), TestBinary(), TestBinaryEndToEnd(), TestBinaryExitsTwoOnFailure()

### Community 10 - "pr.go"
Cohesion: 0.17
Nodes (12): sync.Mutex, ghArgs(), lastLine(), TestGhArgs(), TestGhCLIImplementsPROpener(), TestGhCLIReportsMissingBinary(), TestLastLine(), fakePR (+4 more)

## Ambiguous Edges - Review These
- `Per-repo command timeout, defaulting to 10 minutes` → `A failed repo is not cleaned up at all`  [AMBIGUOUS]
  todo.md · relation: conceptually_related_to

## Knowledge Gaps
- **3 isolated node(s):** `github.com/jarrettgilliam/mkprs`, `--stop-on-failure`, `Split commitAndPush into commit and push`
  These have ≤1 connection - possible missing edges or undocumented components.
- **1 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `Per-repo command timeout, defaulting to 10 minutes` and `A failed repo is not cleaned up at all`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._
- **Why does `repo` connect `gitArgs` to `testing.T`, `app`, `capture`?**
  _High betweenness centrality (0.071) - this node is a cross-community bridge._
- **Why does `TestGitRunStreams()` connect `gitArgs` to `testing.T`, `capture`?**
  _High betweenness centrality (0.061) - this node is a cross-community bridge._
- **Why does `capture` connect `capture` to `app`, `gitArgs`?**
  _High betweenness centrality (0.058) - this node is a cross-community bridge._
- **Are the 44 inferred relationships involving `newFixture()` (e.g. with `TestDiscoverRepos()` and `TestDiscoverReposRejectsBadTargets()`) actually correct?**
  _`newFixture()` has 44 INFERRED edges - model-reasoned connections that need verification._
- **Are the 26 inferred relationships involving `helperCmd()` (e.g. with `TestRunCommandThatCommitsItsOwnWork()` and `TestRunDeduplicatesRepos()`) actually correct?**
  _`helperCmd()` has 26 INFERRED edges - model-reasoned connections that need verification._
- **Are the 23 inferred relationships involving `run()` (e.g. with `parseArgs()` and `TestRunCommandThatCommitsItsOwnWork()`) actually correct?**
  _`run()` has 23 INFERRED edges - model-reasoned connections that need verification._