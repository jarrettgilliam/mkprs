# Graph Report - .  (2026-08-12)

## Corpus Check
- Corpus is ~31,120 words - fits in a single context window. You may not need a graph.

## Summary
- 279 nodes · 785 edges · 12 communities (11 shown, 1 thin omitted)
- Extraction: 66% EXTRACTED · 34% INFERRED · 0% AMBIGUOUS · INFERRED: 266 edges (avg confidence: 0.8)
- Token cost: 54,712 input · 0 output

## Community Hubs (Navigation)
- Git Behavior Tests
- Git Command Layer
- Design Decisions and Backlog
- Output Capture and Run Pipeline
- Outcome Model and Pipeline Steps
- CLI Argument Parsing
- Test Helpers and Fixtures
- Report Rendering
- PR Opening via gh
- Repo Discovery
- Binary Smoke Tests
- Module Root Package

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
- `--tracked-only staging` --conceptually_related_to--> `The capture holds the user's workflow, not mkprs's bookkeeping`  [AMBIGUOUS]
  todo.md → design-decisions.md
- `main()` --calls--> `Run()`  [EXTRACTED]
  main.go → internal/mkprs/mkprs.go
- `Refuse the default branch, before any repo is touched` --conceptually_related_to--> `PRs always target the repo's own default branch`  [INFERRED]
  todo.md → design-decisions.md
- `e — drop into a shell in the repo` --references--> `The command must leave the repo on mkprs's branch`  [EXTRACTED]
  todo.md → design-decisions.md
- `GitHub REST API notes` --conceptually_related_to--> `A failure is a repo you will have to run again for`  [INFERRED]
  todo.md → design-decisions.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **--update branch state machine (row table, base selection, cleanup, PR create-or-update)** — todo_update_flag, todo_when_to_continue_table, todo_where_the_branch_starts_from_by_row, todo_cleanup_restores_the_branches_the_repo_had, todo_opening_versus_updating_the_pull_request, todo_branchahead, design_decisions_never_moves_branch_to_commit_it_did_not_create [EXTRACTED 1.00]
- **Output stream discipline: report on stdout, capture and trace on stderr** — design_decisions_stdout_is_the_report_stderr_is_everything_else, design_decisions_capture_holds_the_users_workflow, design_decisions_execution_stays_serial, todo_make_verbose_actually_verbose, todo_mark_the_lines_that_only_exist_because_of_v, todo_delete_giterror [EXTRACTED 1.00]
- **Replacing gh with the GitHub REST API** — todo_replace_gh_with_github_api, todo_token_discovery_authentication, todo_api_notes, todo_gh_migration, design_decisions_propener, design_decisions_tests_use_real_git [EXTRACTED 1.00]

## Communities (12 total, 1 thin omitted)

### Community 0 - "Git Behavior Tests"
Cohesion: 0.19
Nodes (47): testing.T, TestBranchAhead(), TestBranchLocation(), TestDefaultBranch(), TestGitErrorCarriesStderr(), TestHeadBranch(), TestIsCleanTree(), TestIsCleanTreeUnreadable() (+39 more)

### Community 1 - "Git Command Layer"
Cohesion: 0.12
Nodes (24): gitError(), TestGitRunStreams(), validateBranchName(), checkFormat(), checkoutBranch(), commit(), commitsAhead(), createBranch() (+16 more)

### Community 2 - "Design Decisions and Backlog"
Cohesion: 0.10
Nodes (37): The capture holds the user's workflow, not mkprs's bookkeeping, The command must leave the repo on mkprs's branch, dedupeRepos, Discovery finds only outermost, non-linked repos, Execution stays serial, A failed repo is not cleaned up at all, A failure is a repo you will have to run again for, mkprs never moves a branch to a commit it did not create (+29 more)

### Community 3 - "Output Capture and Run Pipeline"
Cohesion: 0.11
Nodes (14): bytes.Buffer, newCapture(), TestCaptureIndented(), TestCaptureStreaming(), TestLoggedErrorDoesNotRepeatStderr(), stepRun(), firstLine(), plural() (+6 more)

### Community 4 - "Outcome Model and Pipeline Steps"
Cohesion: 0.19
Nodes (16): failure(), skip(), success(), TestFailureReplaysOutput(), TestNameWidthAligns(), testReporter(), TestReporterTally(), TestResultLines() (+8 more)

### Community 5 - "CLI Argument Parsing"
Cohesion: 0.15
Nodes (19): github.com/spf13/pflag.FlagSet, checkFlagValues(), parseArgs(), flagForms(), TestParseArgsFlagForms(), TestParseArgsFlagFormsCoversEveryFlag(), TestParseArgsFlagOrder(), TestParseArgsHelp() (+11 more)

### Community 6 - "Test Helpers and Fixtures"
Cohesion: 0.16
Nodes (10): testing.M, fileURL(), helperGit(), helperGitOK(), isolateGit(), runHelper(), TestMain(), writeLines() (+2 more)

### Community 7 - "Report Rendering"
Cohesion: 0.12
Nodes (10): io.Writer, nameWidth(), newReporter(), TestNameWidthMinimum(), TestSummary(), TestSummaryCountsReposNotProcessed(), outcomeFailed, outcomeSkipped (+2 more)

### Community 8 - "PR Opening via gh"
Cohesion: 0.17
Nodes (12): sync.Mutex, ghArgs(), lastLine(), TestGhArgs(), TestGhCLIImplementsPROpener(), TestGhCLIReportsMissingBinary(), TestLastLine(), fakePR (+4 more)

### Community 9 - "Repo Discovery"
Cohesion: 0.39
Nodes (7): assertEqualSlice(), dedupeRepos(), discoverRepos(), mustDiscover(), TestDedupeRepos(), TestDiscoverRepos(), TestDiscoverReposRejectsBadTargets()

### Community 10 - "Binary Smoke Tests"
Cohesion: 0.62
Nodes (6): buildBinary(), exitCodeOf(), moduleRoot(), TestBinary(), TestBinaryEndToEnd(), TestBinaryExitsTwoOnFailure()

## Ambiguous Edges - Review These
- `The capture holds the user's workflow, not mkprs's bookkeeping` → `--tracked-only staging`  [AMBIGUOUS]
  todo.md · relation: conceptually_related_to

## Knowledge Gaps
- **4 isolated node(s):** `github.com/jarrettgilliam/mkprs`, `dedupeRepos`, `Where the branch starts from, by row`, `--list to preview the repo set`
  These have ≤1 connection - possible missing edges or undocumented components.
- **1 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `The capture holds the user's workflow, not mkprs's bookkeeping` and `--tracked-only staging`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._
- **Why does `repo` connect `Git Command Layer` to `Git Behavior Tests`, `Output Capture and Run Pipeline`, `Outcome Model and Pipeline Steps`?**
  _High betweenness centrality (0.074) - this node is a cross-community bridge._
- **Why does `TestGitRunStreams()` connect `Git Command Layer` to `Git Behavior Tests`, `Output Capture and Run Pipeline`?**
  _High betweenness centrality (0.064) - this node is a cross-community bridge._
- **Why does `capture` connect `Output Capture and Run Pipeline` to `Git Command Layer`, `Outcome Model and Pipeline Steps`, `Report Rendering`?**
  _High betweenness centrality (0.061) - this node is a cross-community bridge._
- **Are the 44 inferred relationships involving `newFixture()` (e.g. with `TestDiscoverRepos()` and `TestDiscoverReposRejectsBadTargets()`) actually correct?**
  _`newFixture()` has 44 INFERRED edges - model-reasoned connections that need verification._
- **Are the 26 inferred relationships involving `helperCmd()` (e.g. with `TestRunCommandThatCommitsItsOwnWork()` and `TestRunDeduplicatesRepos()`) actually correct?**
  _`helperCmd()` has 26 INFERRED edges - model-reasoned connections that need verification._
- **Are the 23 inferred relationships involving `run()` (e.g. with `parseArgs()` and `TestRunCommandThatCommitsItsOwnWork()`) actually correct?**
  _`run()` has 23 INFERRED edges - model-reasoned connections that need verification._