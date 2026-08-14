package mkprs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// =============================================================================
// Test process: helper mode and environment isolation
// =============================================================================

// helperFlag marks a re-execution of the test binary as the command under test
// rather than as the test suite. Re-executing ourselves is the stdlib's own
// idiom (see os/exec's tests): it needs no shell, no PATH manipulation, and
// behaves identically everywhere, including on Windows.
const helperFlag = "-mkprs.helper"

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == helperFlag {
		os.Exit(runHelper(os.Args[2:]))
	}

	cleanup, err := isolateGit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "test setup: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	cleanup()
	os.Exit(code)
}

// isolateGit detaches every git invocation from the developer's own config.
// core.autocrlf and commit.gpgsign in a real ~/.gitconfig would both break
// fixtures, and the failure would look like a bug in mkprs.
//
// This is process-wide rather than per-test (os.Setenv, not t.Setenv) so that
// tests remain free to call t.Parallel.
func isolateGit() (func(), error) {
	dir, err := os.MkdirTemp("", "mkprs-gitenv")
	if err != nil {
		return nil, err
	}

	empty := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		return nil, err
	}

	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   empty,
		"GIT_CONFIG_SYSTEM":   empty,
		"GIT_AUTHOR_NAME":     "Test User",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test User",
		"GIT_COMMITTER_EMAIL": "test@example.com",
		// Pinned so commit SHAs are reproducible across runs.
		"GIT_AUTHOR_DATE":     "2024-01-01T00:00:00+00:00",
		"GIT_COMMITTER_DATE":  "2024-01-01T00:00:00+00:00",
		"GIT_TERMINAL_PROMPT": "0",
	} {
		if err := os.Setenv(k, v); err != nil {
			return nil, err
		}
	}

	return func() { _ = os.RemoveAll(dir) }, nil
}

// runHelper is the command under test, one mode per behavior a fixture needs.
func runHelper(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "helper: no mode")
		return 2
	}

	mode, rest := args[0], args[1:]
	switch mode {
	case "noop":
		return 0

	case "write", "append":
		if len(rest) < 2 {
			return 2
		}

		flags := os.O_CREATE | os.O_WRONLY
		if mode == "append" {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}

		f, err := os.OpenFile(rest[0], flags, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		defer func() { _ = f.Close() }()

		fmt.Fprintln(f, strings.Join(rest[1:], " "))
		return 0

	// A command that both changes the repo and says something, so that a
	// successful run still has output to capture.
	case "writeprint":
		if code := writeLines(rest[0], strings.Join(rest[1:], " ")); code != 0 {
			return code
		}
		fmt.Println(strings.Join(rest[1:], " "))
		return 0

	// gitcommit <file> <content> [leftover] -- a command that commits its own
	// work, optionally leaving an uncommitted file behind afterward. Its cwd is
	// already the repo, and it inherits the pinned GIT_* identity.
	case "gitcommit":
		if code := writeLines(rest[0], rest[1]); code != 0 {
			return code
		}
		for _, args := range [][]string{
			{"add", "-A"},
			{"commit", "-q", "-m", "committed by the command"},
		} {
			if code := helperGit(args...); code != 0 {
				return code
			}
		}
		if len(rest) > 2 {
			return writeLines(rest[2], "left behind")
		}
		return 0

	// gitcheckout <branch> [file content] -- switches to branch, creating it if
	// it does not exist, and optionally commits a file there.
	case "gitcheckout":
		checkout := []string{"checkout", "-q", rest[0]}
		if !helperGitOK("rev-parse", "--verify", "--quiet", "refs/heads/"+rest[0]) {
			checkout = []string{"checkout", "-q", "-b", rest[0]}
		}
		if code := helperGit(checkout...); code != 0 {
			return code
		}
		if len(rest) >= 3 {
			if code := writeLines(rest[1], rest[2]); code != 0 {
				return code
			}
			for _, args := range [][]string{
				{"add", "-A"},
				{"commit", "-q", "-m", "committed by the command"},
			} {
				if code := helperGit(args...); code != 0 {
					return code
				}
			}
		}
		return 0

	// gitdetach -- leaves HEAD off any branch.
	case "gitdetach":
		return helperGit("checkout", "-q", "--detach")

	case "rm":
		if err := os.Remove(rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0

	// env/pwd/args write to a file so the assertion can read them back from the
	// commit, rather than depending on how output is captured.
	case "env":
		return writeLines(rest[0], os.Getenv(rest[1]))

	case "pwd":
		wd, err := os.Getwd()
		if err != nil {
			return 1
		}
		return writeLines(rest[0], wd)

	case "args":
		return writeLines(rest[0], rest[1:]...)

	case "print":
		fmt.Println(strings.Join(rest, " "))
		return 0

	case "printerr":
		fmt.Fprintln(os.Stderr, strings.Join(rest, " "))
		return 0

	case "exit":
		code, err := strconv.Atoi(rest[0])
		if err != nil {
			return 2
		}
		return code

	case "fail":
		code, err := strconv.Atoi(rest[0])
		if err != nil {
			return 2
		}
		fmt.Fprintln(os.Stderr, strings.Join(rest[1:], " "))
		return code

	// kill stands in for an OOM kill: the process dies with no exit status at
	// all. os.Process.Signal is used rather than syscall.Kill because the latter
	// does not exist on Windows.
	case "kill":
		p, err := os.FindProcess(os.Getpid())
		if err != nil {
			return 2
		}
		if err := p.Signal(os.Kill); err != nil {
			return 2
		}
		select {} // unreachable once the signal lands

	// spew <lines> <exit-code> prints numbered lines before failing, so a test
	// can check that a long failure is replayed in full rather than truncated.
	case "spew":
		lines, err := strconv.Atoi(rest[0])
		if err != nil {
			return 2
		}
		code, err := strconv.Atoi(rest[1])
		if err != nil {
			return 2
		}
		for i := 1; i <= lines; i++ {
			fmt.Printf("line-%d\n", i)
		}
		return code

	default:
		fmt.Fprintf(os.Stderr, "helper: unknown mode %q\n", mode)
		return 2
	}
}

// helperGit runs git in the helper's cwd -- the repo -- returning a shell-style
// status. The helper stands in for a user's command, so it drives git itself
// rather than going through the package's own helpers.
func helperGit(args ...string) int {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func helperGitOK(args ...string) bool {
	return exec.Command("git", args...).Run() == nil
}

func writeLines(path string, lines ...string) int {
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// helperCmd builds the argv that runs a helper mode as the command under test.
func helperCmd(t *testing.T, mode string, args ...string) []string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return append([]string{exe, helperFlag, mode}, args...)
}

// =============================================================================
// Repository fixtures
// =============================================================================

// fixture is a workspace holding target repos and the bare repos that stand in
// for GitHub.
type fixture struct {
	t       *testing.T
	root    string
	targets string
	remotes string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t:       t,
		root:    root,
		targets: filepath.Join(root, "targets"),
		remotes: filepath.Join(root, "remotes"),
	}
	mkdir(t, f.targets)
	mkdir(t, f.remotes)
	return f
}

// stepRun builds a repo for the tests that drive a single pipeline step rather
// than a whole run. A nil output means the test does not read it; the PR opener
// is nil because openPR is covered through a whole run instead.
func stepRun(cfg config, repoPath string, output *capture) *repo {
	if output == nil {
		output = newCapture("x", false, io.Discard)
	}
	return newRepo(cfg, nil, repoPath, output)
}

// at wraps a fixture path so the git methods can be called on it. The fixtures
// deal in paths, because most of what they hand back is joined onto or passed
// to gitCmd rather than interrogated. Output is discarded: the few tests that
// read it build their own capture.
func at(path string) *repo { return &repo{path: path} }

// repo creates a repo whose origin looks like GitHub but resolves to a local
// bare repo, so fetch and push genuinely work offline.
func (f *fixture) repo(name string) string {
	f.t.Helper()
	return f.repoWithRemote(name, "git@github.com:fake/"+name+".git")
}

// repoWithRemote creates a repo with an arbitrary origin URL. Only github.com
// URLs get a working local remote; others exist to be skipped.
func (f *fixture) repoWithRemote(name, remote string) string {
	f.t.Helper()
	return f.repoOn(name, "main", remote)
}

// repoOn is repoWithRemote with the default branch named explicitly, for the
// repos that are not on main.
func (f *fixture) repoOn(name, defaultBranch, remote string) string {
	f.t.Helper()

	path := filepath.Join(f.targets, name)
	gitCmdHelper(f.t, "", "init", "-q", "-b", defaultBranch, path)
	writeFile(f.t, filepath.Join(path, "file.txt"), "hello\n")
	gitCmdHelper(f.t, path, "add", "file.txt")
	gitCmdHelper(f.t, path, "commit", "-q", "-m", "initial commit")

	if remote == "" {
		return path
	}
	gitCmdHelper(f.t, path, "remote", "add", "origin", remote)

	if strings.Contains(remote, "github.com") {
		bare := filepath.Join(f.remotes, name+".git")
		gitCmdHelper(f.t, "", "init", "-q", "--bare", bare)
		gitCmdHelper(f.t, path, "config", "url."+fileURL(bare)+".insteadOf", remote)
		gitCmdHelper(f.t, path, "push", "-q", "-u", "origin", defaultBranch)
		gitCmdHelper(f.t, path, "remote", "set-head", "origin", defaultBranch)
	}
	return path
}

// plainRepo creates a repo with no origin at all.
func (f *fixture) plainRepo(name string) string {
	f.t.Helper()
	return f.repoWithRemote(name, "")
}

// bare is the path of the local repo standing in for GitHub.
func (f *fixture) bare(name string) string {
	return filepath.Join(f.remotes, name+".git")
}

// remoteHasBranch reports whether the fake GitHub side has the branch.
func (f *fixture) remoteHasBranch(name, branch string) bool {
	f.t.Helper()
	cmd := exec.Command("git", "-C", f.bare(name), "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// remoteFile is a file's content on a branch of the fake GitHub side.
func (f *fixture) remoteFile(name, branch, path string) string {
	f.t.Helper()
	return gitCmdHelper(f.t, f.bare(name), "show", branch+":"+path)
}

// remoteSubject is the latest commit subject on a branch of the fake side.
func (f *fixture) remoteSubject(name, branch string) string {
	f.t.Helper()
	return gitCmdHelper(f.t, f.bare(name), "log", "-1", "--pretty=%s", branch)
}

// fileURL renders a path as a file:// URL. Bare Windows paths cannot go in a
// git config subsection name: they carry a drive letter and backslashes.
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// =============================================================================
// git and filesystem helpers
// =============================================================================

// git runs a git command, failing the test if it does not succeed. Use this for
// fixture setup, where a failure is a broken test rather than a result.
func gitCmdHelper(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func localHasBranch(t *testing.T, repo, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = repo
	return cmd.Run() == nil
}

func commitCount(t *testing.T, repo, rev string) int {
	t.Helper()
	n, err := strconv.Atoi(gitCmdHelper(t, repo, "rev-list", "--count", rev))
	if err != nil {
		t.Fatalf("rev-list --count %s: %v", rev, err)
	}
	return n
}

func currentBranch(t *testing.T, repo string) string {
	t.Helper()
	return gitCmdHelper(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readFile is the working-tree counterpart to fixture.remoteFile, for the tests
// that stop before anything is committed. It does not trim: a helper mode that
// writes a trailing newline and one that does not are different results.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// =============================================================================
// Running mkprs in-process
// =============================================================================

// fakePR is a prOpener test double. It records what it was asked to open, so
// tests assert on the pull request itself rather than on a fake gh's log.
type fakePR struct {
	mu    sync.Mutex
	calls []fakePRCall
	err   error
}

type fakePRCall struct {
	repoPath string
	pr       pullRequest
}

func (f *fakePR) open(repoPath string, pr pullRequest, log io.Writer) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakePRCall{repoPath: repoPath, pr: pr})
	f.mu.Unlock()

	if f.err != nil {
		fmt.Fprintln(log, "pull request failed")
		return "", f.err
	}

	// Like ghCLI, the URL is returned rather than written to log: it belongs on
	// the result line, and echoing it here would print it twice.
	return "https://github.com/fake/" + filepath.Base(repoPath) + "/pull/7", nil
}

func (f *fakePR) only(t *testing.T) fakePRCall {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("prOpener called %d times, want 1", len(f.calls))
	}
	return f.calls[0]
}

// result is one in-process run of mkprs.
type result struct {
	code   int
	stdout string
	stderr string
}

func (r result) all() string { return r.stdout + r.stderr }

// run runs mkprs in-process with a stubbed PR opener. args are everything
// before the -- separator; command is everything after it.
func run(t *testing.T, prs prOpener, args []string, command ...string) result {
	t.Helper()

	full := append(append([]string{}, args...), "--")
	full = append(full, command...)

	cfg, _, err := parseArgs(full)
	if err != nil {
		t.Fatalf("parseArgs(%q): %v", args, err)
	}

	var stdout, stderr bytes.Buffer
	a := &app{cfg: cfg, out: &stdout, errOut: &stderr, prs: prs}
	code := a.run()
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}
