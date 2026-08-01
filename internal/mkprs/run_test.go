package mkprs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestRunOpensPullRequest(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("good")
	prs := &fakePR{}

	got := run(t, prs, []string{f.targets, "-b", "greet", "-m", "Say hello"}, helperCmd(t, "write", "file.txt", "hello world")...)

	if got.code != exitOK {
		t.Errorf("exit code = %d, want %d", got.code, exitOK)
	}
	if want := "✅ good PR created  https://github.com/fake/good/pull/7\n"; !strings.Contains(got.stdout, want) {
		t.Errorf("stdout = %q, want it to contain %q", got.stdout, want)
	}

	call := prs.only(t)
	if call.repoPath != repo {
		t.Errorf("opened PR for %q, want %q", call.repoPath, repo)
	}
	if want := (pullRequest{Base: "main", Head: "greet", Title: "Say hello"}); call.pr != want {
		t.Errorf("pullRequest = %+v, want %+v", call.pr, want)
	}

	// The change reached the fake GitHub side, on the right branch.
	if !f.remoteHasBranch("good", "greet") {
		t.Fatal("branch was not pushed to origin")
	}
	if got, want := f.remoteFile("good", "greet", "file.txt"), "hello world"; got != want {
		t.Errorf("pushed file = %q, want %q", got, want)
	}
	if got, want := f.remoteSubject("good", "greet"), "Say hello"; got != want {
		t.Errorf("commit subject = %q, want %q", got, want)
	}

	// The repo is left as it was found.
	if got := currentBranch(t, repo); got != "main" {
		t.Errorf("left on branch %q, want main", got)
	}
	if localHasBranch(t, repo, "greet") {
		t.Error("the working branch should have been deleted")
	}
}

func TestRunPullRequestFields(t *testing.T) {
	t.Parallel()

	t.Run("title defaults to the commit message", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "First line\nsecond line"}, helperCmd(t, "write", "file.txt", "x")...)

		if got, want := prs.only(t).pr.Title, "First line"; got != want {
			t.Errorf("title = %q, want %q", got, want)
		}
	})

	t.Run("message defaults to the command text", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		command := helperCmd(t, "write", "file.txt", "x")
		run(t, prs, []string{f.targets, "-b", "b"}, command...)

		want := strings.Join(command, " ")
		if got := prs.only(t).pr.Title; got != want {
			t.Errorf("title = %q, want the command text %q", got, want)
		}
		if got := f.remoteSubject("x", "b"); got != want {
			t.Errorf("commit subject = %q, want the command text %q", got, want)
		}
	})

	t.Run("explicit title, body and reviewers are passed through", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "msg", "-t", "Title", "-B", "Body", "-r", "alice,bob"}, helperCmd(t, "write", "file.txt", "x")...)

		want := pullRequest{Base: "main", Head: "b", Title: "Title", Body: "Body", Reviewers: "alice,bob"}
		if got := prs.only(t).pr; got != want {
			t.Errorf("pullRequest = %+v, want %+v", got, want)
		}
	})

	t.Run("-d asks for a draft PR", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "msg", "-d"}, helperCmd(t, "write", "file.txt", "x")...)

		if !prs.only(t).pr.Draft {
			t.Error("Draft = false, want true")
		}
	})

	// The base follows the repo rather than a hardcoded "main": a PR against a
	// branch the repo does not have would be rejected by GitHub outright.
	t.Run("base is the repo's own default branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repoOn("x", "master", "git@github.com:fake/x.git")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "msg"}, helperCmd(t, "write", "file.txt", "x")...)

		if got, want := prs.only(t).pr.Base, "master"; got != want {
			t.Errorf("base = %q, want %q", got, want)
		}

		// And the branch was cut from master, so the PR's diff is just this
		// commit -- the base and the fork point have to agree.
		if got := currentBranch(t, repo); got != "master" {
			t.Errorf("left on branch %q, want master", got)
		}
		if got, want := f.remoteFile("x", "b", "file.txt"), "x"; got != want {
			t.Errorf("pushed file = %q, want %q", got, want)
		}
	})
}

func TestRunStagesEveryChange(t *testing.T) {
	t.Parallel()

	t.Run("new files are committed", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "brand-new.txt", "content")...)

		if got, want := f.remoteFile("x", "b", "brand-new.txt"), "content"; got != want {
			t.Errorf("new file = %q, want %q", got, want)
		}
	})

	t.Run("deletions are committed", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "rm", "file.txt")...)

		files := gitCmd(t, f.bare("x"), "ls-tree", "--name-only", "b")
		if strings.Contains(files, "file.txt") {
			t.Errorf("file.txt still present on the branch: %q", files)
		}
	})
}

// Regression: a command is allowed to commit its own work. Deciding "did
// anything happen?" from the index read that as a no-op skip, and the deferred
// restoreRepo then deleted the branch the commit was on.
func TestRunCommandThatCommitsItsOwnWork(t *testing.T) {
	t.Parallel()

	t.Run("the commit survives and a PR is opened", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "gitcommit", "file.txt", "by the command")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d", got.code, exitOK)
		}
		if !f.remoteHasBranch("x", "b") {
			t.Fatalf("the command's commit was discarded:\n%s", got.all())
		}
		if got, want := f.remoteFile("x", "b", "file.txt"), "by the command"; got != want {
			t.Errorf("pushed file = %q, want %q", got, want)
		}
		if len(prs.calls) != 1 {
			t.Errorf("opened %d PRs, want 1", len(prs.calls))
		}
		if got := currentBranch(t, repo); got != "main" {
			t.Errorf("left on branch %q, want main", got)
		}
	})

	// Guards against a fix that reaches for --allow-empty: with nothing staged
	// there is nothing for mkprs to commit, so the command's own commit is the
	// only one on the branch.
	t.Run("mkprs does not add a commit of its own", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "mkprs message"}, helperCmd(t, "gitcommit", "file.txt", "by the command")...)

		if got, want := f.remoteSubject("x", "b"), "committed by the command"; got != want {
			t.Errorf("commit subject = %q, want %q", got, want)
		}
		if got, want := commitCount(t, f.bare("x"), "b"), 2; got != want {
			t.Errorf("branch has %d commits, want %d", got, want)
		}
	})

	// Staging still runs: whatever the command left uncommitted is mkprs's to
	// commit, on top of the command's own.
	t.Run("a command that commits and dirties the tree lands both", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "mkprs message"},
			helperCmd(t, "gitcommit", "committed.txt", "by the command", "leftover.txt")...)

		if got, want := f.remoteFile("x", "b", "committed.txt"), "by the command"; got != want {
			t.Errorf("command's file = %q, want %q", got, want)
		}
		if got, want := f.remoteFile("x", "b", "leftover.txt"), "left behind"; got != want {
			t.Errorf("leftover file = %q, want %q", got, want)
		}
		if got, want := f.remoteSubject("x", "b"), "mkprs message"; got != want {
			t.Errorf("commit subject = %q, want %q", got, want)
		}
	})
}

// The branch mkprs works on is cut from origin's default branch, so whatever was
// checked out when the run started never contributes to it -- it is only where
// the repo gets put back. A detached HEAD is the exception: no name to record.
func TestRunStartsFromAnyBranch(t *testing.T) {
	t.Parallel()

	t.Run("on a feature branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		// Let the feature branch carry a commit of its own, so "cut from
		// origin's default" and "cut from wherever we were" are distinguishable.
		gitCmd(t, repo, "checkout", "-q", "-b", "feature")
		writeFile(t, filepath.Join(repo, "feature-only.txt"), "wip")
		gitCmd(t, repo, "add", "-A")
		gitCmd(t, repo, "commit", "-q", "-m", "feature work")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d", got.code, exitOK)
		}
		if len(prs.calls) != 1 {
			t.Fatalf("opened %d PRs, want 1", len(prs.calls))
		}
		// Cut from origin/main, so the feature branch's own work is not
		// along for the ride.
		if tree := gitCmd(t, f.bare("x"), "ls-tree", "-r", "--name-only", "b"); strings.Contains(tree, "feature-only.txt") {
			t.Errorf("pushed tree = %q, want it cut from the default branch", tree)
		}
		// The PR still targets the default branch, not the branch the repo
		// happened to be sitting on.
		if got, want := prs.calls[0].pr.Base, "main"; got != want {
			t.Errorf("PR base = %q, want %q", got, want)
		}
		// And the repo is handed back exactly where it was found.
		if got := currentBranch(t, repo); got != "feature" {
			t.Errorf("left on branch %q, want feature", got)
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "checkout", "-q", "--detach")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if want := "not on a branch"; !strings.Contains(got.stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", got.stdout, want)
		}
		if len(prs.calls) != 0 {
			t.Errorf("opened %d PRs, want none", len(prs.calls))
		}
	})
}

// Everything after the command assumes mkprs's own branch is checked out:
// staging and committing act on whatever HEAD points at. A command that switches
// branches fails the repo rather than committing somewhere mkprs does not own.
// Whatever it created is left in place for the user to find.
func TestRunRefusesABranchSwitch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *fixture, repo string)
		command func(t *testing.T) []string
		want    string
		// wantHead is where the repo is left: wherever the command put it. This
		// is a failure, and a failure is not cleaned up.
		wantHead string
	}{
		{
			name:     "a branch the command creates and commits to",
			command:  func(t *testing.T) []string { return helperCmd(t, "gitcheckout", "other", "file.txt", "by the command") },
			want:     "command left the repo on 'other', not 'b'",
			wantHead: "other",
		},
		{
			name: "a pre-existing branch",
			setup: func(t *testing.T, f *fixture, repo string) {
				gitCmd(t, repo, "branch", "other")
			},
			command:  func(t *testing.T) []string { return helperCmd(t, "gitcheckout", "other") },
			want:     "command left the repo on 'other', not 'b'",
			wantHead: "other",
		},
		{
			// Without this the run would commit straight to the default branch
			// and push it, bypassing review entirely.
			name:     "the default branch",
			command:  func(t *testing.T) []string { return helperCmd(t, "gitcheckout", "main", "file.txt", "sneaky") },
			want:     "command left the repo on 'main', not 'b'",
			wantHead: "main",
		},
		{
			name:     "a detached HEAD",
			command:  func(t *testing.T) []string { return helperCmd(t, "gitdetach") },
			want:     "command left the repo with a detached HEAD",
			wantHead: "HEAD", // what --abbrev-ref reports when detached
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := f.repo("x")
			if tt.setup != nil {
				tt.setup(t, f, repo)
			}
			mainBefore := f.remoteSubject("x", "main")
			prs := &fakePR{}

			got := run(t, prs, []string{f.targets, "-b", "b"}, tt.command(t)...)

			// One repo failing is not fatal to the run; the ❌ line carries it.
			if got.code != exitOK {
				t.Errorf("exit code = %d, want %d", got.code, exitOK)
			}
			if !strings.Contains(got.stdout, tt.want) {
				t.Errorf("stdout = %q, want it to contain %q", got.stdout, tt.want)
			}
			if !strings.Contains(got.stdout, "Failed:    1") {
				t.Errorf("summary did not count the failure:\n%s", got.stdout)
			}
			if len(prs.calls) != 0 {
				t.Errorf("opened %d PRs, want none", len(prs.calls))
			}
			// Nothing reached origin: not the stray branch, not the default one.
			if f.remoteHasBranch("x", "other") {
				t.Error("the command's branch was pushed, it should not have been")
			}
			if got := f.remoteSubject("x", "main"); got != mainBefore {
				t.Errorf("origin/main moved to %q, want it untouched at %q", got, mainBefore)
			}
			// Nothing is cleaned up, mkprs's own branch included: the failure is
			// left standing where it happened for the user to sort out.
			if !localHasBranch(t, repo, "b") {
				t.Error("mkprs's own branch should have been left in place")
			}
			if got := currentBranch(t, repo); got != tt.wantHead {
				t.Errorf("left on %q, want %q", got, tt.wantHead)
			}
		})
	}
}

// A branch the command created is the user's, and survives the failure with its
// commit intact. Cleanup would only ever have deleted the branch mkprs made,
// and on a failure it does not run at all.
func TestRunLeavesTheCommandsBranchAlone(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	prs := &fakePR{}

	run(t, prs, []string{f.targets, "-b", "b"},
		helperCmd(t, "gitcheckout", "other", "file.txt", "by the command")...)

	if !localHasBranch(t, repo, "other") {
		t.Fatal("the command's branch was deleted, it should survive")
	}
	if got, want := gitCmd(t, repo, "log", "-1", "--pretty=%s", "other"), "committed by the command"; got != want {
		t.Errorf("'other' subject = %q, want %q", got, want)
	}
	if got, want := gitCmd(t, repo, "show", "other:file.txt"), "by the command"; got != want {
		t.Errorf("'other' file = %q, want %q", got, want)
	}
}

// Skips are normal outcomes, not errors: the run still exits 0.
//
// Which conditions skip is settled by TestPreflight and TestCommitAndPush. What
// needs a whole run is what happens afterwards, and that does not vary by
// reason -- so this keeps one row from each of the two places a skip can come
// from, rather than one per condition.
func TestRunSkips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *fixture) string // returns the repo path
		command func(t *testing.T) []string
		want    string
	}{
		{
			name: "before the command runs",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				writeFile(t, filepath.Join(repo, "file.txt"), "uncommitted\n")
				return repo
			},
			want: "skipped: working tree not clean",
		},
		{
			// The other source, and the one that reaches cleanup with a branch
			// already cut: the run gets as far as committing before deciding
			// there is nothing to open a PR for.
			name: "after the command runs",
			setup: func(t *testing.T, f *fixture) string {
				return f.repo("x")
			},
			command: func(t *testing.T) []string { return helperCmd(t, "noop") },
			want:    "skipped: command made no changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := tt.setup(t, f)
			prs := &fakePR{}

			command := helperCmd(t, "write", "file.txt", "changed")
			if tt.command != nil {
				command = tt.command(t)
			}

			got := run(t, prs, []string{f.targets, "-b", "b"}, command...)

			if got.code != exitOK {
				t.Errorf("exit code = %d, want %d", got.code, exitOK)
			}
			if !strings.Contains(got.stdout, tt.want) {
				t.Errorf("stdout = %q, want it to contain %q", got.stdout, tt.want)
			}
			if len(prs.calls) != 0 {
				t.Errorf("opened %d PRs, want none", len(prs.calls))
			}
			if !strings.Contains(got.stdout, "Skipped:   1") {
				t.Errorf("summary did not count the skip:\n%s", got.stdout)
			}
			// A skipped repo is left exactly as it was found.
			if got := currentBranch(t, repo); got != "main" {
				t.Errorf("left on branch %q, want main", got)
			}
		})
	}
}

// Regression: a merged-and-deleted PR branch leaves refs/remotes/origin/<branch>
// behind. The branch check has to run after the pruning fetch, or every repo
// skips on the next run with the same branch name.
func TestRunPrunesStaleRemoteBranch(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("stale")

	// Exactly the state GitHub leaves after "delete branch on merge".
	gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/gone")
	gitCmd(t, repo, "fetch", "-q", "origin")
	gitCmd(t, f.bare("stale"), "update-ref", "-d", "refs/heads/gone")
	if got := branchLocation(repo, "gone"); got != "on origin" {
		t.Fatalf("fixture: branchLocation = %q, want a stale ref to be present", got)
	}

	prs := &fakePR{}
	got := run(t, prs, []string{f.targets, "-b", "gone"}, helperCmd(t, "write", "file.txt", "changed")...)

	if strings.Contains(got.stdout, "already exists") {
		t.Errorf("skipped on a stale ref:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "✅ stale PR created") {
		t.Errorf("stdout = %q, want a successful PR", got.stdout)
	}
	if len(prs.calls) != 1 {
		t.Errorf("opened %d PRs, want 1", len(prs.calls))
	}
}

func TestRunFailures(t *testing.T) {
	t.Parallel()

	t.Run("a failing command reports its exit code and output", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "fail", "3", "it went wrong")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d even when a repo fails", got.code, exitOK)
		}
		if !strings.Contains(got.stdout, "❌ x command exited 3") {
			t.Errorf("stdout = %q, want the exit code", got.stdout)
		}
		if !strings.Contains(got.stdout, "    it went wrong") {
			t.Errorf("stdout = %q, want the indented output tail", got.stdout)
		}
		if !strings.Contains(got.stdout, "Failed:    1") {
			t.Errorf("summary did not count the failure:\n%s", got.stdout)
		}
		if len(prs.calls) != 0 {
			t.Error("a failing command should not open a PR")
		}
		// A failure is left exactly as it broke: still on the branch, branch
		// still there. Cleanup would have to check out the default branch, and
		// that drags whatever the command wrote across with it.
		if !localHasBranch(t, repo, "b") {
			t.Error("the working branch should have been left in place")
		}
		if got := currentBranch(t, repo); got != "b" {
			t.Errorf("left on branch %q, want b", got)
		}
	})

	// Push failure: origin's fetch URL still works, only the push URL is bad.
	// chmod cannot express this portably -- it has no effect on directories on
	// Windows.
	t.Run("a failing push is reported and cleaned up", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("nopush")
		gitCmd(t, repo, "config", "remote.origin.pushurl", fileURL(filepath.Join(f.root, "nowhere.git")))
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stdout, "❌ nopush unable to push to origin/b") {
			t.Errorf("stdout = %q, want a push failure", got.stdout)
		}
		if len(prs.calls) != 0 {
			t.Error("a failed push should not open a PR")
		}
		// Nothing else has the commit -- the push is what failed -- so deleting
		// the branch here would be the one case that actually loses work.
		if !localHasBranch(t, repo, "b") {
			t.Fatal("the working branch should have been left in place")
		}
		if got, want := gitCmd(t, repo, "show", "b:file.txt"), "changed"; got != want {
			t.Errorf("file on the surviving branch = %q, want %q", got, want)
		}
		if got := currentBranch(t, repo); got != "b" {
			t.Errorf("left on branch %q, want b", got)
		}
	})

	t.Run("a failing PR is reported and cleaned up", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{err: errors.New("failed to create PR")}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stdout, "❌ x failed to create PR") {
			t.Errorf("stdout = %q, want a PR failure", got.stdout)
		}
		if !strings.Contains(got.stdout, "Failed:    1") {
			t.Errorf("summary did not count the failure:\n%s", got.stdout)
		}
		// The branch stays on origin: the commit is pushed before the PR is
		// opened, so the work is not lost.
		if !f.remoteHasBranch("x", "b") {
			t.Error("the pushed branch should survive a PR failure")
		}
		// And locally too. The push succeeded, so this one is recoverable either
		// way, but a failure gets the same treatment wherever it happens rather
		// than a rule per step.
		if !localHasBranch(t, repo, "b") {
			t.Error("the local branch should have been left in place")
		}
	})
}

// -k skips cleanup outright rather than skipping only the delete: the repo is
// left on the branch, ready to carry on in.
func TestRunKeepBranch(t *testing.T) {
	t.Parallel()

	t.Run("a successful repo is left on its branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b", "-k", "-m", "msg"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d", got.code, exitOK)
		}
		if len(prs.calls) != 1 {
			t.Errorf("opened %d PRs, want 1 -- -k is about cleanup, nothing else", len(prs.calls))
		}
		if !localHasBranch(t, repo, "b") {
			t.Fatal("the working branch should have been kept")
		}
		if got := currentBranch(t, repo); got != "b" {
			t.Errorf("left on branch %q, want b", got)
		}
	})

	// Uniform, so there is one rule to remember rather than one per outcome --
	// even though the branch a skip leaves behind carries no commits.
	t.Run("a skipped repo keeps its branch too", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b", "-k"}, helperCmd(t, "noop")...)

		if !strings.Contains(got.stdout, "skipped: command made no changes") {
			t.Errorf("stdout = %q, want the no-changes skip", got.stdout)
		}
		if !localHasBranch(t, repo, "b") {
			t.Error("the working branch should have been kept")
		}
	})

	// Without -k the default holds: restored and deleted.
	t.Run("without -k a successful repo is restored", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "msg"}, helperCmd(t, "write", "file.txt", "changed")...)

		if localHasBranch(t, repo, "b") {
			t.Error("the working branch should have been deleted")
		}
		if got := currentBranch(t, repo); got != "main" {
			t.Errorf("left on branch %q, want main", got)
		}
	})
}

func TestRunMultipleReposAndDirs(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("alpha")
	f.repo("beta")

	other := filepath.Join(f.root, "other")
	mkdir(t, other)
	gitCmd(t, "", "init", "-q", "-b", "main", filepath.Join(other, "gamma"))

	prs := &fakePR{}
	got := run(t, prs, []string{f.targets, other, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	// Names are padded to align the status column, so match the name only.
	for _, want := range []string{"✅ alpha", "✅ beta", "⏭️  gamma", "Succeeded: 2"} {
		if !strings.Contains(got.all(), want) {
			t.Errorf("output missing %q:\n%s", want, got.all())
		}
	}
	if len(prs.calls) != 2 {
		t.Errorf("opened %d PRs, want 2", len(prs.calls))
	}
}

func TestRunWarnsAboutMissingTargetDir(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("x")
	missing := filepath.Join(f.root, "nope")

	got := run(t, &fakePR{}, []string{f.targets, missing, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	if !strings.Contains(got.stderr, "Target directory does not exist: "+missing) {
		t.Errorf("stderr = %q, want a warning", got.stderr)
	}
	if !strings.Contains(got.stdout, "✅ x PR created") {
		t.Errorf("the surviving repo should still be processed:\n%s", got.stdout)
	}
}

// By default, a repo's own output is captured, not printed. --verbose streams it
// live, prefixed with the repo it came from.
func TestRunOutputVerbosity(t *testing.T) {
	t.Parallel()

	t.Run("quiet by default", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "print", "chatty output")...)

		if strings.Contains(got.all(), "chatty output") {
			t.Errorf("command output leaked into a quiet run:\n%s", got.all())
		}
	})

	t.Run("verbose streams prefixed lines", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b", "-v"}, helperCmd(t, "print", "chatty output")...)

		if !strings.Contains(got.stdout, "[x] chatty output") {
			t.Errorf("stdout = %q, want prefixed streaming", got.stdout)
		}
	})

	// The URL belongs on the result line; the PR opener never writes it into
	// the capture, so there is nothing to stream.
	t.Run("verbose does not stream the PR url", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b", "-v"}, helperCmd(t, "write", "file.txt", "changed")...)

		if strings.Contains(got.stdout, "[x] https://") {
			t.Errorf("PR url was streamed:\n%s", got.stdout)
		}
		if !strings.Contains(got.stdout, "✅ x PR created  https://") {
			t.Errorf("stdout = %q, want the URL on the result line", got.stdout)
		}
	})

	// Under --verbose the failure output has already streamed past, so the
	// result line must not repeat it.
	t.Run("verbose does not duplicate failure output", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b", "-v"}, helperCmd(t, "fail", "1", "boom")...)

		if n := strings.Count(got.stdout, "boom"); n != 1 {
			t.Errorf("output mentions boom %d times, want 1:\n%s", n, got.stdout)
		}
	})
}

// A failure is the one place the whole capture is replayed, since there is no
// --log to read it from. Nothing is truncated and nothing is held back -- not
// even the PR opener's output, which used to be visible only in the log file.
func TestRunFailuresExplainThemselves(t *testing.T) {
	t.Parallel()

	t.Run("the command's full output is printed", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "fail", "3", "it went wrong")...)

		if !strings.Contains(got.stdout, "❌ x command exited 3") {
			t.Errorf("stdout missing the reason:\n%s", got.stdout)
		}
		if !strings.Contains(got.stdout, "    it went wrong") {
			t.Errorf("stdout missing the indented output:\n%s", got.stdout)
		}
	})

	// A long failure is replayed in full: truncating it would put the useful
	// part out of reach now that there is nowhere else to look.
	t.Run("long output is not truncated", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "spew", "100", "3")...)

		for _, want := range []string{"    line-1\n", "    line-100\n"} {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("stdout missing %q:\n%s", want, got.stdout)
			}
		}
	})

	// The gap this change closed: a PR failure used to say only "failed to
	// create PR", with the opener's explanation reachable only via --log.
	t.Run("the PR opener's output is shown", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{err: errors.New("failed to create PR")}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stdout, "❌ x failed to create PR") {
			t.Errorf("stdout missing the reason:\n%s", got.stdout)
		}
		if !strings.Contains(got.stdout, "    pull request failed") {
			t.Errorf("stdout missing the opener's output:\n%s", got.stdout)
		}
	})

	// ...and under --verbose it streams live instead, rather than vanishing.
	t.Run("the PR opener's output streams under verbose", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{err: errors.New("failed to create PR")}

		got := run(t, prs, []string{f.targets, "-b", "b", "-v"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stdout, "[x] pull request failed") {
			t.Errorf("stdout missing the streamed opener output:\n%s", got.stdout)
		}
	})
}

func TestRunSummaryCountsEveryState(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("succeeds")
	dirty := f.repo("skips")
	writeFile(t, filepath.Join(dirty, "file.txt"), "uncommitted\n")
	f.repoWithRemote("alsoskips", "git@gitlab.com:fake/alsoskips.git")

	// Fails only in the repo where the command can run: it writes to a path
	// that does not exist.
	got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	for _, want := range []string{"Succeeded: 1", "Failed:    0", "Skipped:   2"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("summary missing %q:\n%s", want, got.stdout)
		}
	}
}

// =============================================================================
// The pieces of processRepo, tested directly
//
// Everything above drives a whole run, which is the only way to test a step
// that lives inside a 127-line function. These have one exit each, so they can
// be called on their own -- and the cases that are awkward to stage as a repo
// become rows in a table.
//
// The tests above are what is left once that is possible: the wiring, the
// reporting, and the interactions between steps. A condition that only decides
// one step's answer belongs down here, where it costs a fixture instead of a
// full run.
// =============================================================================

// {} substitution was reachable only through TestRunCommandContext, which
// builds a repo, runs a command and reads the pushed commit back to prove a
// loop over strings works. These rows cost nothing, and two of them -- {} twice
// over, and {} inside a larger argument -- are cases no single command line can
// stage.
func TestExpandCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command []string
		want    []string
	}{
		{
			name:    "no placeholder",
			command: []string{"echo", "hello"},
			want:    []string{"echo", "hello"},
		},
		{
			name:    "the placeholder alone",
			command: []string{"cp", "/example/file", "{}"},
			want:    []string{"cp", "/example/file", "/repo"},
		},
		{
			name:    "every occurrence, not just the first",
			command: []string{"diff", "{}", "{}"},
			want:    []string{"diff", "/repo", "/repo"},
		},
		{
			// Only an argument that is exactly {} substitutes, which is what
			// usageTail promises. A command meaning those two characters
			// literally has no escape, so the rule has to stay this narrow.
			name:    "an argument that merely contains it",
			command: []string{"echo", "{}/sub", "a{}b", "{ }", "{}}"},
			want:    []string{"echo", "{}/sub", "a{}b", "{ }", "{}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := expandCommand(tt.command, "/repo")
			if !slices.Equal(got, tt.want) {
				t.Errorf("expandCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
			// The caller's slice is reused for the next repo, so writing
			// through it would leave the second repo's command holding the
			// first repo's path.
			if tt.command[len(tt.command)-1] == "{}" && got[len(got)-1] == tt.command[len(tt.command)-1] {
				t.Error("expandCommand substituted in place")
			}
		})
	}
}

// preflight is every filter that can end a repo before its command runs, plus
// the three branch names the rest of processRepo needs.
func TestPreflight(t *testing.T) {
	t.Parallel()

	// What is only visible here is the data return: a repo that passes hands
	// back three names that are all distinct from one another.
	t.Run("a repo that passes", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		// Somewhere other than the default branch, so startBranch cannot pass
		// by accidentally agreeing with defaultBranch.
		gitCmd(t, repo, "checkout", "-q", "-b", "feature")

		p, res := preflight(repo, "b", newCapture("x", false, io.Discard))
		if res != nil {
			t.Fatalf("preflight = %#v, want it to carry on", res)
		}
		if got, want := p.defaultBranch, "main"; got != want {
			t.Errorf("defaultBranch = %q, want %q", got, want)
		}
		if got, want := p.startBranch, "feature"; got != want {
			t.Errorf("startBranch = %q, want %q", got, want)
		}
		// The remote-tracking ref, so the branch is cut from what origin has.
		if got, want := p.base, "origin/main"; got != want {
			t.Errorf("base = %q, want %q", got, want)
		}
	})

	// Every way a repo can stop here. These used to be rows in TestRunSkips,
	// each paying for a whole run -- discovery, a command, a fake PR opener --
	// to reach a decision made before any of that happens. TestRunSkips keeps
	// one, since how a skip is reported does not vary by reason.
	//
	// Being one table is also what makes the set legible: "could not determine
	// default branch" had no test at all, and its absence was invisible while
	// these were scattered.
	stops := []struct {
		name  string
		setup func(t *testing.T, f *fixture) string // returns the repo path
		want  string
	}{
		{
			name: "non-GitHub remote",
			setup: func(t *testing.T, f *fixture) string {
				return f.repoWithRemote("x", "git@gitlab.com:fake/x.git")
			},
			want: "non-GitHub remote (git@gitlab.com:fake/x.git)",
		},
		{
			name: "no origin at all",
			setup: func(t *testing.T, f *fixture) string {
				return f.plainRepo("x")
			},
			want: "no 'origin' remote",
		},
		{
			name: "dirty working tree",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				writeFile(t, filepath.Join(repo, "file.txt"), "uncommitted\n")
				return repo
			},
			want: "working tree not clean",
		},
		{
			// Only reachable when the fetch fails: git recreates origin/HEAD
			// on a successful one, so preflight would otherwise heal this
			// between deleting the ref and reading it. Origin still says
			// github.com -- the repo gets this far -- but there is nothing
			// behind it any more and no cached refs to fall back on.
			name: "no discoverable default branch",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				if err := os.RemoveAll(f.bare("x")); err != nil {
					t.Fatalf("remove the remote: %v", err)
				}
				// --no-deref, or this deletes origin/main instead: origin/HEAD
				// is a symref pointing at it.
				gitCmd(t, repo, "update-ref", "--no-deref", "-d", "refs/remotes/origin/HEAD")
				gitCmd(t, repo, "update-ref", "-d", "refs/remotes/origin/main")
				return repo
			},
			want: "could not determine default branch",
		},
		{
			name: "detached HEAD",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "checkout", "-q", "--detach")
				return repo
			},
			want: "not on a branch (detached HEAD)",
		},
		{
			name: "branch already exists locally",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "branch", "b")
				return repo
			},
			want: "branch 'b' already exists locally",
		},
		{
			name: "branch already exists on origin",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/b")
				gitCmd(t, repo, "fetch", "-q", "origin")
				return repo
			},
			want: "branch 'b' already exists on origin",
		},
	}

	for _, tt := range stops {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := tt.setup(t, f)

			p, res := preflight(repo, "b", newCapture("x", false, io.Discard))
			if res == nil {
				t.Fatalf("preflight carried on, want %q", tt.want)
			}
			skipped, ok := res.(outcomeSkipped)
			if !ok {
				t.Fatalf("outcome = %T, want a skip -- none of these is a failure", res)
			}
			if got := skipped.reason; got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
			// Nothing downstream may read the data return once the outcome is
			// set, so it must not be half-filled.
			if (p != prep{}) {
				t.Errorf("prep = %#v, want the zero value alongside an outcome", p)
			}
		})
	}
}

// cleanup used to be a deferred closure that could only be reached by making a
// repo end each way for real. The rule it encodes is small enough to state as a
// table: restore unless the repo failed or -k was passed.
func TestCleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		keepBranch  bool
		res         outcome
		wantRestore bool
	}{
		{
			name:        "success",
			res:         success("https://github.com/fake/x/pull/7"),
			wantRestore: true,
		},
		{
			// Nothing worth keeping, so the branch goes with it.
			name:        "a skip",
			res:         skip("command made no changes"),
			wantRestore: true,
		},
		{
			// The branch, its commits and any uncommitted edits are the only
			// record of what broke.
			name:        "a failure",
			res:         fail("command exited 1", nil),
			wantRestore: false,
		},
		{
			name:        "-k",
			keepBranch:  true,
			res:         success("https://github.com/fake/x/pull/7"),
			wantRestore: false,
		},
		{
			// -k and a failure agree, and neither needs to know about the
			// other: both mean leave the repo alone.
			name:        "-k and a failure",
			keepBranch:  true,
			res:         fail("command exited 1", nil),
			wantRestore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := f.repo("x")
			gitCmd(t, repo, "checkout", "-q", "-b", "feature")
			gitCmd(t, repo, "checkout", "-q", "-b", "b")

			a := &app{cfg: &config{branch: "b", keepBranch: tt.keepBranch}}
			a.cleanup(tt.res, repo, "feature", newCapture("x", false, io.Discard))

			wantBranch := "b"
			if tt.wantRestore {
				wantBranch = "feature"
			}
			if got := currentBranch(t, repo); got != wantBranch {
				t.Errorf("left on branch %q, want %q", got, wantBranch)
			}
			if got := localHasBranch(t, repo, "b"); got == tt.wantRestore {
				t.Errorf("branch 'b' exists = %v, want %v", got, !tt.wantRestore)
			}
		})
	}
}

// The command sees the repo as its working directory, its path via {} and
// $REPO, and its name via $REPO_NAME.
//
// Each row used to be a whole run -- discover, cut a branch, commit, push, open
// a PR -- with the value read back out of a commit on the fake remote, all to
// find out what one environment variable held. runCommand is that step on its
// own, so the file can be read where the command wrote it.
func TestRunCommandContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command func(t *testing.T) []string
		want    func(repo string) string
	}{
		{
			name:    "{} becomes the repo path",
			command: func(t *testing.T) []string { return helperCmd(t, "args", "out.txt", "{}") },
			want:    func(repo string) string { return resolvePath(repo) + "\n" },
		},
		{
			name:    "REPO is the absolute path",
			command: func(t *testing.T) []string { return helperCmd(t, "env", "out.txt", "REPO") },
			want:    func(repo string) string { return resolvePath(repo) + "\n" },
		},
		{
			name:    "REPO_NAME is the basename",
			command: func(t *testing.T) []string { return helperCmd(t, "env", "out.txt", "REPO_NAME") },
			want:    func(repo string) string { return filepath.Base(repo) + "\n" },
		},
		{
			name:    "the working directory is the repo root",
			command: func(t *testing.T) []string { return helperCmd(t, "pwd", "out.txt") },
			want:    func(repo string) string { return resolvePath(repo) + "\n" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := f.repo("x")
			a := &app{cfg: &config{command: tt.command(t)}}

			if err := a.runCommand(repo, newCapture("x", false, io.Discard)); err != nil {
				t.Fatalf("runCommand: %v", err)
			}

			if got, want := readFile(t, filepath.Join(repo, "out.txt")), tt.want(repo); got != want {
				t.Errorf("out.txt = %q, want %q", got, want)
			}
		})
	}
}

// runCommand is the one step mkprs does not control the behaviour of, so what
// is worth pinning is the boundary around it: where it runs, what it can see,
// and what comes back when it goes wrong. The context is the table above; these
// are the failure shapes, which never write a file at all.
func TestRunCommand(t *testing.T) {
	t.Parallel()

	t.Run("a command that succeeds", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		c := newCapture("x", false, io.Discard)
		a := &app{cfg: &config{command: helperCmd(t, "writeprint", "out.txt", "hello")}}

		if err := a.runCommand(repo, c); err != nil {
			t.Fatalf("runCommand: %v", err)
		}
		// Both streams reach the capture, which is what a failure replays.
		if got, want := c.String(), "hello\n"; got != want {
			t.Errorf("capture = %q, want %q", got, want)
		}
		// And the working directory was the repo, not the test's.
		if _, err := os.Stat(filepath.Join(repo, "out.txt")); err != nil {
			t.Errorf("command did not run in the repo: %v", err)
		}
	})

	t.Run("a command that exits non-zero", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		c := newCapture("x", false, io.Discard)
		a := &app{cfg: &config{command: helperCmd(t, "fail", "3", "went wrong")}}

		err := a.runCommand(f.repo("x"), c)
		if err == nil {
			t.Fatal("runCommand succeeded, want an error")
		}
		if got, want := err.Error(), "command exited 3"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
		// The command's own explanation stays in the capture rather than being
		// folded into the error, so it is replayed under the failure line.
		if got, want := c.String(), "went wrong\n"; got != want {
			t.Errorf("capture = %q, want %q", got, want)
		}
	})

	t.Run("a command that cannot start", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		a := &app{cfg: &config{command: []string{"mkprs-no-such-binary"}}}

		err := a.runCommand(f.repo("x"), newCapture("x", false, io.Discard))
		if err == nil {
			t.Fatal("runCommand succeeded, want an error")
		}
		// 127 is what a shell reports for "not found", and there is no exit
		// status to read: the process never ran.
		if got, want := err.Error(), "command exited 127"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})
}

// commitAndPush is everything between the command finishing and the PR: check
// the command stayed put, stage, commit, decide there is something to open a PR
// for, push. It is the half of processRepo that mutates, and the only one that
// can end a repo either way -- "no changes" is a skip, everything else a
// failure -- which is why it returns an outcome rather than an error.
func TestCommitAndPush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup runs with the working branch already checked out, standing in
		// for whatever the user's command did.
		setup func(t *testing.T, repo string)
		// want is the reason, and wantKind the variant it arrives as. An empty
		// reason means the repo carries on to the PR.
		want     string
		wantKind outcome
	}{
		{
			name: "the command edited the tree",
			setup: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "file.txt"), "changed\n")
			},
		},
		{
			// Nothing staged is not the same as nothing done, so this has to
			// reach the PR as well.
			name: "the command committed its own work",
			setup: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "file.txt"), "changed\n")
				gitCmd(t, repo, "add", "-A")
				gitCmd(t, repo, "commit", "-q", "-m", "committed by the command")
			},
		},
		{
			name:     "the command changed nothing",
			setup:    func(t *testing.T, repo string) {},
			want:     "command made no changes",
			wantKind: outcomeSkipped{},
		},
		{
			// Staging and committing act on whatever HEAD points at, so this
			// would otherwise commit to a branch mkprs does not own.
			name: "the command switched branch",
			setup: func(t *testing.T, repo string) {
				gitCmd(t, repo, "checkout", "-q", "-b", "elsewhere")
			},
			want:     "command left the repo on 'elsewhere', not 'b'",
			wantKind: outcomeFailed{},
		},
		{
			name: "the command detached HEAD",
			setup: func(t *testing.T, repo string) {
				gitCmd(t, repo, "checkout", "-q", "--detach")
			},
			want:     "command left the repo with a detached HEAD",
			wantKind: outcomeFailed{},
		},
		{
			name: "the branch cannot be pushed",
			setup: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "file.txt"), "changed\n")
				gitCmd(t, repo, "remote", "set-url", "origin", "git@github.com:fake/gone.git")
			},
			want:     "unable to push to origin/b",
			wantKind: outcomeFailed{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := f.repo("x")
			gitCmd(t, repo, "checkout", "-q", "-b", "b", "origin/main")
			tt.setup(t, repo)

			a := &app{cfg: &config{branch: "b", message: "commit msg"}}
			p := prep{defaultBranch: "main", base: "origin/main", startBranch: "main"}
			got := a.commitAndPush(repo, p, newCapture("x", false, io.Discard))

			if tt.want == "" {
				if got != nil {
					t.Fatalf("commitAndPush = %#v, want it to carry on", got)
				}
				// Carrying on means the branch is on origin for the PR to
				// point at, and carries the command's work.
				if !f.remoteHasBranch("x", "b") {
					t.Error("branch was not pushed")
				}
				if got, want := f.remoteFile("x", "b", "file.txt"), "changed"; got != want {
					t.Errorf("pushed file.txt = %q, want %q", got, want)
				}
				return
			}

			if got == nil {
				t.Fatalf("commitAndPush carried on, want %q", tt.want)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tt.wantKind) {
				t.Errorf("outcome = %T, want %T", got, tt.wantKind)
			}
			var reason string
			switch o := got.(type) {
			case outcomeSkipped:
				reason = o.reason
			case outcomeFailed:
				reason = o.reason
			}
			if reason != tt.want {
				t.Errorf("reason = %q, want %q", reason, tt.want)
			}
			// A repo that ends here never reaches origin.
			if f.remoteHasBranch("x", "b") {
				t.Error("branch was pushed despite the repo ending early")
			}
		})
	}
}
