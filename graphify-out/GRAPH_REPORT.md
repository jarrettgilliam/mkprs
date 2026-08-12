# Graph Report - .  (2026-08-12)

## Corpus Check
- Corpus is ~30,742 words - fits in a single context window. You may not need a graph.

## Summary
- 264 nodes · 732 edges · 10 communities (9 shown, 1 thin omitted)
- Extraction: 67% EXTRACTED · 33% INFERRED · 0% AMBIGUOUS · INFERRED: 239 edges (avg confidence: 0.8)
- Token cost: 49,969 input · 0 output

## Community Hubs (Navigation)
- Git Behavior Tests
- Output Capture and Outcomes
- Design Notes and Roadmap
- PR Opening and Reporting
- Test Fixtures and Helpers
- CLI Flag Parsing
- Git Command Layer
- Repo Discovery and App Pipeline
- End-to-End Smoke Tests
- Module Entry Point

## God Nodes (most connected - your core abstractions)
1. `newFixture()` - 47 edges
2. `helperCmd()` - 28 edges
3. `run()` - 27 edges
4. `gitCmd()` - 24 edges
5. `repo` - 19 edges
6. `writeFile()` - 16 edges
7. `at()` - 15 edges
8. `capture` - 14 edges
9. `parseArgs()` - 14 edges
10. `currentBranch()` - 14 edges

## Surprising Connections (you probably didn't know these)
- `Refuse the default branch as the working branch` --semantically_similar_to--> `A target is a repo or a place to find them`  [INFERRED] [semantically similar]
  todo.md → design-decisions.md
- `branchAhead rework — success only when something was pushed or created` --semantically_similar_to--> `PRs always target the repo's own default branch`  [INFERRED] [semantically similar]
  todo.md → design-decisions.md
- `Delete gitError (and TestGitErrorCarriesStderr)` --semantically_similar_to--> `stdout is the report; stderr is everything else`  [INFERRED] [semantically similar]
  todo.md → design-decisions.md
- `Per-repo command timeout, defaulting to 10 minutes` --conceptually_related_to--> `A failed repo is not cleaned up at all`  [AMBIGUOUS]
  todo.md → design-decisions.md
- `main()` --calls--> `Run()`  [EXTRACTED]
  main.go → internal/mkprs/mkprs.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **--update branch-state decision flow** — todo_update_flag, todo_when_to_continue_table, todo_branch_start_by_row, todo_cleanup_restores_branches, todo_opening_versus_updating_pr, design_notes_never_moves_a_branch [EXTRACTED 1.00]
- **Output stream discipline: report, capture, trace** — design_notes_stdout_is_the_report, design_notes_capture_holds_user_workflow, todo_make_verbose_verbose, todo_trace_lines, todo_delete_giterror, todo_token_redaction [EXTRACTED 1.00]
- **Replacing gh with the REST API** — todo_replace_gh_with_api, todo_token_discovery, todo_token_redaction, todo_api_notes, todo_gh_migration, design_notes_propener_seam [EXTRACTED 1.00]

## Communities (10 total, 1 thin omitted)

### Community 0 - "Git Behavior Tests"
Cohesion: 0.19
Nodes (47): testing.T, TestDiscoverRepos(), TestBranchAhead(), TestBranchLocation(), TestDefaultBranch(), TestGitErrorCarriesStderr(), TestGitHelperStreams(), TestHeadBranch() (+39 more)

### Community 1 - "Output Capture and Outcomes"
Cohesion: 0.10
Nodes (25): bytes.Buffer, newCapture(), TestCaptureIndented(), TestCaptureStreaming(), stepRun(), failure(), skip(), success() (+17 more)

### Community 2 - "Design Notes and Roadmap"
Cohesion: 0.10
Nodes (44): mkprs Design Notes, The capture holds the user's workflow, not mkprs's bookkeeping, The command must leave the repo on mkprs's branch, Discovery finds only outermost, non-linked repos, Execution stays serial, A failed repo is not cleaned up at all, A failure is a repo you will have to run again for, mkprs never moves a branch to a commit it did not create (+36 more)

### Community 3 - "PR Opening and Reporting"
Cohesion: 0.09
Nodes (19): io.Writer, TestNameWidthAligns(), ghArgs(), lastLine(), TestGhArgs(), TestGhCLIImplementsPROpener(), TestGhCLIReportsMissingBinary(), TestLastLine() (+11 more)

### Community 4 - "Test Fixtures and Helpers"
Cohesion: 0.13
Nodes (14): sync.Mutex, testing.M, fileURL(), helperGit(), helperGitOK(), isolateGit(), readFile(), runHelper() (+6 more)

### Community 5 - "CLI Flag Parsing"
Cohesion: 0.14
Nodes (20): github.com/spf13/pflag.FlagSet, checkFlagValues(), parseArgs(), assertEqualSlice(), flagForms(), TestParseArgsFlagForms(), TestParseArgsFlagFormsCoversEveryFlag(), TestParseArgsFlagOrder() (+12 more)

### Community 6 - "Git Command Layer"
Cohesion: 0.19
Nodes (5): os/exec.Cmd, gitError(), TestValidateBranchName(), validateBranchName(), repo

### Community 7 - "Repo Discovery and App Pipeline"
Cohesion: 0.20
Nodes (9): dedupeRepos(), discoverRepos(), mustDiscover(), TestDedupeRepos(), TestDiscoverReposRejectsBadTargets(), firstLine(), plural(), newRepoRun() (+1 more)

### Community 8 - "End-to-End Smoke Tests"
Cohesion: 0.62
Nodes (6): buildBinary(), exitCodeOf(), moduleRoot(), TestBinary(), TestBinaryEndToEnd(), TestBinaryExitsTwoOnFailure()

## Ambiguous Edges - Review These
- `A failed repo is not cleaned up at all` → `Per-repo command timeout, defaulting to 10 minutes`  [AMBIGUOUS]
  todo.md · relation: conceptually_related_to

## Knowledge Gaps
- **3 isolated node(s):** `github.com/jarrettgilliam/mkprs`, `--stop-on-failure`, `Split commitAndPush into commit and push`
  These have ≤1 connection - possible missing edges or undocumented components.
- **1 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **What is the exact relationship between `A failed repo is not cleaned up at all` and `Per-repo command timeout, defaulting to 10 minutes`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._
- **Why does `repo` connect `Git Command Layer` to `Git Behavior Tests`, `Output Capture and Outcomes`?**
  _High betweenness centrality (0.099) - this node is a cross-community bridge._
- **Why does `capture` connect `Output Capture and Outcomes` to `PR Opening and Reporting`, `Git Command Layer`, `Repo Discovery and App Pipeline`?**
  _High betweenness centrality (0.068) - this node is a cross-community bridge._
- **Why does `at()` connect `Git Behavior Tests` to `Test Fixtures and Helpers`, `Git Command Layer`?**
  _High betweenness centrality (0.059) - this node is a cross-community bridge._
- **Are the 43 inferred relationships involving `newFixture()` (e.g. with `TestDiscoverRepos()` and `TestDiscoverReposRejectsBadTargets()`) actually correct?**
  _`newFixture()` has 43 INFERRED edges - model-reasoned connections that need verification._
- **Are the 26 inferred relationships involving `helperCmd()` (e.g. with `TestRunCommandThatCommitsItsOwnWork()` and `TestRunDeduplicatesRepos()`) actually correct?**
  _`helperCmd()` has 26 INFERRED edges - model-reasoned connections that need verification._
- **Are the 23 inferred relationships involving `run()` (e.g. with `parseArgs()` and `TestRunCommandThatCommitsItsOwnWork()`) actually correct?**
  _`run()` has 23 INFERRED edges - model-reasoned connections that need verification._