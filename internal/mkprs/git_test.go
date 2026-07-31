package mkprs

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestShortSHA(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")

	if got := shortSHA(repo, "HEAD"); len(got) < 7 {
		t.Errorf("shortSHA = %q, want an abbreviated hash", got)
	}
	if got := shortSHA(repo, "no-such-rev"); got != "" {
		t.Errorf("shortSHA = %q for a bad rev, want empty", got)
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
