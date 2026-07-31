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

// flush emits a trailing fragment that never got its newline. Without this the
// last line of output from a command that doesn't end in \n is lost.
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
}

func newReporter(out io.Writer, repos []string) *reporter {
	return &reporter{out: out, width: nameWidth(repos)}
}

func (r *reporter) summary() {
	fmt.Fprintln(r.out, "")
	fmt.Fprintln(r.out, "=== Summary ===")
	fmt.Fprintf(r.out, "Succeeded: %d\n", r.succeeded)
	fmt.Fprintf(r.out, "Failed:    %d\n", r.failed)
	fmt.Fprintf(r.out, "Skipped:   %d\n", r.skipped)
}
