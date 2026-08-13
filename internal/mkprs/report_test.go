package mkprs

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// testReporter is a reporter writing to w, with a fixed column width.
func testReporter(w io.Writer) *reporter {
	return &reporter{out: w, width: 8}
}

func TestNameWidthMinimum(t *testing.T) {
	t.Parallel()
	if got := nameWidth(nil); got != 1 {
		t.Errorf("nameWidth(nil) = %d, want 1", got)
	}
}

func TestResultLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo string
		res  outcome
		want string
	}{
		{
			name: "success carries the PR url",
			repo: "acme-web",
			res:  *success("https://x/1"),
			want: "✅ acme-web PR created  https://x/1\n",
		},
		{
			name: "success without a url",
			repo: "acme-web",
			res:  *success(""),
			want: "✅ acme-web PR created\n",
		},
		{
			name: "skip states the reason",
			repo: "acme-web",
			res:  *skip("command made no changes"),
			want: "⏭️  acme-web skipped: command made no changes\n",
		},
		{
			name: "failure states the reason",
			repo: "acme-web",
			res:  *failure("command exited 1"),
			want: "❌ acme-web command exited 1\n",
		},
		{
			name: "failure pads to the column width",
			repo: "web",
			res:  *failure("could not commit"),
			want: "❌ web      could not commit\n",
		},
		// The zero outcome is a failure with nothing to say. Nobody can return
		// one by accident any more, but a blank line would be worse than a
		// placeholder if one is ever written by hand.
		{
			name: "a reasonless result says so",
			repo: "web",
			res:  outcome{},
			want: "❌ web      reason unknown\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			testReporter(&buf).record(tt.repo, tt.res, nil)
			if got := buf.String(); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// A failed repo replays all of its output, indented -- but not under --verbose,
// where it has already streamed past.
func TestFailureReplaysOutput(t *testing.T) {
	t.Parallel()

	t.Run("quiet replays everything", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", false, &out)
		_, _ = c.Write([]byte("line one\nline two\n"))

		var buf bytes.Buffer
		(&reporter{out: &buf, width: 3}).record("web", *failure("boom"), c)

		want := "❌ web boom\n    line one\n    line two\n"
		if got := buf.String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("verbose does not repeat it", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", true, &out)
		_, _ = c.Write([]byte("line one\n"))

		var buf bytes.Buffer
		(&reporter{out: &buf, width: 3}).record("web", *failure("boom"), c)

		if got := buf.String(); got != "❌ web boom\n" {
			t.Errorf("got %q, want the reason only", got)
		}
	})
}

// The reporter counts what it renders, so the summary adds up without the
// caller tracking which kind it just recorded.
func TestReporterTally(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := testReporter(&buf)

	r.record("a", *success("https://x/1"), nil)
	r.record("b", *skip("command made no changes"), nil)
	r.record("c", *failure("boom"), nil)
	r.record("d", *failure("boom"), nil)

	buf.Reset()
	r.summary()

	want := "\n=== Summary ===\nSucceeded: 1\nFailed:    2\nSkipped:   1\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestNameWidthAligns(t *testing.T) {
	t.Parallel()

	repos := []string{"/x/short", "/x/a-much-longer-name", "/x/mid"}
	width := nameWidth(repos)
	if want := len("a-much-longer-name"); width != want {
		t.Fatalf("nameWidth = %d, want %d", width, want)
	}

	var buf bytes.Buffer
	r := &reporter{out: &buf, width: width}
	r.record("short", *success(""), nil)
	r.record("a-much-longer-name", *success(""), nil)

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	first := strings.Index(lines[0], "PR created")
	second := strings.Index(lines[1], "PR created")
	if first != second {
		t.Errorf("status column at %d and %d, want aligned:\n%s", first, second, buf.String())
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := &reporter{out: &buf, succeededCount: 2, failedCount: 1, skippedCount: 3}
	r.summary()

	want := "\n=== Summary ===\nSucceeded: 2\nFailed:    1\nSkipped:   3\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// The line appears only when there are repos the run never reached, and its
// label is the longest, so the whole block widens with it.
func TestSummaryCountsReposNotProcessed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := &reporter{out: &buf, succeededCount: 2, failedCount: 1, skippedCount: 3, notProcessedCount: 4}
	r.summary()

	want := "\n=== Summary ===\nSucceeded:     2\nFailed:        1\nSkipped:       3\nNot processed: 4\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
