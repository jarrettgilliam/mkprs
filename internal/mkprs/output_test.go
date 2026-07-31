package mkprs

import (
	"bytes"
	"strings"
	"testing"
)

func TestNameWidthMinimum(t *testing.T) {
	t.Parallel()
	if got := nameWidth(nil); got != 1 {
		t.Errorf("nameWidth(nil) = %d, want 1", got)
	}
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

func TestSummary(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := &reporter{out: &buf, succeeded: 2, failed: 1, skipped: 3}
	r.summary()

	want := "\n=== Summary ===\nSucceeded: 2\nFailed:    1\nSkipped:   3\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
