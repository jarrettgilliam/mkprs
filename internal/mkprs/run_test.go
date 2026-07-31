package mkprs

import (
	"errors"
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

	t.Run("explicit title, body and reviewer are passed through", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "msg", "-t", "Title", "-B", "Body", "-r", "alice"}, helperCmd(t, "write", "file.txt", "x")...)

		want := pullRequest{Base: "main", Head: "b", Title: "Title", Body: "Body", Reviewer: "alice"}
		if got := prs.only(t).pr; got != want {
			t.Errorf("pullRequest = %+v, want %+v", got, want)
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

// The command sees the repo as its working directory, its path via {} and
// $REPO, and its name via $REPO_NAME.
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
			prs := &fakePR{}

			got := run(t, prs, []string{f.targets, "-b", "b"}, tt.command(t)...)
			if len(prs.calls) != 1 {
				t.Fatalf("no PR opened; output:\n%s", got.all())
			}

			if got, want := f.remoteFile("x", "b", "out.txt")+"\n", tt.want(repo); got != want {
				t.Errorf("out.txt = %q, want %q", got, want)
			}
		})
	}
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

// mkprs cuts its branch from the repo's default branch and returns there when it
// is done, so it only runs on a repo that started there. Anything else is a
// normal state of a repo, not a failure.
func TestRunRequiresDefaultBranch(t *testing.T) {
	t.Parallel()

	t.Run("on a feature branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "checkout", "-q", "-b", "feature")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d", got.code, exitOK)
		}
		if want := "not on the default branch (on 'feature', want 'main')"; !strings.Contains(got.stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", got.stdout, want)
		}
		if len(prs.calls) != 0 {
			t.Errorf("opened %d PRs, want none", len(prs.calls))
		}
		// Left where it was found -- the reason the check exists.
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

// A command is free to move HEAD -- `git checkout -b per-repo-name`, or a bare
// `git checkout my-feature` over work done by hand earlier. mkprs follows it,
// opens the PR from it, and never deletes a branch it did not create itself.
func TestRunFollowsTheCommandsBranch(t *testing.T) {
	t.Parallel()

	t.Run("a branch the command creates and commits to", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"},
			helperCmd(t, "gitcheckout", "other", "file.txt", "by the command")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d\n%s", got.code, exitOK, got.all())
		}
		if !f.remoteHasBranch("x", "other") {
			t.Fatalf("the command's branch was not pushed:\n%s", got.all())
		}
		call := prs.only(t)
		if call.pr.Head != "other" {
			t.Errorf("PR head = %q, want other", call.pr.Head)
		}
		if call.pr.Base != "main" {
			t.Errorf("PR base = %q, want main", call.pr.Base)
		}
		// The command's branch is the user's; only mkprs's own is cleaned up.
		if !localHasBranch(t, repo, "other") {
			t.Error("the command's branch was deleted, it should survive")
		}
		if localHasBranch(t, repo, "b") {
			t.Error("mkprs's own branch should have been deleted")
		}
		if got := currentBranch(t, repo); got != "main" {
			t.Errorf("left on branch %q, want main", got)
		}
	})

	t.Run("a branch the command creates and leaves dirty", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		prs := &fakePR{}

		run(t, prs, []string{f.targets, "-b", "b", "-m", "mkprs message"},
			helperCmd(t, "gitcheckout", "other", "committed.txt", "by the command", "leftover.txt")...)

		if got, want := f.remoteFile("x", "other", "leftover.txt"), "left behind"; got != want {
			t.Errorf("leftover file = %q, want %q", got, want)
		}
		if got, want := f.remoteSubject("x", "other"), "mkprs message"; got != want {
			t.Errorf("commit subject = %q, want %q", got, want)
		}
	})

	// The "manual work already done, now automate the PR" case: the command is
	// nothing but a checkout, and the commits were made by hand earlier.
	t.Run("a pre-existing branch already ahead of the default", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "checkout", "-q", "-b", "by-hand")
		writeFile(t, filepath.Join(repo, "file.txt"), "done by hand\n")
		gitCmd(t, repo, "commit", "-q", "-a", "-m", "hand-written work")
		gitCmd(t, repo, "checkout", "-q", "main")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"}, helperCmd(t, "gitcheckout", "by-hand")...)

		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d\n%s", got.code, exitOK, got.all())
		}
		call := prs.only(t)
		if call.pr.Head != "by-hand" || call.pr.Base != "main" {
			t.Errorf("PR = %+v, want head by-hand onto main", call.pr)
		}
		if got, want := f.remoteSubject("x", "by-hand"), "hand-written work"; got != want {
			t.Errorf("pushed subject = %q, want %q", got, want)
		}
		if !localHasBranch(t, repo, "by-hand") {
			t.Error("a branch mkprs did not create must never be deleted")
		}
	})

	// Safety: without this guard the run would commit to the default branch and
	// push it, bypassing review entirely.
	t.Run("the default branch is refused", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("x")
		before := f.remoteSubject("x", "main")
		prs := &fakePR{}

		got := run(t, prs, []string{f.targets, "-b", "b"},
			helperCmd(t, "gitcheckout", "main", "file.txt", "sneaky")...)

		// One repo failing is not fatal to the run; the ❌ line carries it.
		if got.code != exitOK {
			t.Errorf("exit code = %d, want %d", got.code, exitOK)
		}
		if want := "a PR needs a branch to open from"; !strings.Contains(got.stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", got.stdout, want)
		}
		if len(prs.calls) != 0 {
			t.Errorf("opened %d PRs, want none", len(prs.calls))
		}
		if got := f.remoteSubject("x", "main"); got != before {
			t.Errorf("origin/main moved to %q, want it untouched at %q", got, before)
		}
	})
}

// Skips are normal outcomes, not errors: the run still exits 0.
func TestRunSkips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, f *fixture) string // returns the repo path
		command func(t *testing.T) []string
		want    string
	}{
		{
			name: "non-GitHub remote",
			setup: func(t *testing.T, f *fixture) string {
				return f.repoWithRemote("x", "git@gitlab.com:fake/x.git")
			},
			want: "skipped: non-GitHub remote",
		},
		{
			name: "no origin at all",
			setup: func(t *testing.T, f *fixture) string {
				return f.plainRepo("x")
			},
			want: "skipped: no 'origin' remote",
		},
		{
			name: "dirty working tree",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				writeFile(t, filepath.Join(repo, "file.txt"), "uncommitted\n")
				return repo
			},
			want: "skipped: working tree not clean",
		},
		{
			name: "branch already exists locally",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "branch", "b")
				return repo
			},
			want: "skipped: branch 'b' already exists locally",
		},
		{
			name: "branch already exists on origin",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/b")
				gitCmd(t, repo, "fetch", "-q", "origin")
				return repo
			},
			want: "skipped: branch 'b' already exists on origin",
		},
		{
			name: "command made no changes",
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
		// The half-finished branch is cleaned up.
		if localHasBranch(t, repo, "b") {
			t.Error("the working branch should have been deleted")
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
		if localHasBranch(t, repo, "b") {
			t.Error("the working branch should have been deleted")
		}
		if got := currentBranch(t, repo); got != "main" {
			t.Errorf("left on branch %q, want main", got)
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
		if localHasBranch(t, repo, "b") {
			t.Error("the local branch should still be cleaned up")
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

// By default a repo's own output is captured, not printed. --verbose streams it
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
