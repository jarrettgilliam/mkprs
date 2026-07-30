#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Variables
# =============================================================================
TARGET_DIRS=()
BRANCH_NAME=""
COMMIT_MESSAGE=""
PR_TITLE=""
PR_BODY=""
PR_REVIEWER=""
LOG_DIR=""
VERBOSE=0
declare -a COMMAND=()
declare -a ALL_REPOS=()
PROCESSED=0
FAILED=0
SKIPPED=0

# Output state. CAPTURE_FILE collects everything a repo's command, git and gh
# emit; NOTE_FILE carries the one-line reason back out of run_command's subshell.
NAME_WIDTH=1
CAPTURE_FILE=""
NOTE_FILE=""
FAIL_TAIL_LINES=20
SUMMARY_FILE=""

# Per-repo facts worth recording, reset by reset_repo_state.
RESOLVED_COMMAND=""
COMMIT_SHA=""

# =============================================================================
# Functions
# =============================================================================

init_output() {
  CAPTURE_FILE=$(mktemp)
  NOTE_FILE=$(mktemp)
  trap 'rm -f "$CAPTURE_FILE" "$NOTE_FILE"' EXIT

  # Nothing touches the disk unless --log was given.
  [[ -z "$LOG_DIR" ]] && return 0

  if ! mkdir -p "$LOG_DIR"; then
    echo "Error: could not create log directory: $LOG_DIR" >&2
    exit 1
  fi
  SUMMARY_FILE="$LOG_DIR/summary.tsv"
  printf 'repo_path\tstatus\tbranch\tcommit_sha\tpr_url\tnotes\n' > "$SUMMARY_FILE"
}

# The reason a repo ended up where it did, written from wherever it is known
# (including run_command's subshell) and read back in the main loop.
set_note() {
  printf '%s\n' "$*" > "$NOTE_FILE"
}

get_note() {
  cat "$NOTE_FILE" 2>/dev/null || true
}

reset_repo_state() {
  : > "$CAPTURE_FILE"
  : > "$NOTE_FILE"
  RESOLVED_COMMAND=""
  COMMIT_SHA=""
}

# Every repo's stdout+stderr goes through here: always into CAPTURE_FILE, and
# also live to the terminal, repo-prefixed, under --verbose.
capture_output() {
  local repo_name="$1"
  local line=""

  if [[ $VERBOSE -eq 0 ]]; then
    cat >> "$CAPTURE_FILE"
    return 0
  fi

  while IFS= read -r line; do
    printf '%s\n' "$line" >> "$CAPTURE_FILE"
    printf '[%s] %s\n' "$repo_name" "$line"
  done
  # A trailing fragment with no newline never reaches the loop body.
  if [[ -n "$line" ]]; then
    printf '%s\n' "$line" >> "$CAPTURE_FILE"
    printf '[%s] %s\n' "$repo_name" "$line"
  fi
}

# Two repos can share a basename under different target dirs; suffix rather than
# let the second clobber the first.
log_file_for() {
  local name="$1"
  local candidate="$LOG_DIR/$name.log"
  local n=2
  while [[ -e "$candidate" ]]; do
    candidate="$LOG_DIR/$name-$n.log"
    n=$((n + 1))
  done
  printf '%s\n' "$candidate"
}

# TSV has no quoting rules, so fields must not contain tabs or newlines.
tsv_field() {
  printf '%s' "$1" | tr '\t\n' '  '
}

# One <repo>.log plus one summary.tsv row. No-op without --log.
record_repo() {
  local repo_path="$1" status="$2" note="$3"
  local pr_url="" tsv_note="$note" dest

  [[ -z "$LOG_DIR" ]] && return 0

  # On success the note is the PR URL; it belongs in pr_url, not in notes too.
  if [[ "$status" == "success" ]]; then
    pr_url="$note"
    tsv_note=""
  fi

  dest=$(log_file_for "$(basename "$repo_path")")
  {
    printf 'repo:     %s\n' "$repo_path"
    printf 'branch:   %s\n' "$BRANCH_NAME"
    printf 'command:  %s\n' "${RESOLVED_COMMAND:-${COMMAND[*]}}"
    printf 'status:   %s\n' "$status"
    printf 'commit:   %s\n' "${COMMIT_SHA:--}"
    printf 'note:     %s\n' "${note:--}"
    printf -- '----------------------------------------\n'
    cat "$CAPTURE_FILE"
  } > "$dest"

  printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(tsv_field "$repo_path")" \
    "$status" \
    "$(tsv_field "$BRANCH_NAME")" \
    "$COMMIT_SHA" \
    "$(tsv_field "$pr_url")" \
    "$(tsv_field "$tsv_note")" >> "$SUMMARY_FILE"
}

result_ok() {
  local name="$1" url="$2"
  printf '✅ %-*s PR created%s\n' "$NAME_WIDTH" "$name" "${url:+  $url}"
}

result_fail() {
  local name="$1" note="$2"
  printf '❌ %-*s %s\n' "$NAME_WIDTH" "$name" "${note:-failed}"
  # Under --verbose the output has already streamed past; don't repeat it.
  if [[ $VERBOSE -eq 0 && -s "$CAPTURE_FILE" ]]; then
    tail -n "$FAIL_TAIL_LINES" "$CAPTURE_FILE" | sed 's/^/    /'
  fi
}

result_skip() {
  local name="$1" note="$2"
  printf '⏭️  %-*s skipped: %s\n' "$NAME_WIDTH" "$name" "${note:-unknown reason}"
}

usage() {
  cat << 'EOF'
Usage: mkprs.sh <target-dir> [<target-dir> ...] -b <branch> [OPTIONS] -- <command> [args...]

Run a command in every repository found under the target directories, then commit
the result and open a pull request for each repo that changed.

Arguments:
  <target-dir>...      One or more directories to search for repositories
  <command> [args...]  Everything after -- is the command to run in each repo

Options:
  -b, --branch <name>          Branch to create in each repo (required)
  -m, --message <msg>          Commit message (default: the command text)
  -t, --title <title>          PR title (default: first line of commit message)
  -B, --body <body>            PR body description (default: empty)
  -r, --reviewer <user>        GitHub user to request review from (optional)
      --log <dir>              Write per-repo logs and summary.tsv to <dir>
  -v, --verbose                Stream command output live, prefixed by repo name
  -h, --help                   Show this help message

Short and long options accept either a space (-b my-branch) or equals (-b=my-branch) separator.

The command:
  * runs with the current directory set to the repository root, so relative
    paths work as they would if you had cd'd into the repo yourself
  * is executed directly, not through a shell -- no globbing, pipes, or
    redirection. Use `-- bash -c '...'` when you need those.
  * has any argument that is exactly {} replaced with the repo's absolute path
  * can read $REPO (absolute path) and $REPO_NAME (basename) from the environment

A repo is skipped when its working tree is dirty, the branch already exists, the
command leaves no changes behind, or its origin remote does not point at
github.com.

Output is one line per repo: ✅ and the PR URL on success, ❌ and the reason on
failure (followed by the tail of the command's output), ⏭️ when the repo was
skipped. Command output is otherwise captured and discarded.

--log <dir> keeps that captured output instead. The directory holds one
<repo>.log per repository -- the resolved command, its full output, the outcome
-- plus a summary.tsv of one tab-separated record each:

  repo_path  status  branch  commit_sha  pr_url  notes

Absent --log nothing is written to disk.

--verbose streams that output live instead of buffering it, each line prefixed
with the repo it came from. It composes with --log: stream and write.

Examples:
  # Bump NuGet dependencies everywhere
  ./mkprs.sh ~/repos -b bump-deps -- dotnet outdated -u

  # Fix a typo, with an explicit commit message
  ./mkprs.sh ~/repos -b fix-typo -m "Fix typo in README" -- sed -i '' 's/teh/the/g' README.md

  # Apply a patch file (replaces the old patch-specific behavior)
  ./mkprs.sh ~/repos -b apply-x -- git apply /tmp/x.patch

  # Anything needing a shell goes through bash -c
  ./mkprs.sh ~/repos -b lint -- bash -c 'npm ci && npm run lint:fix'

  # A tool that insists on an explicit path
  ./mkprs.sh ~/repos -b scan -- some-tool --root {}
EOF
}

parse_args() {
  local positional=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --)                  shift; COMMAND=("$@"); break ;;
      -b|--branch)         BRANCH_NAME="${2-}"; shift 2 ;;
      --branch=*)          BRANCH_NAME="${1#--branch=}"; shift ;;
      -b=*)                BRANCH_NAME="${1#-b=}"; shift ;;
      -m|--message)        COMMIT_MESSAGE="${2-}"; shift 2 ;;
      --message=*)         COMMIT_MESSAGE="${1#--message=}"; shift ;;
      -m=*)                COMMIT_MESSAGE="${1#-m=}"; shift ;;
      -t|--title)          PR_TITLE="${2-}"; shift 2 ;;
      --title=*)           PR_TITLE="${1#--title=}"; shift ;;
      -t=*)                PR_TITLE="${1#-t=}"; shift ;;
      -B|--body)           PR_BODY="${2-}"; shift 2 ;;
      --body=*)            PR_BODY="${1#--body=}"; shift ;;
      -B=*)                PR_BODY="${1#-B=}"; shift ;;
      -r|--reviewer)       PR_REVIEWER="${2-}"; shift 2 ;;
      --reviewer=*)        PR_REVIEWER="${1#--reviewer=}"; shift ;;
      -r=*)                PR_REVIEWER="${1#-r=}"; shift ;;
      --log)               LOG_DIR="${2-}"; shift 2 ;;
      --log=*)             LOG_DIR="${1#--log=}"; shift ;;
      -v|--verbose)        VERBOSE=1; shift ;;
      -h|--help)           usage; exit 0 ;;
      -*)                  echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
      *)                   positional+=("$1"); shift ;;
    esac
  done

  TARGET_DIRS=("${positional[@]+${positional[@]}}")

  if [[ ${#TARGET_DIRS[@]} -lt 1 ]]; then
    echo "Error: Must specify at least one target dir" >&2
    usage >&2
    exit 1
  fi

  if [[ -z "$BRANCH_NAME" ]]; then
    echo "Error: -b/--branch is required" >&2
    usage >&2
    exit 1
  fi

  if [[ ${#COMMAND[@]} -eq 0 ]]; then
    echo "Error: no command specified (everything after -- is the command to run)" >&2
    usage >&2
    exit 1
  fi
}

derive_commit_message() {
  COMMIT_MESSAGE="${COMMAND[*]}"
}

derive_pr_title() {
  PR_TITLE=$(printf '%s\n' "$COMMIT_MESSAGE" | head -n 1)
}

discover_repos() {
  local target_dir="$1"

  if [[ ! -d "$target_dir" ]]; then
    echo "Warning: Target directory does not exist: $target_dir" >&2
    return 0
  fi

  while IFS= read -r -d '' git_dir; do
    ALL_REPOS+=("$(dirname "$git_dir")")
  done < <(find "$target_dir" -type d -name ".git" -prune -print0)
}

is_github_repo() {
  local repo_path="$1"
  local url
  if ! url=$(git -C "$repo_path" config --get remote.origin.url 2>/dev/null); then
    set_note "no 'origin' remote"
    return 1
  fi
  if [[ "$url" != *github.com[:/]* ]]; then
    set_note "non-GitHub remote ($url)"
    return 1
  fi
}

is_clean_tree() {
  local repo_path="$1"
  if [[ -n "$(git -C "$repo_path" status --porcelain)" ]]; then
    set_note "working tree not clean"
    return 1
  fi
}

default_branch() {
  local repo_path="$1" name candidate
  if name=$(git -C "$repo_path" symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null); then
    printf '%s\n' "${name#origin/}"
    return 0
  fi
  for candidate in main master; do
    if git -C "$repo_path" rev-parse --verify --quiet "refs/remotes/origin/$candidate" >/dev/null; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  set_note "could not determine default branch"
  return 1
}

branch_exists() {
  local repo_path="$1" branch="$2"
  git -C "$repo_path" rev-parse --verify --quiet "refs/heads/$branch" >/dev/null \
    || git -C "$repo_path" rev-parse --verify --quiet "refs/remotes/origin/$branch" >/dev/null
}

fetch_origin() {
  local repo_path="$1"
  if ! git -C "$repo_path" fetch origin --quiet --prune; then
    echo "Could not fetch origin for $(basename "$repo_path"); using local refs."
  fi
}

resolve_base() {
  local repo_path="$1" default="$2"
  if git -C "$repo_path" rev-parse --verify --quiet "refs/remotes/origin/$default" >/dev/null; then
    printf 'origin/%s\n' "$default"
  else
    printf '%s\n' "$default"
  fi
}

# Abandon the working branch and return the repo to its default branch.
# Must be run with the current directory inside the repo.
restore_repo() {
  local default="$1"
  git checkout "$default" --quiet
  git branch -D "$BRANCH_NAME" >/dev/null
}

# Returns 0 on success, 1 on failure, 2 when the repo is skipped. The reason is
# left in NOTE_FILE and all command, git and gh output in CAPTURE_FILE.
run_command() {
  local repo_path="$1"
  local repo_name
  repo_name=$(basename "$repo_path")

  is_clean_tree "$repo_path" || return 2
  if branch_exists "$repo_path" "$BRANCH_NAME"; then
    set_note "branch '$BRANCH_NAME' already exists"
    return 2
  fi

  local default
  default=$(default_branch "$repo_path") || return 2
  fetch_origin "$repo_path" 2>&1 | capture_output "$repo_name"

  local base
  base=$(resolve_base "$repo_path" "$default")

  local abs_path
  abs_path=$(realpath "$repo_path")

  # Substitute {} with the repo path, leaving every other argument untouched.
  local -a expanded=()
  local arg
  for arg in "${COMMAND[@]}"; do
    if [[ "$arg" == "{}" ]]; then
      expanded+=("$abs_path")
    else
      expanded+=("$arg")
    fi
  done
  RESOLVED_COMMAND="${expanded[*]}"

  (
    cd "$repo_path" || exit 1

    git checkout -b "$BRANCH_NAME" "$base" --quiet

    export REPO="$abs_path"
    export REPO_NAME="$repo_name"

    local rc=0
    "${expanded[@]}" || rc=$?
    if [[ $rc -ne 0 ]]; then
      set_note "command exited $rc"
      restore_repo "$default"
      exit 1
    fi

    git add -A

    if git diff --cached --quiet; then
      set_note "command made no changes"
      restore_repo "$default"
      exit 2
    fi

    git commit -q -m "$COMMIT_MESSAGE"

    if ! git push -u origin "$BRANCH_NAME" --quiet; then
      set_note "unable to push to origin/$BRANCH_NAME"
      restore_repo "$default"
      exit 1
    fi
  ) 2>&1 | capture_output "$repo_name"
}

create_pr() {
  local repo_path="$1"
  local repo_name
  repo_name=$(basename "$repo_path")

  if ! command -v gh >/dev/null 2>&1; then
    set_note "'gh' (GitHub CLI) is not installed"
    return 1
  fi

  local -a gh_args=(
    pr create
    --base main
    --head "$BRANCH_NAME"
    --title "$PR_TITLE"
    --body "${PR_BODY:-}"
  )
  if [[ -n "$PR_REVIEWER" ]]; then
    gh_args+=(--reviewer "$PR_REVIEWER")
  fi

  # gh prints the PR URL on stdout; that URL is the success line.
  local out
  if out=$( cd "$repo_path" && gh "${gh_args[@]}" 2>> "$CAPTURE_FILE" ); then
    printf '%s\n' "$out" >> "$CAPTURE_FILE"
    set_note "$(printf '%s' "$out" | tail -n 1)"
  else
    printf '%s\n' "$out" >> "$CAPTURE_FILE"
    set_note "failed to create PR"
    return 1
  fi
}

cleanup_branch() {
  local repo_path="$1" default
  default=$(default_branch "$repo_path") || default="main"
  (
    cd "$repo_path" || return
    restore_repo "$default"
  ) 2>&1 | capture_output "$(basename "$repo_path")"
}

# One repo, start to finish. Same exit-code convention as run_command.
process_repo() {
  local repo_path="$1" rc=0

  is_github_repo "$repo_path" || return 2
  run_command "$repo_path" || return $?
  # Read the SHA while the branch still exists; cleanup_branch deletes it below.
  COMMIT_SHA=$(git -C "$repo_path" rev-parse --short "$BRANCH_NAME" 2>/dev/null || true)
  create_pr "$repo_path" || rc=1
  cleanup_branch "$repo_path"
  return $rc
}

print_summary() {
  echo ""
  echo "=== Summary ==="
  echo "Succeeded: $PROCESSED"
  echo "Failed:    $FAILED"
  echo "Skipped:   $SKIPPED"
}

# =============================================================================
# Main execution
# =============================================================================

parse_args "$@"
init_output

if [[ -z "$COMMIT_MESSAGE" ]]; then
  derive_commit_message
fi

if [[ -z "$PR_TITLE" ]]; then
  derive_pr_title
fi

for target_dir in "${TARGET_DIRS[@]}"; do
  discover_repos "$target_dir"
done

if [[ ${#ALL_REPOS[@]} -eq 0 ]]; then
  echo "No target repositories found." >&2
  exit 0
fi

# Align the result column to the longest repo name.
for repo_path in "${ALL_REPOS[@]}"; do
  repo_name=$(basename "$repo_path")
  if [[ ${#repo_name} -gt $NAME_WIDTH ]]; then
    NAME_WIDTH=${#repo_name}
  fi
done

for repo_path in "${ALL_REPOS[@]}"; do
  repo_name=$(basename "$repo_path")
  reset_repo_state

  status=0
  process_repo "$repo_path" || status=$?
  note=$(get_note)

  case $status in
    0)
      result_ok "$repo_name" "$note"
      record_repo "$repo_path" "success" "$note"
      PROCESSED=$((PROCESSED + 1))
      ;;
    2)
      result_skip "$repo_name" "$note"
      record_repo "$repo_path" "skipped" "$note"
      SKIPPED=$((SKIPPED + 1))
      ;;
    *)
      result_fail "$repo_name" "$note"
      record_repo "$repo_path" "failed" "$note"
      FAILED=$((FAILED + 1))
      ;;
  esac
done

print_summary
