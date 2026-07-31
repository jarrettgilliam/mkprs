package mkprs

import (
	"bytes"
	"strings"
	"testing"
)

// The shell suite spun up real git repos to check these strings. They are pure
// formatting, so here they are pure tests.
func TestResultLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		write func(w *bytes.Buffer)
		want  string
	}{
		{
			name:  "success carries the PR url",
			write: func(w *bytes.Buffer) { resultOK(w, 8, "acme-web", "https://x/1") },
			want:  "✅ acme-web PR created  https://x/1\n",
		},
		{
			// gh can exit 0 without a parseable URL on stdout. The PR still
			// exists, so this reports success and simply omits the link.
			name:  "success without a url",
			write: func(w *bytes.Buffer) { resultOK(w, 8, "acme-web", "") },
			want:  "✅ acme-web PR created\n",
		},
		{
			name:  "skip states the reason",
			write: func(w *bytes.Buffer) { resultSkip(w, 8, "acme-web", "working tree not clean") },
			want:  "⏭️  acme-web skipped: working tree not clean\n",
		},
		{
			name: "failure states the reason",
			write: func(w *bytes.Buffer) {
				resultFail(w, 8, "acme-web", "command exited 1", newCapture("acme-web", false, w))
			},
			want: "❌ acme-web command exited 1\n",
		},
		{
			name: "failure pads to the column width",
			write: func(w *bytes.Buffer) {
				resultFail(w, 8, "web", "could not commit", newCapture("web", false, w))
			},
			want: "❌ web      could not commit\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			tt.write(&buf)
			if got := buf.String(); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
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
	resultOK(&buf, width, "short", "")
	resultOK(&buf, width, "a-much-longer-name", "")

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	first := strings.Index(lines[0], "PR created")
	second := strings.Index(lines[1], "PR created")
	if first != second {
		t.Errorf("status column at %d and %d, want aligned:\n%s", first, second, buf.String())
	}
}

func TestNameWidthMinimum(t *testing.T) {
	t.Parallel()
	if got := nameWidth(nil); got != 1 {
		t.Errorf("nameWidth(nil) = %d, want 1", got)
	}
}

// A failed repo replays all of its output, indented -- but not under --verbose,
// where it has already streamed past.
func TestResultFailReplaysOutput(t *testing.T) {
	t.Parallel()

	t.Run("quiet replays everything", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", false, &out)
		c.Write([]byte("line one\nline two\n"))

		var buf bytes.Buffer
		resultFail(&buf, 3, "web", "boom", c)

		want := "❌ web boom\n    line one\n    line two\n"
		if got := buf.String(); got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("verbose does not repeat it", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", true, &out)
		c.Write([]byte("line one\n"))

		var buf bytes.Buffer
		resultFail(&buf, 3, "web", "boom", c)

		if got := buf.String(); got != "❌ web boom\n" {
			t.Errorf("got %q, want the reason only", got)
		}
	})
}

func TestCaptureIndented(t *testing.T) {
	t.Parallel()

	// Nothing is truncated: with --log gone this replay is the only place a
	// failure's output can be read.
	t.Run("keeps every line", func(t *testing.T) {
		t.Parallel()

		const lines = 500
		c := newCapture("web", false, &bytes.Buffer{})
		for i := 0; i < lines; i++ {
			c.Write([]byte("line\n"))
		}
		if got := strings.Count(c.indented(), "\n"); got != lines {
			t.Errorf("indented has %d lines, want %d", got, lines)
		}
	})

	t.Run("empty capture yields nothing", func(t *testing.T) {
		t.Parallel()

		c := newCapture("web", false, &bytes.Buffer{})
		if !c.empty() {
			t.Error("empty() = false on a fresh capture")
		}
		if got := c.indented(); got != "" {
			t.Errorf("indented = %q, want empty", got)
		}
	})
}

// Under --verbose output streams live, prefixed with the repo it came from.
func TestCaptureStreaming(t *testing.T) {
	t.Parallel()

	t.Run("prefixes each line", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", true, &out)
		c.Write([]byte("one\ntwo\n"))

		if got, want := out.String(), "[web] one\n[web] two\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("buffers a partial line until its newline", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", true, &out)
		c.Write([]byte("par"))
		if out.Len() != 0 {
			t.Errorf("streamed %q before the newline", out.String())
		}
		c.Write([]byte("tial\n"))
		if got, want := out.String(), "[web] partial\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// A command whose last line has no newline must still be shown; a naive
	// scanner loop drops it.
	t.Run("flush emits a trailing fragment", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", true, &out)
		c.Write([]byte("no newline"))
		c.flush()

		if got, want := out.String(), "[web] no newline\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("quiet streams nothing but still records", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		c := newCapture("web", false, &out)
		c.Write([]byte("one\n"))
		c.flush()

		if out.Len() != 0 {
			t.Errorf("streamed %q with verbose off", out.String())
		}
		if got, want := c.String(), "one\n"; got != want {
			t.Errorf("capture = %q, want %q", got, want)
		}
	})

}

func TestPrintSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printSummary(&buf, 2, 1, 3)

	want := "\n=== Summary ===\nSucceeded: 2\nFailed:    1\nSkipped:   3\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
