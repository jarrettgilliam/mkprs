package mkprs

import (
	"strings"
	"testing"
)

func TestGhArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pr   pullRequest
		want []string
	}{
		{
			name: "without a reviewer",
			pr:   pullRequest{Base: "main", Head: "fix", Title: "Fix it", Body: "why"},
			want: []string{
				"pr", "create",
				"--base", "main",
				"--head", "fix",
				"--title", "Fix it",
				"--body", "why",
			},
		},
		{
			name: "with a reviewer",
			pr:   pullRequest{Base: "main", Head: "fix", Title: "Fix it", Body: "why", Reviewers: "alice"},
			want: []string{
				"pr", "create",
				"--base", "main",
				"--head", "fix",
				"--title", "Fix it",
				"--body", "why",
				"--reviewer", "alice",
			},
		},
		{
			// gh takes the list itself, so mkprs hands the string over whole
			// rather than splitting and repeating the flag.
			name: "several reviewers pass through as one value",
			pr:   pullRequest{Base: "main", Head: "fix", Title: "Fix it", Reviewers: "alice,bob,myorg/team"},
			want: []string{
				"pr", "create",
				"--base", "main",
				"--head", "fix",
				"--title", "Fix it",
				"--body", "",
				"--reviewer", "alice,bob,myorg/team",
			},
		},
		{
			// --draft is a bare flag; gh takes no value for it.
			name: "as a draft",
			pr:   pullRequest{Base: "main", Head: "fix", Title: "Fix it", Body: "why", Draft: true},
			want: []string{
				"pr", "create",
				"--base", "main",
				"--head", "fix",
				"--title", "Fix it",
				"--body", "why",
				"--draft",
			},
		},
		{
			name: "empty body is still passed",
			pr:   pullRequest{Base: "main", Head: "fix", Title: "Fix it"},
			want: []string{
				"pr", "create",
				"--base", "main",
				"--head", "fix",
				"--title", "Fix it",
				"--body", "",
			},
		},
		{
			name: "a non-default base is honored",
			pr:   pullRequest{Base: "develop", Head: "fix", Title: "Fix it"},
			want: []string{
				"pr", "create",
				"--base", "develop",
				"--head", "fix",
				"--title", "Fix it",
				"--body", "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertEqualSlice(t, "ghArgs", ghArgs(tt.pr), tt.want)
		})
	}
}

func TestLastLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single line", "https://example/1", "https://example/1"},
		{"trailing newline", "https://example/1\n", "https://example/1"},
		{"noise before the url", "Warning: x\nhttps://example/1\n", "https://example/1"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := lastLine(tt.in); got != tt.want {
				t.Errorf("lastLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The gh implementation still has to satisfy the interface the rest of the code
// depends on.
func TestGhCLIImplementsPROpener(t *testing.T) {
	t.Parallel()
	var _ prOpener = ghCLI{}
}

func TestGhCLIReportsMissingBinary(t *testing.T) {
	// No t.Parallel: t.Setenv forbids it. This is the only test that touches
	// process-wide state, which is the whole point of mocking gh elsewhere.
	t.Setenv("PATH", "")

	_, err := ghCLI{}.open(t.TempDir(), pullRequest{}, &strings.Builder{})
	if err == nil {
		t.Fatal("open with no gh on PATH: want an error")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %q, want it to mention gh is not installed", err)
	}
}
