package mkprs

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// capture collects everything a repo's command, git and gh emit. Output always
// lands in buf; under --verbose it is also streamed live to out, line by line
// and prefixed with the repo name.
type capture struct {
	buf     bytes.Buffer
	pending []byte
	name    string
	verbose bool
	out     io.Writer
}

func newCapture(name string, verbose bool, out io.Writer) *capture {
	return &capture{name: name, verbose: verbose, out: out}
}

// Write makes capture an io.Writer, so it can be handed straight to
// exec.Cmd.Stdout. The error is always nil, and callers may ignore it: the
// buffer cannot fail, and a broken stdout is not a reason to abandon a repo's
// command mid-run.
func (c *capture) Write(p []byte) (int, error) {
	c.buf.Write(p)
	if !c.verbose {
		return len(p), nil
	}

	c.pending = append(c.pending, p...)
	for {
		i := bytes.IndexByte(c.pending, '\n')
		if i < 0 {
			break
		}
		fmt.Fprintf(c.out, "[%s] %s\n", c.name, c.pending[:i])
		c.pending = c.pending[i+1:]
	}
	return len(p), nil
}

func (c *capture) flush() {
	if c.verbose && len(c.pending) > 0 {
		fmt.Fprintf(c.out, "[%s] %s\n", c.name, c.pending)
		c.pending = nil
	}
}

func (c *capture) String() string { return c.buf.String() }
func (c *capture) empty() bool    { return c.buf.Len() == 0 }

// indented returns the whole capture, each line indented by four spaces.
func (c *capture) indented() string {
	text := strings.TrimSuffix(c.buf.String(), "\n")
	if text == "" {
		return ""
	}

	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

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
