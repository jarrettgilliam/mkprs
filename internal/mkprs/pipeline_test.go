package mkprs

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

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

	t.Run("a repo that passes", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		// Somewhere other than the default branch, so startBranch cannot pass
		// by accidentally agreeing with defaultBranch.
		gitCmd(t, repo, "checkout", "-q", "-b", "feature")

		p, res := stepRun(&app{cfg: &config{branch: "b"}}, repo, nil).preflight()
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

	// Every way a repo can stop here, in one table so the set stays legible.
	// Only the two determinations that nothing is wanted here are skips; every
	// other row is a repo the run has to happen again for. How an outcome is
	// then reported does not vary by reason, which is what TestRunSkips covers
	// with a single row.
	stops := []struct {
		name  string
		setup func(t *testing.T, f *fixture) string // returns the repo path
		want  string
		fails bool // a failure rather than a skip
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
			want:  "working tree not clean",
			fails: true,
		},
		{
			// The other half of the split: git could not answer the question at
			// all, which is not the same fact about the repo.
			name: "unreadable working tree status",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				writeFile(t, filepath.Join(repo, ".git", "index"), "not an index\n")
				return repo
			},
			want:  "could not read the working tree status",
			fails: true,
		},
		{
			name: "could not fetch origin",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				if err := os.RemoveAll(f.bare("x")); err != nil {
					t.Fatalf("remove the remote: %v", err)
				}
				return repo
			},
			want:  "could not fetch origin",
			fails: true,
		},
		{
			name: "no discoverable default branch",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repoOn("x", "trunk", "git@github.com:fake/x.git")
				gitCmd(t, f.bare("x"), "update-ref", "-d", "refs/heads/trunk")
				// Pruning refs/remotes/origin/trunk leaves origin/HEAD pointing
				// at it, and symbolic-ref resolves a dangling symref happily --
				// so without this the default branch is still "trunk".
				// --no-deref, or this deletes origin/trunk instead.
				gitCmd(t, repo, "update-ref", "--no-deref", "-d", "refs/remotes/origin/HEAD")
				return repo
			},
			want:  "no default branch on origin; set it with 'git remote set-head origin -a'",
			fails: true,
		},
		{
			name: "detached HEAD",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "checkout", "-q", "--detach")
				return repo
			},
			want:  "not on a branch (detached HEAD)",
			fails: true,
		},
		{
			name: "branch already exists locally",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "branch", "b")
				return repo
			},
			want:  "branch 'b' already exists locally",
			fails: true,
		},
		{
			name: "branch already exists on origin",
			setup: func(t *testing.T, f *fixture) string {
				repo := f.repo("x")
				gitCmd(t, repo, "push", "-q", "origin", "HEAD:refs/heads/b")
				gitCmd(t, repo, "fetch", "-q", "origin")
				return repo
			},
			want:  "branch 'b' already exists on origin",
			fails: true,
		},
	}

	for _, tt := range stops {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			repo := tt.setup(t, f)

			p, res := stepRun(&app{cfg: &config{branch: "b"}}, repo, nil).preflight()
			if res == nil {
				t.Fatalf("preflight carried on, want %q", tt.want)
			}

			var reason string
			var fails bool
			switch o := res.(type) {
			case outcomeSkipped:
				reason = o.reason
			case outcomeFailed:
				reason, fails = o.reason, true
			default:
				t.Fatalf("outcome = %T, want a skip or a failure", res)
			}

			if fails != tt.fails {
				t.Errorf("failure = %v, want %v", fails, tt.fails)
			}
			if reason != tt.want {
				t.Errorf("reason = %q, want %q", reason, tt.want)
			}
			// Nothing downstream may read the data return once the outcome is
			// set, so it must not be half-filled.
			if (p != prep{}) {
				t.Errorf("prep = %#v, want the zero value alongside an outcome", p)
			}
		})
	}
}

// cleanup's rule in one table: restore unless the repo failed or -k was passed.
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
			res:         failure("command exited 1", nil),
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
			res:         failure("command exited 1", nil),
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
			stepRun(a, repo, nil).cleanup(tt.res, "feature")

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

			if err := stepRun(a, repo, nil).runCommand(); err != nil {
				t.Fatalf("runCommand: %v", err)
			}

			if got, want := readFile(t, filepath.Join(repo, "out.txt")), tt.want(repo); got != want {
				t.Errorf("out.txt = %q, want %q", got, want)
			}
		})
	}
}

// These are the failure shapes, which never write a file at all. What the
// command can see is the table above.
func TestRunCommand(t *testing.T) {
	t.Parallel()

	t.Run("a command that succeeds", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)
		repo := f.repo("x")
		c := newCapture("x", false, io.Discard)
		a := &app{cfg: &config{command: helperCmd(t, "writeprint", "out.txt", "hello")}}

		if err := stepRun(a, repo, c).runCommand(); err != nil {
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

		err := stepRun(a, f.repo("x"), c).runCommand()
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

		err := stepRun(a, f.repo("x"), nil).runCommand()
		if err == nil {
			t.Fatal("runCommand succeeded, want an error")
		}
		if got := err.Error(); !strings.HasPrefix(got, "could not run command: ") {
			t.Errorf("error = %q, want it to report the exec failure", got)
		}
		if got := err.Error(); !strings.Contains(got, "mkprs-no-such-binary") {
			t.Errorf("error = %q, want it to name the binary", got)
		}
	})

	// ExitCode is -1 for a process a signal took down, and "command exited -1"
	// is not a status any shell would report.
	t.Run("a command killed by a signal", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("windows has no signals; TerminateProcess yields a real exit status")
		}

		f := newFixture(t)
		a := &app{cfg: &config{command: helperCmd(t, "kill")}}

		err := stepRun(a, f.repo("x"), nil).runCommand()
		if err == nil {
			t.Fatal("runCommand succeeded, want an error")
		}
		if got, want := err.Error(), "command was killed (signal: killed)"; got != want {
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
			got := stepRun(a, repo, nil).commitAndPush(p)

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
