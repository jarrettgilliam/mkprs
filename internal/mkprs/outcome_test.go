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

// The shell suite spun up real git repos to check these strings. They are pure
// formatting, so here they are pure tests.
func TestResultLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report func(w *bytes.Buffer)
		want   string
	}{
		{
			name:   "success carries the PR url",
			report: func(w *bytes.Buffer) { success("https://x/1").report(testReporter(w), "acme-web") },
			want:   "✅ acme-web PR created  https://x/1\n",
		},
		{
			// gh can exit 0 without a parseable URL on stdout. The PR still
			// exists, so this reports success and simply omits the link.
			name:   "success without a url",
			report: func(w *bytes.Buffer) { success("").report(testReporter(w), "acme-web") },
			want:   "✅ acme-web PR created\n",
		},
		{
			name:   "skip states the reason",
			report: func(w *bytes.Buffer) { skip("working tree not clean").report(testReporter(w), "acme-web") },
			want:   "⏭️  acme-web skipped: working tree not clean\n",
		},
		{
			name: "failure states the reason",
			report: func(w *bytes.Buffer) {
				fail("command exited 1", newCapture("acme-web", false, w)).report(testReporter(w), "acme-web")
			},
			want: "❌ acme-web command exited 1\n",
		},
		{
			name: "failure pads to the column width",
			report: func(w *bytes.Buffer) {
				r := &reporter{out: w, width: 8}
				fail("could not commit", newCapture("web", false, w)).report(r, "web")
			},
			want: "❌ web      could not commit\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			tt.report(&buf)
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
		fail("boom", c).report(&reporter{out: &buf, width: 3}, "web")

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
		fail("boom", c).report(&reporter{out: &buf, width: 3}, "web")

		if got := buf.String(); got != "❌ web boom\n" {
			t.Errorf("got %q, want the reason only", got)
		}
	})
}

// Each outcome counts itself, so the summary adds up without the caller
// tracking which kind it just reported.
func TestReporterTally(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := testReporter(&buf)

	success("https://x/1").report(r, "a")
	skip("working tree not clean").report(r, "b")
	fail("boom", newCapture("c", false, &buf)).report(r, "c")
	fail("boom", newCapture("d", false, &buf)).report(r, "d")

	buf.Reset()
	r.summary()

	want := "\n=== Summary ===\nSucceeded: 1\nFailed:    2\nSkipped:   1\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Result lines pad to the longest repo name so the status column lines up.
func TestNameWidthAligns(t *testing.T) {
	t.Parallel()

	repos := []string{"/x/short", "/x/a-much-longer-name", "/x/mid"}
	width := nameWidth(repos)
	if want := len("a-much-longer-name"); width != want {
		t.Fatalf("nameWidth = %d, want %d", width, want)
	}

	var buf bytes.Buffer
	r := &reporter{out: &buf, width: width}
	success("").report(r, "short")
	success("").report(r, "a-much-longer-name")

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	first := strings.Index(lines[0], "PR created")
	second := strings.Index(lines[1], "PR created")
	if first != second {
		t.Errorf("status column at %d and %d, want aligned:\n%s", first, second, buf.String())
	}
}
