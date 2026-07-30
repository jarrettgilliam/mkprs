#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Tests for mkprs
#
# Builds the binary, then drives it as a black box. These same assertions ran
# against the original mkprs.sh, which is what makes them a port conformance
# check rather than just a test suite.
#
# Fixtures use real local bare repos wired up through url.<...>.insteadOf, so
# fetch and push genuinely work while the origin URL still looks like GitHub.
# 'gh' is stubbed on PATH, so no PR is ever really created.
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUT="$SCRIPT_DIR/mkprs"

PASS=0
FAIL=0
FAILED_TESTS=()

# =============================================================================
# Helpers
# =============================================================================

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if [[ "$haystack" == *"$needle"* ]]; then
    PASS=$((PASS + 1))
    printf '  [PASS] %s\n' "$name"
  else
    FAIL=$((FAIL + 1))
    FAILED_TESTS+=("$name")
    printf '  [FAIL] %s\n' "$name"
    printf '         expected to contain: %s\n' "$needle"
    printf '         actual output:\n%s\n' "$haystack" | sed 's/^/         | /'
  fi
}

assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  if [[ "$haystack" != *"$needle"* ]]; then
    PASS=$((PASS + 1))
    printf '  [PASS] %s\n' "$name"
  else
    FAIL=$((FAIL + 1))
    FAILED_TESTS+=("$name")
    printf '  [FAIL] %s\n' "$name"
    printf '         expected NOT to contain: %s\n' "$needle"
  fi
}

assert_equals() {
  local name="$1" expected="$2" actual="$3"
  if [[ "$expected" == "$actual" ]]; then
    PASS=$((PASS + 1))
    printf '  [PASS] %s\n' "$name"
  else
    FAIL=$((FAIL + 1))
    FAILED_TESTS+=("$name")
    printf '  [FAIL] %s\n' "$name"
    printf '         expected: %s\n' "$expected"
    printf '         actual:   %s\n' "$actual"
  fi
}

assert_exit_code() {
  local name="$1" expected="$2" actual="$3"
  if [[ "$expected" == "$actual" ]]; then
    PASS=$((PASS + 1))
    printf '  [PASS] %s (exit %s)\n' "$name" "$actual"
  else
    FAIL=$((FAIL + 1))
    FAILED_TESTS+=("$name")
    printf '  [FAIL] %s (expected exit %s, got %s)\n' "$name" "$expected" "$actual"
  fi
}

# make_repo <workdir> <path> <name> [remote-url]
#
# With no remote URL, creates a plain repo with no origin.
# With a github.com URL, also creates a local bare repo and redirects that URL
# to it via insteadOf, so fetch/push work offline.
make_repo() {
  local workdir="$1" path="$2" name="$3" remote="${4-}"

  git init -q -b main "$path"
  git -C "$path" config user.email test@example.com
  git -C "$path" config user.name "Test User"
  echo "hello" > "$path/file.txt"
  git -C "$path" add file.txt
  git -C "$path" commit -q -m "initial commit"

  [[ -z "$remote" ]] && return 0

  git -C "$path" remote add origin "$remote"

  # Only github.com URLs get a working local remote; others exist to be skipped.
  if [[ "$remote" == *github.com* ]]; then
    mkdir -p "$workdir/remotes"
    git init -q --bare "$workdir/remotes/$name.git"
    git -C "$path" config "url.$workdir/remotes/$name.git.insteadOf" "$remote"
    git -C "$path" push -q -u origin main
    git -C "$path" remote set-head origin main
  fi
}

# Creates $1/bin/gh, logging every invocation to $1/gh.log. Exits with $2.
# 'pr create' prints a PR URL on stdout, the way the real gh does.
make_fake_gh() {
  local workdir="$1" exit_code="${2:-0}"
  mkdir -p "$workdir/bin"
  cat > "$workdir/bin/gh" << EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "$workdir/gh.log"
if [[ "\${1-}" == "pr" && "\${2-}" == "create" ]]; then
  printf 'https://github.com/fake/%s/pull/7\n' "\$(basename "\$PWD")"
fi
exit $exit_code
EOF
  chmod +x "$workdir/bin/gh"
}

# Builds a workdir containing bin/gh plus targets/, and echoes its path.
new_workspace() {
  local workdir
  workdir=$(mktemp -d)
  make_fake_gh "$workdir"
  mkdir -p "$workdir/targets"
  printf '%s' "$workdir"
}

# run_sut <workdir> <args...> -- runs mkprs with the fake gh on PATH.
run_sut() {
  local workdir="$1"; shift
  PATH="$workdir/bin:$PATH" "$SUT" "$@" 2>&1
}

gh_log() {
  cat "$1/gh.log" 2>/dev/null || true
}

# Latest commit subject on the given branch of a fixture's bare remote.
remote_subject() {
  local workdir="$1" name="$2" branch="$3"
  git -C "$workdir/remotes/$name.git" log -1 --pretty=%s "$branch" 2>/dev/null || true
}

remote_has_branch() {
  local workdir="$1" name="$2" branch="$3"
  git -C "$workdir/remotes/$name.git" rev-parse --verify --quiet "refs/heads/$branch" >/dev/null 2>&1
}

local_has_branch() {
  git -C "$1" rev-parse --verify --quiet "refs/heads/$2" >/dev/null 2>&1
}

current_branch() {
  git -C "$1" rev-parse --abbrev-ref HEAD
}

# =============================================================================
# Tests
# =============================================================================

test_help() {
  echo "test_help:"
  local out rc
  out=$("$SUT" --help 2>&1) && rc=0 || rc=$?
  assert_exit_code "exits 0" 0 "$rc"
  assert_contains "prints usage" "$out" "Usage: mkprs"
  assert_contains "documents the -- separator" "$out" "Everything after -- is the command"
}

test_missing_target_dirs() {
  echo "test_missing_target_dirs:"
  local out rc
  out=$("$SUT" -b some-branch -- true 2>&1) && rc=0 || rc=$?
  assert_exit_code "exits non-zero" 1 "$rc"
  assert_contains "errors with helpful message" "$out" "Must specify at least one target dir"
}

test_missing_branch() {
  echo "test_missing_branch:"
  local out rc
  out=$("$SUT" /tmp -- true 2>&1) && rc=0 || rc=$?
  assert_exit_code "exits non-zero" 1 "$rc"
  assert_contains "demands a branch" "$out" "-b/--branch is required"
}

test_missing_command() {
  echo "test_missing_command:"
  local out rc
  out=$("$SUT" /tmp -b some-branch 2>&1) && rc=0 || rc=$?
  assert_exit_code "exits non-zero" 1 "$rc"
  assert_contains "demands a command" "$out" "no command specified"
}

test_empty_command_after_separator() {
  echo "test_empty_command_after_separator:"
  local out rc
  out=$("$SUT" /tmp -b some-branch -- 2>&1) && rc=0 || rc=$?
  assert_exit_code "exits non-zero" 1 "$rc"
  assert_contains "treats bare -- as no command" "$out" "no command specified"
}

test_unknown_option() {
  echo "test_unknown_option:"
  local out rc
  out=$("$SUT" --bogus 2>&1) && rc=0 || rc=$?
  assert_exit_code "exits non-zero" 1 "$rc"
  assert_contains "reports unknown option" "$out" "unknown flag"
}

test_option_like_args_after_separator() {
  echo "test_option_like_args_after_separator:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/opts" "opts" "git@github.com:fake/opts.git"

  # -b after -- belongs to the command, not to mkprs.
  local out
  out=$(run_sut "$w" "$w/targets" -b flags -- bash -c 'printf "%s" "$*" > args.txt' _ -b --dry-run)
  local written
  written=$(git -C "$w/remotes/opts.git" show "flags:args.txt" 2>/dev/null || echo "MISSING")
  rm -rf "$w"
  assert_contains "does not reject command flags" "$out" "✅ opts"
  assert_equals   "passes flags through verbatim" "-b --dry-run" "$written"
}

test_runs_command_and_opens_pr() {
  echo "test_runs_command_and_opens_pr:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/good" "good" "git@github.com:fake/good.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b greet -- bash -c 'echo "hello world" > file.txt')
  local log content
  log=$(gh_log "$w")
  content=$(git -C "$w/remotes/good.git" show "greet:file.txt")
  local subject
  subject=$(remote_subject "$w" "good" "greet")
  rm -rf "$w"

  assert_contains "reports success"          "$out" "✅ good PR created"
  assert_contains "links to the PR"          "$out" "https://github.com/fake/good/pull/7"
  assert_contains "pushes the edit"          "$content" "hello world"
  assert_contains "invokes gh pr create"     "$log" "pr create"
  assert_contains "passes the branch to gh"  "$log" "--head greet"
  assert_contains "commit subject is command" "$subject" "bash -c echo \"hello world\" > file.txt"
}

test_placeholder_substitution() {
  echo "test_placeholder_substitution:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/ph" "ph" "git@github.com:fake/ph.git"
  local expected
  expected=$(realpath "$w/targets/ph")

  # shellcheck disable=SC2016  # $1 is for the inner bash -c, not this shell
  run_sut "$w" "$w/targets" -b place -- bash -c 'printf "%s" "$1" > where.txt' _ {} >/dev/null
  local written
  written=$(git -C "$w/remotes/ph.git" show "place:where.txt" 2>/dev/null || echo "MISSING")
  rm -rf "$w"
  assert_equals "{} becomes the repo abs path" "$expected" "$written"
}

test_exports_repo_env_vars() {
  echo "test_exports_repo_env_vars:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/envs" "envs" "git@github.com:fake/envs.git"
  local expected
  expected=$(realpath "$w/targets/envs")

  # shellcheck disable=SC2016  # $REPO is exported by the SUT for the inner bash -c
  run_sut "$w" "$w/targets" -b envtest -- bash -c 'printf "%s\n%s" "$REPO" "$REPO_NAME" > env.txt' >/dev/null
  local written
  written=$(git -C "$w/remotes/envs.git" show "envtest:env.txt" 2>/dev/null || echo "MISSING")
  rm -rf "$w"
  assert_equals "REPO and REPO_NAME are set" "$(printf '%s\nenvs' "$expected")" "$written"
}

test_command_runs_in_repo_root() {
  echo "test_command_runs_in_repo_root:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/cwd" "cwd" "git@github.com:fake/cwd.git"
  local expected
  expected=$(realpath "$w/targets/cwd")

  run_sut "$w" "$w/targets" -b cwdtest -- bash -c 'pwd -P > cwd.txt' >/dev/null
  local written
  written=$(git -C "$w/remotes/cwd.git" show "cwdtest:cwd.txt" 2>/dev/null || echo "MISSING")
  rm -rf "$w"
  assert_equals "cwd is the repo root" "$expected" "$written"
}

test_new_files_are_committed() {
  echo "test_new_files_are_committed:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/adds" "adds" "git@github.com:fake/adds.git"

  run_sut "$w" "$w/targets" -b addfile -- bash -c 'echo generated > brand-new.txt' >/dev/null
  local content
  content=$(git -C "$w/remotes/adds.git" show "addfile:brand-new.txt" 2>/dev/null || echo "MISSING")
  rm -rf "$w"
  assert_equals "untracked file is committed" "generated" "$content"
}

test_deletions_are_committed() {
  echo "test_deletions_are_committed:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/dels" "dels" "git@github.com:fake/dels.git"

  run_sut "$w" "$w/targets" -b delfile -- rm file.txt >/dev/null
  local listing
  listing=$(git -C "$w/remotes/dels.git" ls-tree --name-only delfile 2>/dev/null || echo "MISSING")
  rm -rf "$w"
  assert_not_contains "deleted file is gone from the tree" "$listing" "file.txt"
}

test_command_failure_skips_repo() {
  echo "test_command_failure_skips_repo:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/boom" "boom" "git@github.com:fake/boom.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b failing -- bash -c 'echo half-done > partial.txt; exit 3')
  local dangling="no" pushed="no" branch
  local_has_branch "$w/targets/boom" failing && dangling="yes"
  remote_has_branch "$w" "boom" "failing" && pushed="yes"
  branch=$(current_branch "$w/targets/boom")
  local log
  log=$(gh_log "$w")
  rm -rf "$w"

  assert_contains "reports the exit code"     "$out" "❌ boom command exited 3"
  assert_equals   "deletes the local branch"  "no" "$dangling"
  assert_equals   "pushes nothing"            "no" "$pushed"
  assert_equals   "restores default branch"   "main" "$branch"
  assert_not_contains "opens no PR"           "$log" "pr create"
}

test_noop_command_skips_repo() {
  echo "test_noop_command_skips_repo:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/noop" "noop" "git@github.com:fake/noop.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b nothing -- true)
  local dangling="no" branch
  local_has_branch "$w/targets/noop" nothing && dangling="yes"
  branch=$(current_branch "$w/targets/noop")
  local log
  log=$(gh_log "$w")
  rm -rf "$w"

  assert_contains "reports the no-op"        "$out" "⏭️  noop skipped: command made no changes"
  assert_equals   "deletes the local branch" "no" "$dangling"
  assert_equals   "restores default branch"  "main" "$branch"
  assert_not_contains "opens no PR"          "$log" "pr create"
}

test_custom_message_title_and_reviewer() {
  echo "test_custom_message_title_and_reviewer:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/meta" "meta" "git@github.com:fake/meta.git"

  run_sut "$w" "$w/targets" -b custom \
    -m "Custom commit message" -t "Custom PR title" -r reviewer-bot \
    -- bash -c 'echo changed > file.txt' >/dev/null

  local subject log
  subject=$(remote_subject "$w" "meta" "custom")
  log=$(gh_log "$w")
  rm -rf "$w"

  assert_equals   "uses custom commit message" "Custom commit message" "$subject"
  assert_contains "uses custom PR title"       "$log" "--title Custom PR title"
  assert_contains "requests the reviewer"      "$log" "--reviewer reviewer-bot"
}

test_pr_title_defaults_to_message() {
  echo "test_pr_title_defaults_to_message:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/deft" "deft" "git@github.com:fake/deft.git"

  run_sut "$w" "$w/targets" -b deftitle -m "Only a message" \
    -- bash -c 'echo changed > file.txt' >/dev/null
  local log
  log=$(gh_log "$w")
  rm -rf "$w"
  assert_contains "PR title falls back to message" "$log" "--title Only a message"
}

test_skips_non_github_remotes() {
  echo "test_skips_non_github_remotes:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/gitlab-repo" "gitlab-repo" "git@gitlab.com:fake/gitlab-repo.git"
  make_repo "$w" "$w/targets/no-remote"   "no-remote"

  local out
  out=$(run_sut "$w" "$w/targets" -b skipme -- bash -c 'echo x > file.txt')
  local log
  log=$(gh_log "$w")
  rm -rf "$w"

  assert_contains "skips repo with no remote"  "$out" "⏭️  no-remote   skipped: no 'origin' remote"
  assert_contains "skips non-github remote"    "$out" "skipped: non-GitHub remote"
  assert_not_contains "runs no command"        "$out" "✅"
  assert_not_contains "opens no PR"            "$log" "pr create"
}

test_skips_dirty_target() {
  echo "test_skips_dirty_target:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/dirty" "dirty" "git@github.com:fake/dirty.git"
  echo "uncommitted" >> "$w/targets/dirty/file.txt"

  local out
  out=$(run_sut "$w" "$w/targets" -b dirtybranch -- bash -c 'echo x > file.txt')
  local created="no"
  local_has_branch "$w/targets/dirty" dirtybranch && created="yes"
  rm -rf "$w"

  assert_contains "warns about dirty tree" "$out" "⏭️  dirty skipped: working tree not clean"
  assert_equals   "creates no branch"      "no" "$created"
}

test_skips_existing_branch() {
  echo "test_skips_existing_branch:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/has-branch" "has-branch" "git@github.com:fake/has-branch.git"
  git -C "$w/targets/has-branch" branch already-here

  local out
  out=$(run_sut "$w" "$w/targets" -b already-here -- bash -c 'echo x > file.txt')
  local content
  content=$(cat "$w/targets/has-branch/file.txt")
  rm -rf "$w"

  assert_contains "warns about existing branch" "$out" "skipped: branch 'already-here' already exists"
  assert_equals   "command never runs"          "hello" "$content"
}

test_non_main_default_branch() {
  echo "test_non_main_default_branch:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/dev-default" "dev-default" "git@github.com:fake/dev-default.git"
  # Re-point both the local repo and its bare remote at develop.
  git -C "$w/targets/dev-default" branch develop
  git -C "$w/targets/dev-default" push -q origin develop
  git -C "$w/remotes/dev-default.git" symbolic-ref HEAD refs/heads/develop
  git -C "$w/targets/dev-default" push -q origin --delete main
  git -C "$w/targets/dev-default" checkout -q develop
  git -C "$w/targets/dev-default" branch -q -D main
  git -C "$w/targets/dev-default" remote set-head origin develop
  git -C "$w/targets/dev-default" fetch -q origin --prune

  local out
  out=$(run_sut "$w" "$w/targets" -b ondev -- bash -c 'echo changed > file.txt')
  local branch
  branch=$(current_branch "$w/targets/dev-default")
  rm -rf "$w"

  assert_not_contains "no default-branch warning" "$out" "could not determine default branch"
  assert_contains     "opens the PR"              "$out" "✅ dev-default PR created"
  assert_equals       "returns to develop"        "develop" "$branch"
}

test_branch_deleted_on_push_failure() {
  echo "test_branch_deleted_on_push_failure:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/nopush" "nopush" "git@github.com:fake/nopush.git"
  # Make the bare remote unwritable so the push fails but the fetch still works.
  chmod -R a-w "$w/remotes/nopush.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b pushfail -- bash -c 'echo changed > file.txt')
  local dangling="no" branch
  local_has_branch "$w/targets/nopush" pushfail && dangling="yes"
  branch=$(current_branch "$w/targets/nopush")
  chmod -R u+w "$w/remotes/nopush.git"
  local log
  log=$(gh_log "$w")
  rm -rf "$w"

  assert_contains "warns about the push"     "$out" "❌ nopush unable to push to origin/pushfail"
  assert_equals   "leaves no dangling branch" "no" "$dangling"
  assert_equals   "restores default branch"  "main" "$branch"
  assert_not_contains "opens no PR"          "$log" "pr create"
}

test_cleanup_after_success() {
  echo "test_cleanup_after_success:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/tidy" "tidy" "git@github.com:fake/tidy.git"

  run_sut "$w" "$w/targets" -b tidybranch -- bash -c 'echo changed > file.txt' >/dev/null
  local dangling="no" branch pushed="no"
  local_has_branch "$w/targets/tidy" tidybranch && dangling="yes"
  remote_has_branch "$w" "tidy" "tidybranch" && pushed="yes"
  branch=$(current_branch "$w/targets/tidy")
  rm -rf "$w"

  assert_equals "deletes the local branch"  "no" "$dangling"
  assert_equals "keeps the pushed branch"   "yes" "$pushed"
  assert_equals "restores default branch"   "main" "$branch"
}

test_pr_failure_counts_as_failed() {
  echo "test_pr_failure_counts_as_failed:"
  local w
  w=$(new_workspace)
  make_fake_gh "$w" 1   # gh now fails
  make_repo "$w" "$w/targets/prfail" "prfail" "git@github.com:fake/prfail.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b prfailbranch -- bash -c 'echo changed > file.txt')
  rm -rf "$w"

  assert_contains "reports the PR failure" "$out" "❌ prfail failed to create PR"
  assert_contains "counts it as failed"    "$out" "Failed:    1"
}

test_processes_multiple_repos_and_dirs() {
  echo "test_processes_multiple_repos_and_dirs:"
  local w
  w=$(new_workspace)
  mkdir -p "$w/other"
  make_repo "$w" "$w/targets/one" "one" "git@github.com:fake/one.git"
  make_repo "$w" "$w/targets/two" "two" "git@github.com:fake/two.git"
  make_repo "$w" "$w/other/three" "three" "git@github.com:fake/three.git"

  local out
  out=$(run_sut "$w" "$w/targets" "$w/other" -b multi -- bash -c 'echo changed > file.txt')
  rm -rf "$w"

  assert_contains "processes first repo"        "$out" "✅ one"
  assert_contains "processes second repo"       "$out" "✅ two"
  assert_contains "processes second target dir" "$out" "✅ three"
  assert_contains "summarises the run"          "$out" "Succeeded: 3"
}

test_missing_target_dir_warns() {
  echo "test_missing_target_dir_warns:"
  local w
  w=$(new_workspace)
  local out
  out=$(run_sut "$w" "$w/does-not-exist" -b whatever -- true)
  rm -rf "$w"
  assert_contains "warns about the missing dir" "$out" "Target directory does not exist"
  assert_contains "reports nothing to do"       "$out" "No target repositories found"
}

test_quiet_on_success() {
  echo "test_quiet_on_success:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/quiet" "quiet" "git@github.com:fake/quiet.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b hush \
    -- bash -c 'echo NOISY_STDOUT; echo NOISY_STDERR >&2; echo changed > file.txt')
  rm -rf "$w"

  assert_not_contains "swallows command stdout"   "$out" "NOISY_STDOUT"
  assert_not_contains "swallows command stderr"   "$out" "NOISY_STDERR"
  assert_not_contains "drops the banner"          "$out" "=== Processing"
  assert_contains     "one result line per repo"  "$out" "✅ quiet PR created"
  assert_equals       "success is a single line"  "1" \
    "$(printf '%s\n' "$out" | grep -c '^✅')"
}

test_failure_shows_captured_output() {
  echo "test_failure_shows_captured_output:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/loud" "loud" "git@github.com:fake/loud.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b shout \
    -- bash -c 'echo CTX_STDOUT; echo CTX_STDERR >&2; exit 4')
  rm -rf "$w"

  assert_contains "reports the reason"      "$out" "❌ loud command exited 4"
  assert_contains "replays captured stdout" "$out" "    CTX_STDOUT"
  assert_contains "replays captured stderr" "$out" "    CTX_STDERR"
}

test_status_markers_are_emoji() {
  echo "test_status_markers_are_emoji:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/plain" "plain" "git@github.com:fake/plain.git"
  make_repo "$w" "$w/targets/messy" "messy" "git@github.com:fake/messy.git"
  echo "uncommitted" >> "$w/targets/messy/file.txt"

  local out
  out=$(run_sut "$w" "$w/targets" -b markers -- bash -c 'echo changed > file.txt')
  rm -rf "$w"

  assert_not_contains "emits no ANSI escapes" "$out" $'\033'
  assert_contains     "success marker is ✅"  "$out" "✅ plain"
  assert_contains     "skip marker is ⏭️"     "$out" "⏭️  messy"
}

test_result_lines_are_aligned() {
  echo "test_result_lines_are_aligned:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/ab" "ab" "git@github.com:fake/ab.git"
  make_repo "$w" "$w/targets/a-much-longer-name" "a-much-longer-name" \
    "git@github.com:fake/a-much-longer-name.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b align -- bash -c 'echo changed > file.txt')
  rm -rf "$w"

  # 'ab' is padded out to the width of the longest repo name.
  assert_contains "pads the short name" "$out" "✅ ab                 PR created"
}

test_summary_counts_all_three_states() {
  echo "test_summary_counts_all_three_states:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/win"  "win"  "git@github.com:fake/win.git"
  make_repo "$w" "$w/targets/lose" "lose" "git@github.com:fake/lose.git"
  make_repo "$w" "$w/targets/meh"  "meh"  "git@github.com:fake/meh.git"
  echo "uncommitted" >> "$w/targets/meh/file.txt"

  local out
  # shellcheck disable=SC2016  # $REPO_NAME is exported by the SUT for the inner bash -c
  out=$(run_sut "$w" "$w/targets" -b mixed -- bash -c \
    '[[ "$REPO_NAME" == lose ]] && exit 9; echo changed > file.txt')
  rm -rf "$w"

  assert_contains "counts successes" "$out" "Succeeded: 1"
  assert_contains "counts failures"  "$out" "Failed:    1"
  assert_contains "counts skips"     "$out" "Skipped:   1"
}

test_no_log_dir_without_flag() {
  echo "test_no_log_dir_without_flag:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/nolog" "nolog" "git@github.com:fake/nolog.git"

  run_sut "$w" "$w/targets" -b quiet-log -- bash -c 'echo changed > file.txt' >/dev/null
  local created="no"
  [[ -e "$w/logs" ]] && created="yes"
  rm -rf "$w"

  assert_equals "writes nothing to disk" "no" "$created"
}

test_log_dir_captures_output() {
  echo "test_log_dir_captures_output:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/logged" "logged" "git@github.com:fake/logged.git"

  run_sut "$w" "$w/targets" -b logbranch --log "$w/logs" \
    -- bash -c 'echo VERBOSE_LINE; echo changed > file.txt' >/dev/null
  local body
  body=$(cat "$w/logs/logged.log" 2>/dev/null || echo "MISSING")
  local summary_exists="no"
  [[ -f "$w/logs/summary.tsv" ]] && summary_exists="yes"
  rm -rf "$w"

  assert_equals   "creates summary.tsv"        "yes" "$summary_exists"
  assert_contains "keeps the full output"      "$body" "VERBOSE_LINE"
  assert_contains "records the resolved command" "$body" "command:  bash -c echo VERBOSE_LINE;"
  assert_contains "records the outcome"        "$body" "status:   success"
}

test_log_records_failure_output() {
  echo "test_log_records_failure_output:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/logfail" "logfail" "git@github.com:fake/logfail.git"

  run_sut "$w" "$w/targets" -b logfailbranch --log "$w/logs" \
    -- bash -c 'echo BOOM_CONTEXT >&2; exit 5' >/dev/null
  local body row
  body=$(cat "$w/logs/logfail.log" 2>/dev/null || echo "MISSING")
  row=$(grep -c $'\tfailed\t' "$w/logs/summary.tsv" || true)
  rm -rf "$w"

  assert_contains "keeps the failing output" "$body" "BOOM_CONTEXT"
  assert_contains "records the reason"       "$body" "note:     command exited 5"
  assert_equals   "marks the row failed"     "1" "$row"
}

test_summary_tsv_columns() {
  echo "test_summary_tsv_columns:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/tsv" "tsv" "git@github.com:fake/tsv.git"

  run_sut "$w" "$w/targets" -b tsvbranch --log "$w/logs" \
    -- bash -c 'echo changed > file.txt' >/dev/null
  local header fields branch url sha note
  header=$(head -n 1 "$w/logs/summary.tsv")
  fields=$(awk -F'\t' 'NR==2 {print NF}' "$w/logs/summary.tsv")
  branch=$(awk -F'\t' 'NR==2 {print $3}' "$w/logs/summary.tsv")
  sha=$(awk -F'\t' 'NR==2 {print $4}' "$w/logs/summary.tsv")
  url=$(awk -F'\t' 'NR==2 {print $5}' "$w/logs/summary.tsv")
  note=$(awk -F'\t' 'NR==2 {print $6}' "$w/logs/summary.tsv")
  rm -rf "$w"

  assert_equals "writes a header row" \
    "$(printf 'repo_path\tstatus\tbranch\tcommit_sha\tpr_url\tnotes')" "$header"
  assert_equals   "six columns per record" "6"         "$fields"
  assert_equals   "records the branch"     "tsvbranch" "$branch"
  assert_equals   "records the PR URL"     "https://github.com/fake/tsv/pull/7" "$url"
  assert_equals   "sha is a short hash"    "7" "${#sha}"
  assert_equals   "notes is empty on success" "" "$note"
}

test_log_suffixes_basename_collisions() {
  echo "test_log_suffixes_basename_collisions:"
  local w
  w=$(new_workspace)
  mkdir -p "$w/targets/a" "$w/targets/b"
  make_repo "$w" "$w/targets/a/dup" "dup-a" "git@github.com:fake/dup-a.git"
  make_repo "$w" "$w/targets/b/dup" "dup-b" "git@github.com:fake/dup-b.git"

  run_sut "$w" "$w/targets" -b dupbranch --log "$w/logs" \
    -- bash -c 'echo changed > file.txt' >/dev/null
  local first="no" second="no" rows
  [[ -f "$w/logs/dup.log" ]]   && first="yes"
  [[ -f "$w/logs/dup-2.log" ]] && second="yes"
  rows=$(awk -F'\t' 'NR>1' "$w/logs/summary.tsv" | wc -l | tr -d ' ')
  rm -rf "$w"

  assert_equals "writes the first log"      "yes" "$first"
  assert_equals "suffixes the second"       "yes" "$second"
  assert_equals "both repos in summary.tsv" "2"   "$rows"
}

test_log_equals_form() {
  echo "test_log_equals_form:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/eqlog" "eqlog" "git@github.com:fake/eqlog.git"

  run_sut "$w" "$w/targets" -b eqlogbranch "--log=$w/logs" \
    -- bash -c 'echo changed > file.txt' >/dev/null
  local created="no"
  [[ -f "$w/logs/eqlog.log" ]] && created="yes"
  rm -rf "$w"

  assert_equals "accepts --log=<dir>" "yes" "$created"
}

test_log_records_skipped_repos() {
  echo "test_log_records_skipped_repos:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/skiplog" "skiplog" "git@github.com:fake/skiplog.git"
  echo "uncommitted" >> "$w/targets/skiplog/file.txt"

  run_sut "$w" "$w/targets" -b skiplogbranch --log "$w/logs" \
    -- bash -c 'echo changed > file.txt' >/dev/null
  local status note
  status=$(awk -F'\t' 'NR==2 {print $2}' "$w/logs/summary.tsv")
  note=$(awk -F'\t' 'NR==2 {print $6}' "$w/logs/summary.tsv")
  rm -rf "$w"

  assert_equals "records skipped status" "skipped" "$status"
  assert_equals "records the reason"     "working tree not clean" "$note"
}

test_verbose_streams_output() {
  echo "test_verbose_streams_output:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/loudly" "loudly" "git@github.com:fake/loudly.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b loudbranch --verbose \
    -- bash -c 'echo LIVE_STDOUT; echo LIVE_STDERR >&2; echo changed > file.txt')
  rm -rf "$w"

  assert_contains "streams stdout prefixed"   "$out" "[loudly] LIVE_STDOUT"
  assert_contains "streams stderr prefixed"   "$out" "[loudly] LIVE_STDERR"
  assert_contains "still prints the result"   "$out" "✅ loudly PR created"
}

test_verbose_short_form() {
  echo "test_verbose_short_form:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/vshort" "vshort" "git@github.com:fake/vshort.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b vbranch -v \
    -- bash -c 'echo SHORT_FLAG; echo changed > file.txt')
  rm -rf "$w"

  assert_contains "-v behaves like --verbose" "$out" "[vshort] SHORT_FLAG"
}

test_verbose_does_not_duplicate_failure_output() {
  echo "test_verbose_does_not_duplicate_failure_output:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/dupfail" "dupfail" "git@github.com:fake/dupfail.git"

  local out count
  out=$(run_sut "$w" "$w/targets" -b dupfailbranch --verbose \
    -- bash -c 'echo ONCE_ONLY; exit 6')
  count=$(printf '%s\n' "$out" | grep -c 'ONCE_ONLY')
  rm -rf "$w"

  assert_equals   "output appears exactly once" "1" "$count"
  assert_contains "still reports the reason"    "$out" "❌ dupfail command exited 6"
}

test_verbose_composes_with_log() {
  echo "test_verbose_composes_with_log:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/both" "both" "git@github.com:fake/both.git"

  local out body
  out=$(run_sut "$w" "$w/targets" -b bothbranch --verbose --log "$w/logs" \
    -- bash -c 'echo BOTH_WAYS; echo changed > file.txt')
  body=$(cat "$w/logs/both.log" 2>/dev/null || echo "MISSING")
  rm -rf "$w"

  assert_contains "streams to the terminal" "$out"  "[both] BOTH_WAYS"
  assert_contains "and writes to the log"   "$body" "BOTH_WAYS"
}

test_quiet_is_the_default() {
  echo "test_quiet_is_the_default:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/hushed" "hushed" "git@github.com:fake/hushed.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b hushedbranch \
    -- bash -c 'echo NOT_STREAMED; echo changed > file.txt')
  rm -rf "$w"

  assert_not_contains "no prefixed lines without --verbose" "$out" "[hushed]"
  assert_not_contains "no command output"                   "$out" "NOT_STREAMED"
}

test_short_option_space_form() {
  echo "test_short_option_space_form:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/spaceform" "spaceform" "git@github.com:fake/spaceform.git"

  run_sut "$w" "$w/targets" -b my-space-branch -- bash -c 'echo changed > file.txt' >/dev/null
  local log pushed="no"
  log=$(gh_log "$w")
  remote_has_branch "$w" "spaceform" "my-space-branch" && pushed="yes"
  rm -rf "$w"

  assert_contains "assigns value from space form" "$log" "--head my-space-branch"
  assert_equals   "pushes that branch"            "yes" "$pushed"
}

test_short_option_equals_form() {
  echo "test_short_option_equals_form:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/eqform" "eqform" "git@github.com:fake/eqform.git"

  run_sut "$w" "$w/targets" -b=my-eq-branch -- bash -c 'echo changed > file.txt' >/dev/null
  local log pushed="no"
  log=$(gh_log "$w")
  remote_has_branch "$w" "eqform" "my-eq-branch" && pushed="yes"
  rm -rf "$w"

  assert_contains "assigns value from equals form" "$log" "--head my-eq-branch"
  assert_equals   "pushes that branch"             "yes" "$pushed"
}

# Discovery matches `find -type d -name .git -prune`: a repo nested inside
# another repo is still found, while a submodule-style .git *file* is not.
test_discovers_nested_repos() {
  echo "test_discovers_nested_repos:"
  local w
  w=$(new_workspace)
  make_repo "$w" "$w/targets/outer" "outer" "git@github.com:fake/outer.git"
  make_repo "$w" "$w/targets/outer/inner" "inner" "git@github.com:fake/inner.git"
  # A submodule records its git dir in a file, not a directory.
  mkdir -p "$w/targets/outer/submod"
  printf 'gitdir: ../../nowhere\n' > "$w/targets/outer/submod/.git"

  local out
  out=$(run_sut "$w" "$w/targets" -b nested -- bash -c 'echo changed > file.txt')
  rm -rf "$w"

  # The outer repo is discovered too, then skipped: the nested directories are
  # untracked content in its working tree.
  assert_contains     "finds the outer repo"  "$out" "⏭️  outer skipped: working tree not clean"
  assert_contains     "finds the nested repo" "$out" "✅ inner"
  assert_not_contains "ignores .git files"    "$out" "submod"
}

# =============================================================================
# Main
# =============================================================================

main() {
  if ! (cd "$SCRIPT_DIR" && go build -o "$SUT" .); then
    echo "Error: build failed" >&2
    exit 2
  fi

  test_help
  test_missing_target_dirs
  test_missing_branch
  test_missing_command
  test_empty_command_after_separator
  test_unknown_option
  test_option_like_args_after_separator
  test_runs_command_and_opens_pr
  test_placeholder_substitution
  test_exports_repo_env_vars
  test_command_runs_in_repo_root
  test_new_files_are_committed
  test_deletions_are_committed
  test_command_failure_skips_repo
  test_noop_command_skips_repo
  test_custom_message_title_and_reviewer
  test_pr_title_defaults_to_message
  test_skips_non_github_remotes
  test_skips_dirty_target
  test_skips_existing_branch
  test_non_main_default_branch
  test_branch_deleted_on_push_failure
  test_cleanup_after_success
  test_pr_failure_counts_as_failed
  test_processes_multiple_repos_and_dirs
  test_missing_target_dir_warns
  test_quiet_on_success
  test_failure_shows_captured_output
  test_status_markers_are_emoji
  test_result_lines_are_aligned
  test_summary_counts_all_three_states
  test_no_log_dir_without_flag
  test_log_dir_captures_output
  test_log_records_failure_output
  test_summary_tsv_columns
  test_log_suffixes_basename_collisions
  test_log_equals_form
  test_log_records_skipped_repos
  test_verbose_streams_output
  test_verbose_short_form
  test_verbose_does_not_duplicate_failure_output
  test_verbose_composes_with_log
  test_quiet_is_the_default
  test_short_option_space_form
  test_short_option_equals_form
  test_discovers_nested_repos

  echo
  echo "===================="
  echo "  Passed: $PASS"
  echo "  Failed: $FAIL"
  echo "===================="
  if [[ $FAIL -gt 0 ]]; then
    printf 'Failed tests:\n'
    printf '  - %s\n' "${FAILED_TESTS[@]}"
    exit 1
  fi
}

main "$@"
