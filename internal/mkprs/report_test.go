package mkprs

import (
	"bytes"
	"testing"
)

func TestNameWidthMinimum(t *testing.T) {
	t.Parallel()
	if got := nameWidth(nil); got != 1 {
		t.Errorf("nameWidth(nil) = %d, want 1", got)
	}
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

// The line appears only when there are repos the run never reached, and its
// label is the longest, so the whole block widens with it.
func TestSummaryCountsReposNotProcessed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := &reporter{out: &buf, succeeded: 2, failed: 1, skipped: 3, notProcessed: 4}
	r.summary()

	want := "\n=== Summary ===\nSucceeded:     2\nFailed:        1\nSkipped:       3\nNot processed: 4\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
