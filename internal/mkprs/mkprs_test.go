package mkprs

import (
	"errors"
	"os"
	"path/filepath"
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

	if !f.remoteHasBranch("good", "greet") {
		t.Fatal("branch was not pushed to origin")
	}
	if got, want := f.remoteFile("good", "greet", "file.txt"), "hello world"; got != want {
		t.Errorf("pushed file = %q, want %q", got, want)
	}
	if got, want := f.remoteSubject("good", "greet"), "Say hello"; got != want {
		t.Errorf("commit subject = %q, want %q", got, want)
	}

	if got := currentBranch(t, repo); got != "main" {
		t.Errorf("left on branch %q, want main", got)
	}
	if localHasBranch(t, repo, "greet") {
		t.Error("the working branch should have been deleted")
	}
}

func TestRunPullRequestFields(t *testing.T) {
	t.Parallel()

	// The defaults themselves are TestParseArgsFillsMessageAndTitle's; what
	// needs a whole run is that the defaulted message reaches git.
	t.Run("the defaulted message becomes the commit subject", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		command := helperCmd(t, "write", "file.txt", "x")
		run(t, prs, []string{f.targets, "-b", "b"}, command...)

		if got, want := f.remoteSubject("x", "b"), strings.Join(command, " "); got != want {
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

	// With nothing staged there is nothing for mkprs to commit, so the
	// command's own commit is the only one on the branch.
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

			if got.code != exitFailure {
				t.Errorf("exit code = %d, want %d", got.code, exitFailure)
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
				return f.repoWithRemote("x", "git@gitlab.com:fake/x.git")
			},
			want: "skipped: non-GitHub remote",
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
	if got := at(repo).branchLocation("gone"); got != "on origin" {
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

		if got.code != exitFailure {
			t.Errorf("exit code = %d, want %d", got.code, exitFailure)
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

	// TestPreflight covers the outcome and its reason; what only a whole run can
	// show is that failing there costs the repo nothing -- no branch, and the
	// command never runs. That is the entire argument for failing on the fetch
	// rather than at the push it was going to fail at anyway.
	t.Run("a failed fetch touches nothing", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		if err := os.RemoveAll(f.bare("x")); err != nil {
			t.Fatalf("remove the remote: %v", err)
		}

		got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stdout, "❌ x could not fetch origin") {
			t.Errorf("stdout = %q, want a fetch failure", got.stdout)
		}
		if localHasBranch(t, repo, "b") {
			t.Error("the working branch should never have been created")
		}
		if got, want := readFile(t, filepath.Join(repo, "file.txt")), "hello\n"; got != want {
			t.Errorf("file.txt = %q, want %q -- the command should not have run", got, want)
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

// -s/--stop-on-failure is about the repos after the failure: when the command
// itself is wrong, one repo's worth of output is the diagnosis and the rest is
// noise. A skip is a normal result and does not stop anything.
func TestRunStopOnFailure(t *testing.T) {
	t.Parallel()

	t.Run("stops at the first failing repo", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("a")
		second := f.repo("b")
		third := f.repo("c")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "wip", "--stop-on-failure"}, helperCmd(t, "fail", "3", "it went wrong")...)

		// -s changes how much of the run happens, not whether it succeeded.
		if got.code != exitFailure {
			t.Errorf("exit code = %d, want %d", got.code, exitFailure)
		}
		if !strings.Contains(got.stdout, "❌ a command exited 3") {
			t.Errorf("stdout = %q, want the first repo's failure", got.stdout)
		}
		if !strings.Contains(got.stdout, "Failed:        1") {
			t.Errorf("summary counted more than the one failure:\n%s", got.stdout)
		}
		// The repos after it were never touched -- no branch, no result line.
		for _, repo := range []string{second, third} {
			name := filepath.Base(repo)
			if strings.Contains(got.stdout, "❌ "+name) {
				t.Errorf("%s was processed after the failure:\n%s", name, got.stdout)
			}
			if localHasBranch(t, repo, "wip") {
				t.Errorf("%s has a working branch, so it was processed", name)
			}
			if got := currentBranch(t, repo); got != "main" {
				t.Errorf("%s left on branch %q, want main", name, got)
			}
		}
	})

	// The repos left out get their own counter rather than joining the skips:
	// nothing looked at them, and every ⏭️ line on screen has to keep matching
	// the number the summary reports.
	t.Run("says how many repos it did not reach", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("a")
		f.repo("b")
		f.repo("c")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "wip", "--stop-on-failure"}, helperCmd(t, "fail", "1", "nope")...)

		if !strings.Contains(got.stdout, "Stopped at the first failure.") {
			t.Errorf("stdout = %q, want a note that the results end early", got.stdout)
		}
		for _, want := range []string{"Failed:        1", "Skipped:       0", "Not processed: 2"} {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("summary missing %q:\n%s", want, got.stdout)
			}
		}
	})

	t.Run("says nothing when the last repo is the one that fails", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("a")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "wip", "--stop-on-failure"}, helperCmd(t, "fail", "1", "nope")...)

		if strings.Contains(got.stdout, "Stopped") {
			t.Errorf("stdout = %q, want no note when nothing was left out", got.stdout)
		}
		if !strings.Contains(got.stdout, "Failed:    1") {
			t.Errorf("summary should keep its three-line shape:\n%s", got.stdout)
		}
	})

	t.Run("a skip does not stop the run", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repoWithRemote("a", "git@gitlab.com:fake/a.git")
		f.repo("b")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "wip", "--stop-on-failure"}, helperCmd(t, "write", "file.txt", "changed")...)

		for _, want := range []string{"Succeeded: 1", "Failed:    0", "Skipped:   1"} {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("summary missing %q:\n%s", want, got.stdout)
			}
		}
		if strings.Contains(got.stdout, "Stopped") {
			t.Errorf("stdout = %q, want the run to carry on past a skip", got.stdout)
		}
	})

	// The flag asks "will this run have to happen again?", which a starting
	// state answers as readily as a command does -- so a dirty repo stops it.
	t.Run("a wrong starting state stops the run", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dirty := f.repo("a")
		writeFile(t, filepath.Join(dirty, "file.txt"), "uncommitted\n")
		f.repo("b")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "wip", "--stop-on-failure"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stdout, "❌ a working tree not clean") {
			t.Errorf("stdout missing the failure:\n%s", got.stdout)
		}
		if !strings.Contains(got.stdout, "Not processed: 1") {
			t.Errorf("stdout = %q, want the second repo left untouched", got.stdout)
		}
		if len(prs.calls) != 0 {
			t.Errorf("opened %d PRs, want none", len(prs.calls))
		}
	})

	// The default is what it was: every repo runs, however many fail.
	t.Run("without the flag every repo runs", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("a")
		f.repo("b")
		f.repo("c")

		got := run(t, &fakePR{}, []string{f.targets, "-b", "wip"}, helperCmd(t, "fail", "1", "nope")...)

		if !strings.Contains(got.stdout, "Failed:    3") {
			t.Errorf("summary did not count every failure:\n%s", got.stdout)
		}
	})
}

// -k skips cleanup outright rather than skipping only the failures: the repo is
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

// A bad target stops the run before any repo is touched: carrying on would let
// the other targets open pull requests that Ctrl-C cannot take back.
func TestRunRejectsABadTargetDir(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("x")
	missing := filepath.Join(f.root, "nope")
	prs := &fakePR{}

	got := run(t, prs, []string{f.targets, missing, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	if got.code != exitUsage {
		t.Errorf("exit code = %d, want %d", got.code, exitUsage)
	}
	if !strings.Contains(got.stderr, "target directory does not exist: "+missing) {
		t.Errorf("stderr = %q, want the reason", got.stderr)
	}
	// The valid target is not a consolation prize: nothing runs.
	if len(prs.calls) != 0 {
		t.Errorf("opened %d PRs, want none", len(prs.calls))
	}
	if strings.Contains(got.stdout, "PR created") {
		t.Errorf("a repo was processed despite the bad target:\n%s", got.stdout)
	}
}

// `mkprs ~/repos/*` sweeps up whatever sits alongside the repos. Those targets
// are ignored rather than fatal -- but counted, so a glob that matched nothing
// useful does not look like a run with nothing to do.
func TestRunIgnoresTargetsWithoutRepos(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	notes := filepath.Join(f.root, "notes")
	mkdir(t, notes)
	readme := filepath.Join(f.root, "README.md")
	writeFile(t, readme, "x")
	prs := &fakePR{}

	got := run(t, prs, []string{repo, notes, readme, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	if got.code != exitOK {
		t.Errorf("exit code = %d, want %d", got.code, exitOK)
	}
	if len(prs.calls) != 1 {
		t.Fatalf("opened %d PRs, want 1:\n%s", len(prs.calls), got.all())
	}
	if !strings.Contains(got.stderr, "Ignored 2 targets with no repositories.") {
		t.Errorf("stderr = %q, want the ignored targets reported", got.stderr)
	}
	// The count is the whole story when quiet: forty repos and three strays
	// should not cost three lines every run.
	for _, path := range []string{notes, readme} {
		if strings.Contains(got.stderr, path) {
			t.Errorf("stderr named %q without --verbose:\n%s", path, got.stderr)
		}
	}
}

// Under --verbose the count is not enough: which targets were dropped is
// exactly what you need to see when a glob did not match what you expected.
func TestRunVerboseListsIgnoredTargets(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	notes := filepath.Join(f.root, "notes")
	mkdir(t, notes)
	readme := filepath.Join(f.root, "README.md")
	writeFile(t, readme, "x")

	got := run(t, &fakePR{}, []string{repo, notes, readme, "-b", "b", "-v"}, helperCmd(t, "write", "file.txt", "changed")...)

	if !strings.Contains(got.stderr, "Ignored 2 targets with no repositories.") {
		t.Errorf("stderr = %q, want the count as well", got.stderr)
	}
	for _, path := range []string{notes, readme} {
		if !strings.Contains(got.stderr, path) {
			t.Errorf("stderr = %q, want it to name %q", got.stderr, path)
		}
	}
}

// Overlapping targets name the same repo once. Processing it twice would open
// the PR on the first pass, fail on the second with `branch 'b' already exists
// on origin`, and report two outcomes for one repo.
func TestRunDeduplicatesRepos(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	prs := &fakePR{}

	got := run(t, prs, []string{f.targets, repo, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	if got.code != exitOK {
		t.Errorf("exit code = %d, want %d", got.code, exitOK)
	}
	if len(prs.calls) != 1 {
		t.Errorf("opened %d PRs, want 1", len(prs.calls))
	}
	if strings.Contains(got.stdout, "already exists") {
		t.Errorf("the repo was processed twice:\n%s", got.stdout)
	}
	for _, want := range []string{"Succeeded: 1", "Skipped:   0"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("summary missing %q:\n%s", want, got.stdout)
		}
	}
	if !strings.Contains(got.stderr, "Ignored 1 duplicate repository.") {
		t.Errorf("stderr = %q, want the duplicate to be reported", got.stderr)
	}
	if strings.Contains(got.stderr, repo) {
		t.Errorf("stderr named %q without --verbose:\n%s", repo, got.stderr)
	}

	t.Run("verbose names it", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")

		got := run(t, &fakePR{}, []string{f.targets, repo, "-b", "b", "-v"}, helperCmd(t, "write", "file.txt", "changed")...)

		if !strings.Contains(got.stderr, "Ignored 1 duplicate repository.") {
			t.Errorf("stderr = %q, want the count as well", got.stderr)
		}
		if !strings.Contains(got.stderr, repo) {
			t.Errorf("stderr = %q, want it to name %q", got.stderr, repo)
		}
	})
}

// --max-repos is the only thing standing between a mistyped target and a
// hundred pull requests in other people's repos, so it fails before the first
// repo is touched -- nothing to undo is the whole point.
func TestRunMaxRepos(t *testing.T) {
	t.Parallel()

	t.Run("refuses a run above the limit", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		a := f.repo("a")
		b := f.repo("b")
		c := f.repo("c")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "wip", "--max-repos", "2"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitUsage {
			t.Errorf("exit code = %d, want %d", got.code, exitUsage)
		}
		// The message is the fix: the count, the limit, and the flag that
		// proceeds. Someone reading it should not have to count repos.
		for _, want := range []string{"found 3 repositories", "--max-repos limit of 2", "re-run with --max-repos 3"} {
			if !strings.Contains(got.stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, got.stderr)
			}
		}
		if len(prs.calls) != 0 {
			t.Errorf("opened %d PRs, want none", len(prs.calls))
		}
		for _, repo := range []string{a, b, c} {
			if localHasBranch(t, repo, "wip") {
				t.Errorf("%s was touched despite the limit", filepath.Base(repo))
			}
		}
		if got.stdout != "" {
			t.Errorf("stdout = %q, want the whole run on stderr", got.stdout)
		}
	})

	t.Run("a run at the limit proceeds", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("a")
		f.repo("b")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "wip", "--max-repos", "2"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d\nstderr: %s", got.code, exitOK, got.stderr)
		}
		if len(prs.calls) != 2 {
			t.Errorf("opened %d PRs, want 2", len(prs.calls))
		}
	})

	// Counted after dedupeRepos: a repo reached from two targets is one repo, and
	// should not spend two of the budget.
	t.Run("counts a repo named twice is counted once", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, repo, "-b", "wip", "--max-repos", "1"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d\nstderr: %s", got.code, exitOK, got.stderr)
		}
		if len(prs.calls) != 1 {
			t.Errorf("opened %d PRs, want 1", len(prs.calls))
		}
	})

	t.Run("zero disables the limit", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("a")
		f.repo("b")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "wip", "--max-repos", "0"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d\nstderr: %s", got.code, exitOK, got.stderr)
		}
		if len(prs.calls) != 2 {
			t.Errorf("opened %d PRs, want 2", len(prs.calls))
		}
	})
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

// A failure is the one place the whole capture is replayed, and there is
// nowhere else to read it from. Nothing is truncated and nothing is held back,
// the PR opener's own output included.
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

	// "failed to create PR" on its own says nothing about why.
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

	// ...and under --verbose it streams live instead.
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
	dirty := f.repo("fails")
	writeFile(t, filepath.Join(dirty, "file.txt"), "uncommitted\n")
	f.repoWithRemote("skips", "git@gitlab.com:fake/skips.git")

	got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	for _, want := range []string{"Succeeded: 1", "Failed:    1", "Skipped:   1"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("summary missing %q:\n%s", want, got.stdout)
		}
	}
}

// =============================================================================
// The pieces of processRepo, tested directly
//
// The tests above drive a whole run: the wiring, the reporting, and the
// interactions between steps. A condition that only decides one step's answer
// belongs down here, where it costs a fixture instead of a full run.
// =============================================================================

// Two of these rows -- {} twice over, and {} inside a larger argument -- are
// cases no single command line can stage.

// The exit code answers "did it work?" for a caller that is not a human reading
// the summary, so the interesting cases are the ones the result lines already
// distinguish: a skip is not a failure, and one failure among successes is. The
// runs that never reach a repo are in TestRunExitCodesBeforeAnyRepo (cli_test).
func TestRunExitCodesFromRepoOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
		cmd   []string
		want  int
	}{
		{
			name:  "every repo succeeded",
			setup: func(t *testing.T, f *fixture) { f.repo("a"); f.repo("b") },
			cmd:   []string{"write", "file.txt", "changed"},
			want:  exitOK,
		},
		{
			name:  "a skip is not a failure",
			setup: func(t *testing.T, f *fixture) { f.repo("a") },
			cmd:   []string{"noop"},
			want:  exitOK,
		},
		{
			name: "one failure among successes",
			setup: func(t *testing.T, f *fixture) {
				f.repo("a")
				dirty := f.repo("dirty")
				writeFile(t, filepath.Join(dirty, "stray.txt"), "uncommitted")
			},
			cmd:  []string{"write", "file.txt", "changed"},
			want: exitFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			tt.setup(t, f)

			got := run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, tt.cmd[0], tt.cmd[1:]...)...)

			if got.code != tt.want {
				t.Errorf("exit code = %d, want %d\n%s", got.code, tt.want, got.stdout)
			}
		})
	}
}
