package mkprs

import (
	"path/filepath"
	"strings"
	"testing"
)

// mustDiscover is discovery for the cases where the target is fine, so the rows
// that care about repos are not each three lines of error handling.
func mustDiscover(t *testing.T, targetDir string, repos []string) []string {
	t.Helper()
	got, err := discoverRepos(targetDir, repos)
	if err != nil {
		t.Fatalf("discoverRepos(%q): %v", targetDir, err)
	}
	return got
}

func TestDiscoverRepos(t *testing.T) {
	t.Parallel()

	t.Run("finds every repo under a target dir", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		a := f.repo("alpha")
		b := f.repo("beta")

		assertEqualSlice(t, "repos", mustDiscover(t, f.targets, nil), []string{a, b})
	})

	// A repo found *inside* another repo is a stray checkout far more often than
	// something anyone wants a pull request against -- git makes the arrangement
	// awkward on purpose, and submodules exist for the deliberate case. So the
	// whole repo subtree is pruned, not just its .git.
	t.Run("prunes repos nested inside repos", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		outer := f.repo("outer")
		inner := filepath.Join(outer, "vendor", "inner")
		gitCmdHelper(t, "", "init", "-q", "-b", "main", inner)

		assertEqualSlice(t, "repos", mustDiscover(t, f.targets, nil), []string{outer})
	})

	// The other half of pruning, and the half that is easy to lose while
	// implementing the first: a nested repo named directly is still processed.
	// It is found as the walk's own root, so nothing has pruned it and the
	// inside-a-repo check never runs for it.
	t.Run("a nested repo named directly is still found", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		outer := f.repo("outer")
		inner := filepath.Join(outer, "vendor", "inner")
		gitCmdHelper(t, "", "init", "-q", "-b", "main", inner)

		repos := mustDiscover(t, outer, nil)
		repos = mustDiscover(t, inner, repos)
		assertEqualSlice(t, "repos", repos, []string{outer, inner})
	})

	// A submodule's .git, and a linked worktree's, are each a *file* holding a
	// gitdir: pointer, so neither is a repository for our purposes. The fixture
	// sits beside the repos rather than inside one, since inside one it would be
	// pruned before this check could be reached and the test would pass whatever
	// the check did.
	t.Run("ignores a .git file", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("alpha")
		writeFile(t, filepath.Join(f.targets, "worktree", ".git"), "gitdir: /elsewhere/.git/worktrees/wt\n")

		assertEqualSlice(t, "repos", mustDiscover(t, f.targets, nil), []string{repo})
	})

	t.Run("appends across multiple target dirs", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		a := f.repo("alpha")
		b := f.repo("beta")

		repos := mustDiscover(t, a, nil)
		repos = mustDiscover(t, b, repos)
		assertEqualSlice(t, "repos", repos, []string{a, b})
	})

	t.Run("returns repos in lexical order", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		for _, name := range []string{"zulu", "alpha", "mike"} {
			f.repo(name)
		}

		assertEqualSlice(t, "repos", mustDiscover(t, f.targets, nil), []string{
			filepath.Join(f.targets, "alpha"),
			filepath.Join(f.targets, "mike"),
			filepath.Join(f.targets, "zulu"),
		})
	})

	// `mkprs ~/repos/*` sweeps up whatever is sitting alongside the repos --
	// a notes/ folder, a README.md -- and neither is a mistake worth stopping
	// for. The line is whether the argument made sense, not whether it found
	// work, and a glob produces both of these routinely.
	t.Run("targets holding no repos are not errors", func(t *testing.T) {
		t.Parallel()

		tests := map[string]func(t *testing.T, f *fixture) string{
			"a directory with no repos": func(t *testing.T, f *fixture) string {
				dir := filepath.Join(f.root, "notes")
				mkdir(t, dir)
				return dir
			},
			"a file": func(t *testing.T, f *fixture) string {
				file := filepath.Join(f.root, "README.md")
				writeFile(t, file, "x")
				return file
			},
		}

		for name, setup := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				f := newFixture(t)
				if got := mustDiscover(t, setup(t, f), nil); len(got) != 0 {
					t.Errorf("repos = %q, want none", got)
				}
			})
		}
	})

	// A file is ignored, not walked: naming one must not quietly process every
	// repo in the directory that happens to contain it.
	t.Run("a file does not drag in its directory", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		f.repo("alpha")
		readme := filepath.Join(f.targets, "README.md")
		writeFile(t, readme, "x")

		if got := mustDiscover(t, readme, nil); len(got) != 0 {
			t.Errorf("repos = %q, want none", got)
		}
	})
}

// A target mkprs cannot interpret stops the run before any repo is touched
func TestDiscoverReposRejectsBadTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target func(t *testing.T, f *fixture) string
		want   string
	}{
		{
			name: "a target that does not exist",
			target: func(t *testing.T, f *fixture) string {
				return filepath.Join(f.root, "nope")
			},
			want: "target directory does not exist",
		},
		{
			// A file *inside* a repo is still worth stopping for: the useful
			// answer is the repo root, not "nothing found".
			name: "a file inside a repo",
			target: func(t *testing.T, f *fixture) string {
				return filepath.Join(f.repo("app"), "file.txt")
			},
			want: "mkprs runs commands at the repo root",
		},
		{
			// Running at the repo root instead would be a surprise with a
			// commit attached: someone who typed app/src may have meant
			// something to happen in src.
			name: "a target inside a repo",
			target: func(t *testing.T, f *fixture) string {
				src := filepath.Join(f.repo("app"), "src")
				mkdir(t, src)
				return src
			},
			want: "mkprs runs commands at the repo root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			target := tt.target(t, f)

			got, err := discoverRepos(target, nil)
			if err == nil {
				t.Fatalf("discoverRepos(%q) succeeded, want %q", target, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), target) {
				t.Errorf("error = %q, want it to name the target %q", err, target)
			}
			if len(got) != 0 {
				t.Errorf("repos = %q, want none alongside an error", got)
			}
		})
	}

	// The repo root itself is found by the walk, so the inside-a-repo check
	// never runs for it.
	t.Run("a repo root is not inside a repo", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("app")

		assertEqualSlice(t, "repos", mustDiscover(t, repo, nil), []string{repo})
	})
}

// Overlapping targets are an argument mistake with unambiguous intent -- nobody
// means "process this repo twice" -- so this deduplicates rather than refusing.
func TestDedupeRepos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repos       []string
		want        []string
		wantDropped []string
	}{
		{
			name:  "nothing to do",
			repos: []string{"/a", "/b"},
			want:  []string{"/a", "/b"},
		},
		{
			name:        "a repo reached from two targets",
			repos:       []string{"/a", "/b", "/a"},
			want:        []string{"/a", "/b"},
			wantDropped: []string{"/a"},
		},
		{
			// First occurrence wins, so order is still lexical within each
			// target dir in the order the targets were given. Sorting the
			// combined list would quietly change that.
			name:        "order follows first occurrence",
			repos:       []string{"/b", "/a", "/b", "/a"},
			want:        []string{"/b", "/a"},
			wantDropped: []string{"/b", "/a"},
		},
		{
			// Three occurrences drop two. The repo is named once per copy
			// discarded, so the list and the count cannot disagree.
			name:        "the same repo three times",
			repos:       []string{"/a", "/a", "/a"},
			want:        []string{"/a"},
			wantDropped: []string{"/a", "/a"},
		},
		{
			name:  "nothing at all",
			repos: nil,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, dropped := dedupeRepos(tt.repos)
			assertEqualSlice(t, "repos", got, tt.want)
			assertEqualSlice(t, "dropped", dropped, tt.wantDropped)
		})
	}
}
