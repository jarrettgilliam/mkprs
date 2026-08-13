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

// reporter renders one result line per repo and keeps the running tally. It is
// the only place that knows what each kind of outcome looks like on stdout.
type reporter struct {
	out            io.Writer
	width          int
	succeededCount int
	failedCount    int
	skippedCount   int
	// notProcessedCount counts the repos the run never reached, which only -s
	// can produce. They are counted apart from the skips -- nothing looked at
	// them, so mkprs cannot say they had nothing to do -- but counted, so the
	// summary adds up to the number of repos discovered.
	notProcessedCount int
}

func newReporter(out io.Writer, repos []string) *reporter {
	return &reporter{out: out, width: nameWidth(repos)}
}

// record renders one repo's result and counts it. output is the repo's capture,
// replayed under a failure.
func (r *reporter) record(name string, o outcome, output *capture) {
	switch o.kind {
	case succeeded:
		// gh can exit 0 without a parseable URL on stdout. The PR still exists,
		// so this reports success and simply omits the link.
		suffix := ""
		if o.prURL != "" {
			suffix = "  " + o.prURL
		}
		fmt.Fprintf(r.out, "✅ %-*s PR created%s\n", r.width, name, suffix)
		r.succeededCount++

	case skipped:
		fmt.Fprintf(r.out, "⏭️  %-*s skipped: %s\n", r.width, name, reasonOrDefault(o.reason))
		r.skippedCount++

	case failed:
		fmt.Fprintf(r.out, "❌ %-*s %s\n", r.width, name, reasonOrDefault(o.reason))
		// Under --verbose the output has already streamed past; don't repeat it.
		if output != nil && !output.verbose && !output.empty() {
			fmt.Fprint(r.out, output.indented())
		}
		r.failedCount++
	}
}

// reasonOrDefault keeps a result line from trailing off into nothing when an outcome
// arrived without a reason.
func reasonOrDefault(reason string) string {
	if reason == "" {
		return "reason unknown"
	}
	return reason
}

func (r *reporter) summary() {
	// "Not processed" is the longest label and usually absent, so the column is
	// only as wide as the lines actually printed.
	label := "Succeeded:"
	if r.notProcessedCount > 0 {
		label = "Not processed:"
	}
	width := len(label)

	fmt.Fprintln(r.out, "")
	fmt.Fprintln(r.out, "=== Summary ===")
	fmt.Fprintf(r.out, "%-*s %d\n", width, "Succeeded:", r.succeededCount)
	fmt.Fprintf(r.out, "%-*s %d\n", width, "Failed:", r.failedCount)
	fmt.Fprintf(r.out, "%-*s %d\n", width, "Skipped:", r.skippedCount)
	if r.notProcessedCount > 0 {
		fmt.Fprintf(r.out, "%-*s %d\n", width, "Not processed:", r.notProcessedCount)
	}
}
