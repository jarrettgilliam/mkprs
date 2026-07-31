package mkprs

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestParseArgsUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no target dirs", []string{"-b", "x", "--", "true"}, "Must specify at least one target dir"},
		{"no branch", []string{"/tmp", "--", "true"}, "-b/--branch is required"},
		{"no command", []string{"/tmp", "-b", "x"}, "no command specified"},
		{"empty command after separator", []string{"/tmp", "-b", "x", "--"}, "no command specified"},
		{"unknown flag", []string{"/tmp", "--bogus"}, "unknown flag"},
		{"missing flag value", []string{"/tmp", "-b"}, "needs an argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, err := parseArgs(tt.args)
			if cfg != nil {
				t.Errorf("config = %+v, want nil", cfg)
			}

			if err == nil {
				t.Fatal("error = nil, want a usage error")
			}
			// A usage error must be distinguishable from a help request, which
			// the caller answers on stdout with exit 0.
			if errors.Is(err, pflag.ErrHelp) {
				t.Fatalf("error = %v, want a usage error rather than ErrHelp", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

// The four flag-spelling tests in the shell suite each built a repo and ran a
// whole PR flow to prove a string was parsed. Here they are one table.
func TestParseArgsFlagForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want config
	}{
		{
			name: "short space form",
			args: []string{"/tmp", "-b", "my-branch", "--", "true"},
			want: config{branch: "my-branch"},
		},
		{
			name: "short equals form",
			args: []string{"/tmp", "-b=my-branch", "--", "true"},
			want: config{branch: "my-branch"},
		},
		{
			name: "long equals form",
			args: []string{"/tmp", "--branch=my-branch", "--log=/logs", "--", "true"},
			want: config{branch: "my-branch", logDir: "/logs"},
		},
		{
			name: "verbose short form",
			args: []string{"/tmp", "-b", "x", "-v", "--", "true"},
			want: config{branch: "x", verbose: true},
		},
		{
			name: "flags after the target dir",
			args: []string{"/tmp", "-b", "x", "-m", "msg", "-t", "title", "-B", "body", "-r", "alice", "--", "true"},
			want: config{branch: "x", message: "msg", title: "title", body: "body", reviewer: "alice"},
		},
		{
			name: "flags before the target dir",
			args: []string{"-b", "x", "-v", "/tmp", "--", "true"},
			want: config{branch: "x", verbose: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}

			if cfg.branch != tt.want.branch {
				t.Errorf("branch = %q, want %q", cfg.branch, tt.want.branch)
			}
			if cfg.message != tt.want.message {
				t.Errorf("message = %q, want %q", cfg.message, tt.want.message)
			}
			if cfg.title != tt.want.title {
				t.Errorf("title = %q, want %q", cfg.title, tt.want.title)
			}
			if cfg.body != tt.want.body {
				t.Errorf("body = %q, want %q", cfg.body, tt.want.body)
			}
			if cfg.reviewer != tt.want.reviewer {
				t.Errorf("reviewer = %q, want %q", cfg.reviewer, tt.want.reviewer)
			}
			if cfg.logDir != tt.want.logDir {
				t.Errorf("logDir = %q, want %q", cfg.logDir, tt.want.logDir)
			}
			if cfg.verbose != tt.want.verbose {
				t.Errorf("verbose = %v, want %v", cfg.verbose, tt.want.verbose)
			}
		})
	}
}

// Everything before -- is a target dir, everything after is the command --
// including arguments that look like flags.
func TestParseArgsSeparator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantDirs    []string
		wantCommand []string
	}{
		{
			name:        "single dir",
			args:        []string{"/a", "-b", "x", "--", "echo", "hi"},
			wantDirs:    []string{"/a"},
			wantCommand: []string{"echo", "hi"},
		},
		{
			name:        "multiple dirs",
			args:        []string{"/a", "/b", "/c", "-b", "x", "--", "true"},
			wantDirs:    []string{"/a", "/b", "/c"},
			wantCommand: []string{"true"},
		},
		{
			name:        "option-like args belong to the command",
			args:        []string{"/a", "-b", "x", "--", "sed", "-i", "-e", "s/a/b/", "--verbose"},
			wantDirs:    []string{"/a"},
			wantCommand: []string{"sed", "-i", "-e", "s/a/b/", "--verbose"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			assertEqualSlice(t, "targetDirs", cfg.targetDirs, tt.wantDirs)
			assertEqualSlice(t, "command", cfg.command, tt.wantCommand)
		})
	}
}

func TestParseArgsHelp(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()

			_, fs, err := parseArgs([]string{arg})

			if !errors.Is(err, pflag.ErrHelp) {
				t.Fatalf("error = %v (%T), want pflag.ErrHelp", err, err)
			}

			var buf bytes.Buffer
			printUsage(&buf, fs)
			out := buf.String()

			for _, want := range []string{
				"Usage: mkprs",
				"Everything after -- is the command",
				"-b, --branch",
				"--log dir",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("usage missing %q\n%s", want, out)
				}
			}
		})
	}
}

// Run owns the exit codes, so they are asserted here rather than through main.
func TestRunExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string // on stdout
		wantErr  string // on stderr
	}{
		{"help", []string{"--help"}, exitOK, "Usage: mkprs", ""},
		{"unknown flag", []string{"--bogus"}, exitUsage, "", "unknown flag"},
		{"missing branch", []string{"/tmp", "--", "true"}, exitUsage, "", "-b/--branch is required"},
		{"no repos found", []string{t.TempDir(), "-b", "x", "--", "true"}, exitOK, "", "No target repositories found."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d\nstderr: %s", code, tt.wantCode, stderr.String())
			}
			if tt.wantOut != "" && !strings.Contains(stdout.String(), tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantOut)
			}
			if tt.wantErr != "" && !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

// A usage error prints the message and the usage block, and prints nothing at
// all to stdout -- so that piping stdout somewhere useful stays clean.
func TestRunUsageErrorGoesToStderr(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	Run([]string{"--bogus"}, &stdout, &stderr)

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage: mkprs") {
		t.Errorf("stderr missing usage block:\n%s", stderr.String())
	}
}

func assertEqualSlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}
