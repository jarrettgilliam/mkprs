package mkprs

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every other test calls Run directly, which leaves main.go and the os.Exit
// wiring uncovered. These build the real binary and run it.
//
// They deliberately stick to paths that never reach gh, so no stub is needed:
// gh is mocked at the interface, and that mock cannot reach a subprocess.

// buildBinary compiles mkprs into the test's own temp dir and returns its path.
// Each caller builds its own copy: the build cache makes a warm rebuild ~0.2s,
// which is cheaper than the process-wide setup that sharing one would need.
func buildBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping binary build in short mode")
	}

	name := "mkprs"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Safe for the parallel subtests below: a parent's cleanup runs only once
	// its parallel children have finished.
	path := filepath.Join(t.TempDir(), name)

	cmd := exec.Command("go", "build", "-o", path, ".")
	cmd.Dir = moduleRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return path
}

func TestBinary(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)

	tests := []struct {
		name     string
		args     func(t *testing.T) []string
		wantCode int
		want     string
	}{
		{
			name:     "help exits 0",
			args:     func(*testing.T) []string { return []string{"--help"} },
			wantCode: 0,
			want:     "Usage: mkprs",
		},
		{
			name:     "an unknown flag exits 1",
			args:     func(*testing.T) []string { return []string{"--bogus"} },
			wantCode: 1,
			want:     "unknown flag",
		},
		{
			name:     "a missing branch exits 1",
			args:     func(t *testing.T) []string { return []string{t.TempDir(), "--", "true"} },
			wantCode: 1,
			want:     "-b/--branch is required",
		},
		{
			name: "an empty target dir exits 0",
			args: func(t *testing.T) []string {
				return []string{t.TempDir(), "-b", "b", "--", "true"}
			},
			wantCode: 0,
			want:     "No target repositories found.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.Command(bin, tt.args(t)...)
			out, err := cmd.CombinedOutput()

			if got := exitCodeOf(err); got != tt.wantCode {
				t.Errorf("exit code = %d, want %d\n%s", got, tt.wantCode, out)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output = %q, want it to contain %q", out, tt.want)
			}
		})
	}
}

// A repo whose command changes nothing skips, so the run never reaches gh --
// which makes this a complete end-to-end exercise of the shipped binary.
func TestBinaryEndToEnd(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	f := newFixture(t)
	f.repo("x")

	args := append([]string{f.targets, "-b", "b", "--"}, helperCmd(t, "noop")...)
	out, err := exec.Command(bin, args...).CombinedOutput()

	if got := exitCodeOf(err); got != 0 {
		t.Errorf("exit code = %d, want 0\n%s", got, out)
	}
	for _, want := range []string{"⏭️  x skipped: command made no changes", "Skipped:   1"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// moduleRoot is where go build must run: the package under test lives two
// directories down from the module root.
func moduleRoot() string { return filepath.Join("..", "..") }

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
