package mkprs

import (
	"fmt"
	"io"
	"path/filepath"
)

// nameWidth aligns the result column to the longest repo name.
func nameWidth(repos []string) int {
	width := 1
	for _, repo := range repos {
		if n := len(filepath.Base(repo)); n > width {
			width = n
		}
	}
	return width
}

// reporter renders one result line per repo and keeps the running tally.
// Outcomes report themselves through it, so nothing here knows which kind it is
// holding.
type reporter struct {
	out                        io.Writer
	width                      int
	succeeded, failed, skipped int
	// notProcessed counts the repos the run never reached, which only -s can
	// produce. They are counted apart from the skips -- nothing looked at them,
	// so mkprs cannot say they had nothing to do -- but counted, so the summary
	// adds up to the number of repos discovered.
	notProcessed int
}

func newReporter(out io.Writer, repos []string) *reporter {
	return &reporter{out: out, width: nameWidth(repos)}
}

func (r *reporter) summary() {
	// "Not processed" is the longest label and usually absent, so the column is
	// only as wide as the lines actually printed.
	label := "Succeeded:"
	if r.notProcessed > 0 {
		label = "Not processed:"
	}
	width := len(label)

	fmt.Fprintln(r.out, "")
	fmt.Fprintln(r.out, "=== Summary ===")
	fmt.Fprintf(r.out, "%-*s %d\n", width, "Succeeded:", r.succeeded)
	fmt.Fprintf(r.out, "%-*s %d\n", width, "Failed:", r.failed)
	fmt.Fprintf(r.out, "%-*s %d\n", width, "Skipped:", r.skipped)
	if r.notProcessed > 0 {
		fmt.Fprintf(r.out, "%-*s %d\n", width, "Not processed:", r.notProcessed)
	}
}
