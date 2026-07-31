package mkprs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Absent --log, nothing touches the disk.
func TestLogNothingWrittenWithoutFlag(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("x")

	run(t, &fakePR{}, []string{f.targets, "-b", "b"}, helperCmd(t, "write", "file.txt", "changed")...)

	entries, err := os.ReadDir(f.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "targets" && e.Name() != "remotes" {
			t.Errorf("unexpected entry %q in the workspace", e.Name())
		}
	}
}

func TestLogDirContents(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repo("x")
	logDir := filepath.Join(f.root, "logs")

	run(t, &fakePR{}, []string{f.targets, "-b", "b", "-m", "Do it", "--log", logDir},
		helperCmd(t, "writeprint", "file.txt", "some chatter")...)

	body := readFile(t, filepath.Join(logDir, "x.log"))

	// The header is padded to a fixed column, then a rule, then raw output.
	for _, want := range []string{
		"repo:     " + repo,
		"branch:   b",
		"status:   success",
		"note:     https://github.com/fake/x/pull/7",
		"----------------------------------------",
		"some chatter",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("log missing %q:\n%s", want, body)
		}
	}

	// commit: holds a real abbreviated SHA, read before cleanup deletes the
	// branch.
	if strings.Contains(body, "commit:   -") {
		t.Errorf("log has no commit SHA:\n%s", body)
	}
}

func TestLogSummaryTSV(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	good := f.repo("good")
	dirty := f.repo("dirty")
	writeFile(t, filepath.Join(dirty, "file.txt"), "uncommitted\n")
	logDir := filepath.Join(f.root, "logs")

	run(t, &fakePR{}, []string{f.targets, "-b", "b", "--log", logDir},
		helperCmd(t, "write", "file.txt", "changed")...)

	lines := strings.Split(strings.TrimSuffix(readFile(t, filepath.Join(logDir, "summary.tsv")), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("summary.tsv has %d lines, want header + 2 records:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	if got, want := lines[0], "repo_path\tstatus\tbranch\tcommit_sha\tpr_url\tnotes"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}

	// Records are in discovery order: dirty sorts before good.
	fields := strings.Split(lines[1], "\t")
	if len(fields) != 6 {
		t.Fatalf("record has %d fields, want 6: %q", len(fields), lines[1])
	}
	if fields[0] != dirty || fields[1] != "skipped" {
		t.Errorf("record = %q, want the dirty repo skipped", lines[1])
	}
	if fields[5] != "working tree not clean" {
		t.Errorf("notes = %q, want the skip reason", fields[5])
	}

	// On success the URL goes in pr_url and notes stays empty, rather than
	// being duplicated across both.
	fields = strings.Split(lines[2], "\t")
	if fields[0] != good || fields[1] != "success" {
		t.Errorf("record = %q, want the good repo succeeding", lines[2])
	}
	if fields[4] != "https://github.com/fake/good/pull/7" {
		t.Errorf("pr_url = %q", fields[4])
	}
	if fields[5] != "" {
		t.Errorf("notes = %q, want empty on success", fields[5])
	}
	if fields[3] == "" {
		t.Error("commit_sha is empty on success")
	}
}

// Two repos can share a basename under different target dirs. Suffix rather
// than let the second clobber the first.
func TestLogSuffixesBasenameCollisions(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	for _, parent := range []string{"a", "b"} {
		dir := filepath.Join(f.targets, parent)
		mkdir(t, dir)
		gitCmd(t, "", "init", "-q", "-b", "main", filepath.Join(dir, "dup"))
	}
	logDir := filepath.Join(f.root, "logs")

	run(t, &fakePR{}, []string{f.targets, "-b", "b", "--log", logDir},
		helperCmd(t, "write", "file.txt", "changed")...)

	for _, name := range []string{"dup.log", "dup-2.log"} {
		if _, err := os.Stat(filepath.Join(logDir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}

	// Discovery is lexical, so a/dup is written first and b/dup gets suffixed.
	if got, want := readFile(t, filepath.Join(logDir, "dup.log")), filepath.Join(f.targets, "a", "dup"); !strings.Contains(got, want) {
		t.Errorf("dup.log is not a/dup:\n%s", got)
	}
}

func TestLogRecordsSkippedRepos(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	repo := f.repoWithRemote("x", "git@gitlab.com:fake/x.git")
	logDir := filepath.Join(f.root, "logs")

	run(t, &fakePR{}, []string{f.targets, "-b", "b", "--log", logDir},
		helperCmd(t, "write", "file.txt", "changed")...)

	body := readFile(t, filepath.Join(logDir, "x.log"))
	for _, want := range []string{"repo:     " + repo, "status:   skipped", "commit:   -"} {
		if !strings.Contains(body, want) {
			t.Errorf("log missing %q:\n%s", want, body)
		}
	}
}

func TestLogRecordsFailureOutput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("x")
	logDir := filepath.Join(f.root, "logs")

	run(t, &fakePR{}, []string{f.targets, "-b", "b", "--log", logDir},
		helperCmd(t, "fail", "3", "it went wrong")...)

	body := readFile(t, filepath.Join(logDir, "x.log"))
	for _, want := range []string{"status:   failed", "note:     command exited 3", "it went wrong"} {
		if !strings.Contains(body, want) {
			t.Errorf("log missing %q:\n%s", want, body)
		}
	}
}

// --verbose composes with --log: stream and write.
func TestLogComposesWithVerbose(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("x")
	logDir := filepath.Join(f.root, "logs")

	got := run(t, &fakePR{}, []string{f.targets, "-b", "b", "-v", "--log", logDir},
		helperCmd(t, "writeprint", "file.txt", "chatter")...)

	if !strings.Contains(got.stdout, "[x] chatter") {
		t.Errorf("stdout = %q, want streamed output", got.stdout)
	}
	if body := readFile(t, filepath.Join(logDir, "x.log")); !strings.Contains(body, "chatter") {
		t.Errorf("log missing the output:\n%s", body)
	}
}

func TestLogDirCreationFailure(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.repo("x")

	// A file where the log directory should go.
	blocked := filepath.Join(f.root, "not-a-dir")
	writeFile(t, blocked, "x")

	cfg, _, err := parseArgs([]string{f.targets, "-b", "b", "--log", blocked, "--", "true"})
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	a := &app{cfg: cfg, out: &stdout, errw: &stderr, prs: &fakePR{}}
	if code := a.run(); code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "could not create log directory") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// TSV has no quoting rules, so a value must never introduce a field or a row.
func TestTSVField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"tab becomes a space", "a\tb", "a b"},
		{"newline becomes a space", "a\nb", "a b"},
		{"both", "a\tb\nc", "a b c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tsvField(tt.in); got != tt.want {
				t.Errorf("tsvField(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDashIfEmpty(t *testing.T) {
	t.Parallel()

	if got := dashIfEmpty(""); got != "-" {
		t.Errorf("dashIfEmpty(\"\") = %q, want -", got)
	}
	if got := dashIfEmpty("x"); got != "x" {
		t.Errorf("dashIfEmpty(\"x\") = %q, want x", got)
	}
}

// A nil logger is valid and does nothing.
func TestNilLoggerIsSafe(t *testing.T) {
	t.Parallel()

	lg, err := newLogger("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if lg != nil {
		t.Fatalf("newLogger(\"\") = %v, want nil", lg)
	}

	lg.record(&config{}, &repoResult{outcome: skip("x")}, newCapture("x", false, nil))
	lg.close()
}
