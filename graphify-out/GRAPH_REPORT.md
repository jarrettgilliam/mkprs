# Graph Report - mkprs  (2026-08-12)

## Corpus Check
- 28 files · ~31,473 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 283 nodes · 791 edges · 11 communities (10 shown, 1 thin omitted)
- Extraction: 66% EXTRACTED · 34% INFERRED · 0% AMBIGUOUS · INFERRED: 267 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `5743616a`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- testing.T
- gitArgs
- Make --verbose actually verbose
- mkprs — domain language
- capture
- parseArgs
- helper_test.go
- pr.go
- app
- smoke_test.go
- github.com/jarrettgilliam/mkprs

## God Nodes (most connected - your core abstractions)
1. `newFixture()` - 48 edges
2. `helperCmd()` - 28 edges
3. `run()` - 27 edges
4. `gitCmd()` - 24 edges
5. `gitArgs` - 21 edges
6. `parseArgs()` - 16 edges
7. `repo` - 16 edges
8. `writeFile()` - 16 edges
9. `at()` - 15 edges
10. `capture` - 14 edges

## Surprising Connections (you probably didn't know these)
- `--tracked-only staging` --conceptually_related_to--> `The capture holds the user's workflow, not mkprs's bookkeeping`  [AMBIGUOUS]
  todo.md → design-decisions.md
- `main()` --calls--> `Run()`  [EXTRACTED]
  main.go → internal/mkprs/mkprs.go
- `GitHub REST API notes` --conceptually_related_to--> `A failure is a repo you will have to run again for`  [INFERRED]
  todo.md → design-decisions.md
- `branchAhead success/skip determination` --references--> `A failure is a repo you will have to run again for`  [EXTRACTED]
  todo.md → design-decisions.md
- `Cleanup restores the branches the repo had` --conceptually_related_to--> `A failed repo is not cleaned up at all`  [INFERRED]
  todo.md → design-decisions.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Replacing gh with the GitHub REST API** — todo_replace_gh_with_github_api, todo_token_discovery_authentication, todo_api_notes, todo_gh_migration, design_decisions_propener, design_decisions_tests_use_real_git [EXTRACTED 1.00]
- **Output stream discipline: report on stdout, capture and trace on stderr** — design_decisions_stdout_is_the_report_stderr_is_everything_else, design_decisions_capture_holds_the_users_workflow, design_decisions_execution_stays_serial, todo_make_verbose_actually_verbose, todo_mark_the_lines_that_only_exist_because_of_v, todo_delete_giterror [EXTRACTED 1.00]
- **--update branch state machine (row table, base selection, cleanup, PR create-or-update)** — todo_update_flag, todo_when_to_continue_table, todo_where_the_branch_starts_from_by_row, todo_cleanup_restores_the_branches_the_repo_had, todo_opening_versus_updating_the_pull_request, todo_branchahead, design_decisions_never_moves_branch_to_commit_it_did_not_create [EXTRACTED 1.00]

## Communities (11 total, 1 thin omitted)

### Community 0 - "testing.T"
Cohesion: 0.17
Nodes (51): testing.T, TestBranchAhead(), TestBranchLocation(), TestDefaultBranch(), TestGitErrorCarriesStderr(), TestHeadBranch(), TestIsCleanTree(), TestIsCleanTreeUnreadable() (+43 more)

### Community 1 - "gitArgs"
Cohesion: 0.12
Nodes (24): gitError(), TestGitRunStreams(), validateBranchName(), checkFormat(), checkoutBranch(), commit(), commitsAhead(), createBranch() (+16 more)

### Community 2 - "Make --verbose actually verbose"
Cohesion: 0.10
Nodes (37): The capture holds the user's workflow, not mkprs's bookkeeping, The command must leave the repo on mkprs's branch, dedupeRepos, Discovery finds only outermost, non-linked repos, Execution stays serial, A failed repo is not cleaned up at all, A failure is a repo you will have to run again for, mkprs never moves a branch to a commit it did not create (+29 more)

### Community 3 - "mkprs — domain language"
Cohesion: 0.50
Nodes (3): config, mkprs — domain language, repoRun

### Community 4 - "capture"
Cohesion: 0.07
Nodes (35): bytes.Buffer, io.Writer, newCapture(), TestCaptureIndented(), TestCaptureStreaming(), TestLoggedErrorDoesNotRepeatStderr(), stepRun(), failure() (+27 more)

### Community 5 - "parseArgs"
Cohesion: 0.14
Nodes (20): github.com/spf13/pflag.FlagSet, checkFlagValues(), firstLine(), parseArgs(), flagForms(), TestParseArgsFillsMessageAndTitle(), TestParseArgsFlagForms(), TestParseArgsFlagFormsCoversEveryFlag() (+12 more)

### Community 6 - "helper_test.go"
Cohesion: 0.16
Nodes (10): testing.M, fileURL(), helperGit(), helperGitOK(), isolateGit(), runHelper(), TestMain(), writeLines() (+2 more)

### Community 8 - "pr.go"
Cohesion: 0.18
Nodes (11): sync.Mutex, ghArgs(), lastLine(), TestGhArgs(), TestGhCLIImplementsPROpener(), TestGhCLIReportsMissingBinary(), TestLastLine(), fakePR (+3 more)

### Community 9 - "app"
Cohesion: 0.21
Nodes (9): assertEqualSlice(), dedupeRepos(), discoverRepos(), mustDiscover(), TestDedupeRepos(), TestDiscoverRepos(), TestDiscoverReposRejectsBadTargets(), plural() (+1 more)

### Community 10 - "smoke_test.go"
Cohesion: 0.62
Nodes (6): buildBinary(), exitCodeOf(), moduleRoot(), TestBinary(), TestBinaryEndToEnd(), TestBinaryExitsTwoOnFailure()

## Ambiguous Edges - Review These
- `--tracked-only staging` → `The capture holds the user's workflow, not mkprs's bookkeeping`  [AMBIGUOUS]
  todo.md · relation: conceptually_related_to

## Knowledge Gaps
- **6 isolated node(s):** `github.com/jarrettgilliam/mkprs`, `config`, `repoRun`, `dedupeRepos`, `--list to preview the repo set` (+1 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **1 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `--tracked-only staging` and `The capture holds the user's workflow, not mkprs's bookkeeping`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._
- **Why does `repo` connect `gitArgs` to `testing.T`, `capture`?**
  _High betweenness centrality (0.073) - this node is a cross-community bridge._
- **Why does `TestGitRunStreams()` connect `gitArgs` to `testing.T`, `capture`?**
  _High betweenness centrality (0.062) - this node is a cross-community bridge._
- **Why does `capture` connect `capture` to `gitArgs`?**
  _High betweenness centrality (0.061) - this node is a cross-community bridge._
- **Are the 44 inferred relationships involving `newFixture()` (e.g. with `TestDiscoverRepos()` and `TestDiscoverReposRejectsBadTargets()`) actually correct?**
  _`newFixture()` has 44 INFERRED edges - model-reasoned connections that need verification._
- **Are the 26 inferred relationships involving `helperCmd()` (e.g. with `TestRunCommandThatCommitsItsOwnWork()` and `TestRunDeduplicatesRepos()`) actually correct?**
  _`helperCmd()` has 26 INFERRED edges - model-reasoned connections that need verification._
- **Are the 23 inferred relationships involving `run()` (e.g. with `parseArgs()` and `TestRunCommandThatCommitsItsOwnWork()`) actually correct?**
  _`run()` has 23 INFERRED edges - model-reasoned connections that need verification._