package mkprs

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverRepos(t *testing.T) {
	t.Parallel()

	t.Run("finds every repo under a target dir", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		a := f.repo("alpha")
		b := f.repo("beta")

		got := discoverRepos(f.targets, nil, &bytes.Buffer{})
		assertEqualSlice(t, "repos", got, []string{a, b})
	})

	// Pruning stops descent into .git itself, not into the rest of the tree, so
	// a repo inside another repo is found and both are processed. This is
	// load-bearing: it is the argument for a future --max-repos.
	t.Run("finds repos nested inside repos", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		outer := f.repo("outer")
		inner := filepath.Join(outer, "vendor", "inner")
		gitCmd(t, "", "init", "-q", "-b", "main", inner)

		got := discoverRepos(f.targets, nil, &bytes.Buffer{})
		assertEqualSlice(t, "repos", got, []string{outer, inner})
	})

	// A submodule's .git is a *file* holding a gitdir: pointer, so it is not a
	// repository for our purposes. Dropping the IsDir check would silently
	// start walking into submodules.
	t.Run("ignores a .git file", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		outer := f.repo("outer")
		writeFile(t, filepath.Join(outer, "sub", ".git"), "gitdir: ../.git/modules/sub\n")

		got := discoverRepos(f.targets, nil, &bytes.Buffer{})
		assertEqualSlice(t, "repos", got, []string{outer})
	})

	t.Run("appends across multiple target dirs", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		a := f.repo("alpha")
		b := f.repo("beta")

		var repos []string
		repos = discoverRepos(a, repos, &bytes.Buffer{})
		repos = discoverRepos(b, repos, &bytes.Buffer{})
		assertEqualSlice(t, "repos", repos, []string{a, b})
	})

	t.Run("warns about a missing target dir", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		missing := filepath.Join(f.root, "nope")

		var warn bytes.Buffer
		got := discoverRepos(missing, nil, &warn)

		if len(got) != 0 {
			t.Errorf("repos = %q, want none", got)
		}
		if !strings.Contains(warn.String(), "Target directory does not exist: "+missing) {
			t.Errorf("warning = %q", warn.String())
		}
	})

	t.Run("warns when the target is a file", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		file := filepath.Join(f.root, "a-file")
		writeFile(t, file, "x")

		var warn bytes.Buffer
		if got := discoverRepos(file, nil, &warn); len(got) != 0 {
			t.Errorf("repos = %q, want none", got)
		}
		if !strings.Contains(warn.String(), "does not exist") {
			t.Errorf("warning = %q", warn.String())
		}
	})

	// Order is lexical rather than filesystem-dependent, which is what makes
	// log-file collision suffixing deterministic.
	t.Run("returns repos in lexical order", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		for _, name := range []string{"zulu", "alpha", "mike"} {
			f.repo(name)
		}

		got := discoverRepos(f.targets, nil, &bytes.Buffer{})
		assertEqualSlice(t, "repos", got, []string{
			filepath.Join(f.targets, "alpha"),
			filepath.Join(f.targets, "mike"),
			filepath.Join(f.targets, "zulu"),
		})
	})
}
