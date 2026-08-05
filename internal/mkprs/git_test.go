package mkprs

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The helpers are told apart by what they do with the two streams, so that is
// what is worth asserting: one command, four shapes.
func TestGitHelperStreams(t *testing.T) {
	t.Parallel()

	// A ref that does not exist: stdout empty, stderr a complaint, exit non-zero.
	bad := []string{"rev-parse", "--verify", "refs/heads/nope"}

	t.Run("git returns stdout and hides stderr", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		out, err := git(f.repo("x"), "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil || out != "main" {
			t.Errorf("git = %q, %v; want main, nil", out, err)
		}
	})

	// gitTo is the streaming shape: both halves reach the capture.
	t.Run("gitTo writes both streams", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		var out bytes.Buffer
		if err := gitTo(f.repo("x"), &out, bad...); err == nil {
			t.Fatal("gitTo err = nil, want a failure")
		}
		if !strings.Contains(out.String(), "fatal:") {
			t.Errorf("output = %q, want git's complaint", out.String())
		}
	})

	// gitErrTo exists for `git branch -D`, whose stdout is noise.
	t.Run("gitErrTo writes stderr only", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "branch", "work")

		var out bytes.Buffer
		if err := gitErrTo(repo, &out, "branch", "-D", "work"); err != nil {
			t.Fatalf("gitErrTo err = %v, want nil", err)
		}
		if out.Len() != 0 {
			t.Errorf("output = %q, want stdout discarded", out.String())
		}

		// Same command again, now that the branch is gone.
		if err := gitErrTo(repo, &out, "branch", "-D", "work"); err == nil {
			t.Fatal("gitErrTo err = nil, want a failure")
		}
		if !strings.Contains(out.String(), "not found") {
			t.Errorf("output = %q, want git's complaint", out.String())
		}
	})
}

// git's error has to carry git's own words: "exit status 128" alone tells a
// reader nothing, and stderr never reaches the capture on this path.
func TestGitErrorCarriesStderr(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	_, err := git(f.repo("x"), "rev-parse", "--verify", "refs/heads/nope")
	if err == nil {
		t.Fatal("git err = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "fatal:") {
		t.Errorf("err = %q, want git's stderr included", err)
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
			repo := f.repoWithRemote("x", tt.remote)

			ok, msg := isGitHubRepo(repo)
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
	repo := f.repo("x")

	if !isCleanTree(repo) {
		t.Fatal("a freshly committed repo should be clean")
	}

	writeFile(t, filepath.Join(repo, "file.txt"), "changed\n")
	if isCleanTree(repo) {
		t.Error("a modified repo should not be clean")
	}
}

// An untracked file counts as dirty too -- committing it would sweep up
// whatever the developer left lying around.
func TestIsCleanTreeUntracked(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	writeFile(t, filepath.Join(repo, "stray.txt"), "x\n")

	if isCleanTree(repo) {
		t.Error("an untracked file should make the tree dirty")
	}
}

func TestBranchLocation(t *testing.T) {
	t.Parallel()

	t.Run("absent", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got := branchLocation(f.repo("x"), "nope"); got != "" {
			t.Errorf("branchLocation = %q, want empty", got)
		}
	})

	t.Run("local", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "branch", "here")

		if got := branchLocation(repo, "here"); got != "locally" {
			t.Errorf("branchLocation = %q, want %q", got, "locally")
		}
	})

	t.Run("on origin", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/upstream-only")
		gitCmd(t, repo, "fetch", "-q", "origin")

		if got := branchLocation(repo, "upstream-only"); got != "on origin" {
			t.Errorf("branchLocation = %q, want %q", got, "on origin")
		}
	})

	t.Run("local wins when both exist", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/both")
		gitCmd(t, repo, "fetch", "-q", "origin")
		gitCmd(t, repo, "branch", "both")

		if got := branchLocation(repo, "both"); got != "locally" {
			t.Errorf("branchLocation = %q, want %q", got, "locally")
		}
	})
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()

	t.Run("reads origin/HEAD", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		got, ok := getDefaultBranch(f.repo("x"))
		if !ok || got != "main" {
			t.Errorf("defaultBranch = %q, %v; want main, true", got, ok)
		}
	})

	t.Run("honours a non-main default", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("dev-default")
		// Re-point both the repo and its remote at develop.
		gitCmd(t, repo, "branch", "develop")
		gitCmd(t, repo, "push", "-q", "origin", "develop")
		gitCmd(t, f.bare("dev-default"), "symbolic-ref", "HEAD", "refs/heads/develop")
		gitCmd(t, repo, "push", "-q", "origin", "--delete", "main")
		gitCmd(t, repo, "checkout", "-q", "develop")
		gitCmd(t, repo, "branch", "-q", "-D", "main")
		gitCmd(t, repo, "remote", "set-head", "origin", "develop")
		gitCmd(t, repo, "fetch", "-q", "origin", "--prune")

		got, ok := getDefaultBranch(repo)
		if !ok || got != "develop" {
			t.Errorf("defaultBranch = %q, %v; want develop, true", got, ok)
		}
	})

	t.Run("reports failure without a remote", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got, ok := getDefaultBranch(f.plainRepo("x")); ok {
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
		if got := resolveBase(f.repo("x"), "main"); got != "origin/main" {
			t.Errorf("resolveBase = %q, want origin/main", got)
		}
	})

	t.Run("falls back to the local branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		if got := resolveBase(f.plainRepo("x"), "main"); got != "main" {
			t.Errorf("resolveBase = %q, want main", got)
		}
	})
}

func TestHeadBranch(t *testing.T) {
	t.Parallel()

	t.Run("on a branch", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		name, ok := headBranch(f.repo("x"))
		if !ok || name != "main" {
			t.Errorf("headBranch = %q, %v, want main, true", name, ok)
		}
	})

	// A detached HEAD is not a branch, and callers have to be able to tell.
	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		gitCmd(t, repo, "checkout", "-q", "--detach")

		if name, ok := headBranch(repo); ok {
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
		repo := f.repo("x")
		gitCmd(t, repo, "checkout", "-q", "-b", "work")
		writeFile(t, filepath.Join(repo, "file.txt"), "changed\n")
		gitCmd(t, repo, "commit", "-q", "-a", "-m", "work")

		ahead, ok := branchAhead(repo, "main", "work")
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
		repo := f.repo("x")
		gitCmd(t, repo, "checkout", "-q", "-b", "work")

		ahead, ok := branchAhead(repo, "main", "work")
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
		if _, ok := branchAhead(f.repo("x"), "main", "no-such-branch"); ok {
			t.Error("ok = true, want false for a ref that does not exist")
		}
	})
}

// fetchOrigin prunes, which is what clears a remote-tracking ref after the
// branch is deleted upstream. See TestStaleRemoteBranchIsPruned for why.
func TestFetchOriginPrunes(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/gone")
	gitCmd(t, repo, "fetch", "-q", "origin")
	gitCmd(t, f.bare("x"), "update-ref", "-d", "refs/heads/gone")

	if got := branchLocation(repo, "gone"); got != "on origin" {
		t.Fatalf("fixture: branchLocation = %q, want the stale ref to be present", got)
	}

	fetchOrigin(repo, "x", &bytes.Buffer{})

	if got := branchLocation(repo, "gone"); got != "" {
		t.Errorf("branchLocation = %q after fetch, want the ref pruned", got)
	}
}

// A fetch failure is reported but never fails the repo: stale local refs beat
// no run at all.
func TestFetchOriginToleratesFailure(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	gitCmd(t, repo, "remote", "set-url", "origin", fileURL(filepath.Join(f.root, "nope.git")))

	var out bytes.Buffer
	fetchOrigin(repo, "x", &out)

	if !strings.Contains(out.String(), "Could not fetch origin for x") {
		t.Errorf("output = %q, want a fetch warning", out.String())
	}
}

func TestRestoreRepo(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	gitCmd(t, repo, "checkout", "-q", "-b", "work")

	restoreRepo(repo, "main", "work", &bytes.Buffer{})

	if got := currentBranch(t, repo); got != "main" {
		t.Errorf("current branch = %q, want main", got)
	}
	if localHasBranch(t, repo, "work") {
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
