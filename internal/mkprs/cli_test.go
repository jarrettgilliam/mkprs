package mkprs

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
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
		{"no target dirs", []string{"-b", "x", "--", "true"}, "must specify at least one target dir"},
		{"no branch", []string{"/tmp", "--", "true"}, "-b/--branch is required"},
		{"no command", []string{"/tmp", "-b", "x"}, "no command specified"},
		{"empty command after separator", []string{"/tmp", "-b", "x", "--"}, "no command specified"},
		{"unknown flag", []string{"/tmp", "--bogus"}, "unknown flag"},
		{"missing flag value", []string{"/tmp", "-b"}, "needs an argument"},
		// pflag takes whatever follows a flag as its value, so a flag left
		// empty eats the -- separator. Without the guard this reports "no
		// command specified", pointing at the wrong end of the line.
		{"separator taken as a flag value", []string{"/tmp", "-b", "--", "true"}, `-b/--branch needs an argument: "--" is the command separator`},
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

// Only `--` exactly is rejected, since only `--` can be the separator. A value
// that merely starts with it is a value: `-m` is free text, and the guard must
// not decide what a commit message may say.
func TestParseArgsKeepsValuesStartingWithDashDash(t *testing.T) {
	t.Parallel()

	cfg, _, err := parseArgs([]string{"/tmp", "-b", "x", "-m", "-- and then some", "--", "true"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if got, want := cfg.message, "-- and then some"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// One row per flag rather than per spelling. pflag does the parsing; what
// this catches is the wiring: a typo in a long name, or a short and long pair
// pointed at different fields.
var flagRows = []struct {
	long, short string
	value       string // empty for a bool
	get         func(*config) any
}{
	{"branch", "b", "my-branch", func(c *config) any { return c.branch }},
	{"message", "m", "commit msg", func(c *config) any { return c.message }},
	{"title", "t", "pr title", func(c *config) any { return c.title }},
	{"body", "B", "pr body", func(c *config) any { return c.body }},
	{"reviewer", "r", "alice,bob", func(c *config) any { return c.reviewers }},
	{"draft", "d", "", func(c *config) any { return c.draft }},
	{"keep-branch", "k", "", func(c *config) any { return c.keepBranch }},
	{"verbose", "v", "", func(c *config) any { return c.verbose }},
	{"stop-on-failure", "s", "", func(c *config) any { return c.stopOnFailure }},
	{"max-repos", "", "84", func(c *config) any { return strconv.Itoa(c.maxRepos) }},
}

// flagForms returns every spelling pflag accepts for a flag, as argv fragments.
// A bool has no space form: `-d true` would read true as a positional argument.
// A flag with no shorthand contributes only its long forms.
func flagForms(long, short, value string) [][]string {
	var forms [][]string

	// boolean only flags
	if value == "" {
		if short != "" {
			forms = append(forms, []string{"-" + short})
		}
		return append(forms, []string{"--" + long}, []string{"--" + long + "=true"})
	}

	// long form only
	if short != "" {
		forms = append(forms, []string{"-" + short, value}, []string{"-" + short + "=" + value})
	}

	return append(forms, []string{"--" + long, value}, []string{"--" + long + "=" + value})
}

func TestParseArgsFlagForms(t *testing.T) {
	t.Parallel()

	for _, tt := range flagRows {
		t.Run(tt.long, func(t *testing.T) {
			t.Parallel()

			var want any = true
			if tt.value != "" {
				want = tt.value
			}

			for _, form := range flagForms(tt.long, tt.short, tt.value) {
				t.Run(strings.Join(form, " "), func(t *testing.T) {
					t.Parallel()

					args := []string{"/tmp"}
					// -b is required, and supplying it twice for its own row
					// would just test that the last value wins.
					if tt.long != "branch" {
						args = append(args, "-b", "required")
					}
					args = append(args, form...)
					args = append(args, "--", "true")

					cfg, _, err := parseArgs(args)
					if err != nil {
						t.Fatalf("parseArgs: %v", err)
					}
					if got := tt.get(cfg); got != want {
						t.Errorf("%s = %v, want %v", tt.long, got, want)
					}
				})
			}
		})
	}
}

// A row per flag still has to be written, so a new flag can arrive untested.
// Walking the flag set parseArgs built closes that: registering a flag without a
// row breaks the suite immediately.
//
// The exemption list is where a check like this rots, so it stays at one entry
// with a reason: --help returns pflag.ErrHelp and a nil config, so it cannot be
// driven through the table at all.
func TestParseArgsFlagFormsCoversEveryFlag(t *testing.T) {
	t.Parallel()

	exempt := map[string]bool{"help": true}

	covered := map[string]bool{}
	for _, tt := range flagRows {
		covered[tt.long] = true
	}

	_, fs, err := parseArgs([]string{"/tmp", "-b", "x", "--", "true"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	fs.VisitAll(func(f *pflag.Flag) {
		if !covered[f.Name] && !exempt[f.Name] {
			t.Errorf("--%s has no row in flagRows; add one so both its forms are tested", f.Name)
		}
	})
}

// Flags may appear before or after the target dirs, and in any order.
func TestParseArgsFlagOrder(t *testing.T) {
	t.Parallel()

	want, _, err := parseArgs([]string{"/tmp", "-b", "x", "-v", "--", "true"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	tests := []struct {
		name string
		args []string
		want config
	}{
		{
			name: "flags before the target dir",
			args: []string{"-b", "x", "-v", "/tmp", "--", "true"},
		},
		{
			name: "flags on both sides of the target dir",
			args: []string{"-b", "x", "/tmp", "-v", "--", "true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if !reflect.DeepEqual(cfg, want) {
				t.Errorf("config = %+v,\n          want %+v", *cfg, *want)
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
				"-v, --verbose",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("usage missing %q\n%s", want, out)
				}
			}
		})
	}
}

// The limit ships on. The number is asserted rather than merely "non-zero".
func TestParseArgsMaxReposDefaultsOn(t *testing.T) {
	t.Parallel()

	cfg, _, err := parseArgs([]string{"/tmp", "-b", "x", "--", "true"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if got, want := cfg.maxRepos, defaultMaxRepos; got != want {
		t.Errorf("maxRepos = %d, want %d", got, want)
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

// An unusable branch name is one mistake, and the same one in every repo, so it
// is reported once before anything is walked. The target here does not exist:
// if discovery ran first it would be the error reported, so this pins the
// ordering as well as the check.
func TestRunValidatesBranchBeforeDiscovery(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"/nonexistent", "-b", "--draft", "--", "true"}, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "--draft") {
		t.Errorf("stderr = %q, want it to name the branch", stderr.String())
	}
	if strings.Contains(stderr.String(), "target directory") {
		t.Errorf("stderr = %q, want the branch checked before the target", stderr.String())
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
