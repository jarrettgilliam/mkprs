package mkprs

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A ref that does not exist: stdout empty, stderr a complaint, exit non-zero.
// Without --quiet, so that there is a complaint to follow.
var badRef = gitArgs{"rev-parse", "--verify", "refs/heads/nope"}

// The builder's whole point is that the two axes are independent, so that is
// what is worth asserting: one command, each shape.
func TestGitRunStreams(t *testing.T) {
	t.Parallel()

	t.Run("text returns stdout", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		got, err := git(f.repo("x"), currentHeadRef()).text()
		if err != nil || got != "main" {
			t.Errorf("text = %q, %v; want main, nil", got, err)
		}
	})

	t.Run("to writes both streams", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		var out bytes.Buffer
		if err := git(f.repo("x"), badRef).to(&out, &out).run(); err == nil {
			t.Fatal("run err = nil, want a failure")
		}
		if !strings.Contains(out.String(), "fatal:") {
			t.Errorf("output = %q, want git's complaint", out.String())
		}
	})

	// A nil out exists for `git branch -D`, whose stdout is noise.
	t.Run("a nil out discards stdout", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "branch", "work")

		var out bytes.Buffer
		if err := git(dir, deleteBranch("work")).to(nil, &out).run(); err != nil {
			t.Fatalf("run err = %v, want nil", err)
		}
		if out.Len() != 0 {
			t.Errorf("output = %q, want stdout discarded", out.String())
		}

		// Same command again, now that the branch is gone.
		if err := git(dir, deleteBranch("work")).to(nil, &out).run(); err == nil {
			t.Fatal("run err = nil, want a failure")
		}
		if !strings.Contains(out.String(), "not found") {
			t.Errorf("output = %q, want git's complaint", out.String())
		}
	})

	t.Run("ok reports the exit status", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		if !git(dir, localBranchExists("main")).ok() {
			t.Error("ok = false for a branch that exists")
		}
		if git(dir, badRef).ok() {
			t.Error("ok = true for a ref that does not exist")
		}
	})
}

// git's error has to carry git's own words: "exit status 128" alone tells a
// reader nothing, and stderr never reaches the capture on this path.
func TestGitErrorCarriesStderr(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	_, err := git(f.repo("x"), badRef).text()
	if err == nil {
		t.Fatal("text err = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "fatal:") {
		t.Errorf("err = %q, want git's stderr included", err)
	}
}

// The other half of that rule: once the complaint is in the capture, the error
// must not repeat it, or a failed repo prints it twice.
func TestLoggedErrorDoesNotRepeatStderr(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	var out bytes.Buffer

	err := git(f.repo("x"), badRef).to(&out, &out).run()
	if err == nil {
		t.Fatal("run err = nil, want a failure")
	}
	if strings.Contains(err.Error(), "fatal:") {
		t.Errorf("err = %q, want git's complaint left to the capture", err)
	}
}

func TestIsGitHubRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		remote  string
		wantOK  bool
		wantMsg string
	}{
		{"ssh url", "git@github.com:fake/x.git", true, ""},
		{"https url", "https://github.com/fake/x.git", true, ""},
		{"gitlab", "git@gitlab.com:fake/x.git", false, "non-GitHub remote"},
		{"no origin", "", false, "no 'origin' remote"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			dir := f.repoWithRemote("x", tt.remote)

			ok, msg := at(dir).isGitHubRepo()
			if ok != tt.wantOK {
				t.Errorf("isGitHubRepo = %v, want %v (%s)", ok, tt.wantOK, msg)
			}
			if !strings.Contains(msg, tt.wantMsg) {
				t.Errorf("reason = %q, want it to contain %q", msg, tt.wantMsg)
			}
		})
	}
}

func TestIsCleanTree(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	dir := f.repo("x")

	if clean, ok := at(dir).isCleanTree(); !ok || !clean {
		t.Fatalf("isCleanTree = %v, %v; want a freshly committed repo to be clean", clean, ok)
	}

	writeFile(t, filepath.Join(dir, "file.txt"), "changed\n")
	if clean, ok := at(dir).isCleanTree(); !ok || clean {
		t.Errorf("isCleanTree = %v, %v; want a modified repo to be dirty", clean, ok)
	}
}

// A status that could not run at all is a different answer from a dirty tree.
// Both fail the repo, but only one of them is about the working tree, and the
// reason line has to name the right cause.
func TestIsCleanTreeUnreadable(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	notARepo := filepath.Join(f.root, "notes")
	mkdir(t, notARepo)

	if clean, ok := at(notARepo).isCleanTree(); ok {
		t.Errorf("isCleanTree = %v, %v; want the status to be unanswerable", clean, ok)
	}
}

// An untracked file counts as dirty too -- committing it would sweep up
// whatever the developer left lying around.
func TestIsCleanTreeUntracked(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	dir := f.repo("x")
	writeFile(t, filepath.Join(dir, "stray.txt"), "x\n")

	if clean, ok := at(dir).isCleanTree(); !ok || clean {
		t.Errorf("isCleanTree = %v, %v; want an untracked file to make the tree dirty", clean, ok)
	}
}

func TestBranchLocation(t *testing.T) {
	t.Parallel()

	t.Run("absent", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got := at(f.repo("x")).branchLocation("nope"); got != "" {
			t.Errorf("branchLocation = %q, want empty", got)
		}
	})

	t.Run("local", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "branch", "here")

		if got := at(dir).branchLocation("here"); got != "locally" {
			t.Errorf("branchLocation = %q, want %q", got, "locally")
		}
	})

	t.Run("on origin", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "push", "-q", "origin", "HEAD:refs/heads/upstream-only")
		gitCmd(t, dir, "fetch", "-q", "origin")

		if got := at(dir).branchLocation("upstream-only"); got != "on origin" {
			t.Errorf("branchLocation = %q, want %q", got, "on origin")
		}
	})

	t.Run("local wins when both exist", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "push", "-q", "origin", "HEAD:refs/heads/both")
		gitCmd(t, dir, "fetch", "-q", "origin")
		gitCmd(t, dir, "branch", "both")

		if got := at(dir).branchLocation("both"); got != "locally" {
			t.Errorf("branchLocation = %q, want %q", got, "locally")
		}
	})
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()

	t.Run("reads origin/HEAD", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		got, ok := at(f.repo("x")).defaultBranch()
		if !ok || got != "main" {
			t.Errorf("defaultBranch = %q, %v; want main, true", got, ok)
		}
	})

	t.Run("honors a non-main default", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("dev-default")
		// Re-point both the repo and its remote at develop.
		gitCmd(t, dir, "branch", "develop")
		gitCmd(t, dir, "push", "-q", "origin", "develop")
		gitCmd(t, f.bare("dev-default"), "symbolic-ref", "HEAD", "refs/heads/develop")
		gitCmd(t, dir, "push", "-q", "origin", "--delete", "main")
		gitCmd(t, dir, "checkout", "-q", "develop")
		gitCmd(t, dir, "branch", "-q", "-D", "main")
		gitCmd(t, dir, "remote", "set-head", "origin", "develop")
		gitCmd(t, dir, "fetch", "-q", "origin", "--prune")

		got, ok := at(dir).defaultBranch()
		if !ok || got != "develop" {
			t.Errorf("defaultBranch = %q, %v; want develop, true", got, ok)
		}
	})

	t.Run("reports failure without a remote", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got, ok := at(f.plainRepo("x")).defaultBranch(); ok {
			t.Errorf("defaultBranch = %q, %v; want \"\", false", got, ok)
		}
	})
}

// New branches start from what origin has, not from a stale local branch.
func TestResolveBase(t *testing.T) {
	t.Parallel()

	t.Run("prefers the remote-tracking ref", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got := at(f.repo("x")).resolveBase("main"); got != "origin/main" {
			t.Errorf("resolveBase = %q, want origin/main", got)
		}
	})

	t.Run("falls back to the local branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got := at(f.plainRepo("x")).resolveBase("main"); got != "main" {
			t.Errorf("resolveBase = %q, want main", got)
		}
	})
}

func TestHeadBranch(t *testing.T) {
	t.Parallel()

	t.Run("on a branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		name, ok := at(f.repo("x")).headBranch()
		if !ok || name != "main" {
			t.Errorf("headBranch = %q, %v, want main, true", name, ok)
		}
	})

	// A detached HEAD is not a branch, and callers have to be able to tell.
	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "checkout", "-q", "--detach")

		if name, ok := at(dir).headBranch(); ok {
			t.Errorf("headBranch = %q, true; want ok false when detached", name)
		}
	})
}

// Whether a repo has anything to open a PR for is decided by the branch, not
// the index, so that a commit the command made itself still counts.
func TestBranchAhead(t *testing.T) {
	t.Parallel()

	t.Run("branch with a commit of its own", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "checkout", "-q", "-b", "work")
		writeFile(t, filepath.Join(dir, "file.txt"), "changed\n")
		gitCmd(t, dir, "commit", "-q", "-a", "-m", "work")

		ahead, ok := at(dir).branchAhead("main", "work")
		if !ok {
			t.Fatal("branchAhead could not answer")
		}
		if !ahead {
			t.Error("ahead = false, want true")
		}
	})

	t.Run("branch level with its base", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		dir := f.repo("x")
		gitCmd(t, dir, "checkout", "-q", "-b", "work")

		ahead, ok := at(dir).branchAhead("main", "work")
		if !ok {
			t.Fatal("branchAhead could not answer")
		}
		if ahead {
			t.Error("ahead = true, want false")
		}
	})

	// An unanswerable question must not look like "nothing happened": that path
	// skips the repo, and skipping deletes the branch.
	t.Run("unknown ref cannot be answered", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if _, ok := at(f.repo("x")).branchAhead("main", "no-such-branch"); ok {
			t.Error("ok = true, want false for a ref that does not exist")
		}
	})
}

func TestRestoreRepo(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	dir := f.repo("x")
	gitCmd(t, dir, "checkout", "-q", "-b", "work")

	at(dir).restore("main", "work")

	if got := currentBranch(t, dir); got != "main" {
		t.Errorf("current branch = %q, want main", got)
	}
	if localHasBranch(t, dir, "work") {
		t.Error("the work branch should have been deleted")
	}
}

// The rules themselves are git's, so this asserts the delegation rather than
// re-listing them: a handful that must pass, and one per class that must not.
// The leading dash is the case check-ref-format does not cover on its own.
func TestValidateBranchName(t *testing.T) {
	t.Parallel()

	valid := []string{"bump-deps", "feature/x", "fix.typo", "release-1.2"}
	invalid := []string{"--draft", "-x", "my branch", "a..b", "x.lock", "x/", "a@{b}", ""}

	for _, name := range valid {
		t.Run("valid/"+name, func(t *testing.T) {
			t.Parallel()

			if err := validateBranchName(name); err != nil {
				t.Errorf("validateBranchName(%q) = %v, want nil", name, err)
			}
		})
	}

	for _, name := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			t.Parallel()

			err := validateBranchName(name)
			if err == nil {
				t.Fatalf("validateBranchName(%q) = nil, want an error", name)
			}
			// The branch has to appear in the message: the whole point is to
			// say which name was rejected, once, instead of per repo.
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error = %q, want it to name %q", err, name)
			}
		})
	}
}
