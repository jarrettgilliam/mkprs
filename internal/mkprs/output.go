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

func resultOK(w io.Writer, width int, name, url string) {
	suffix := ""
	if url != "" {
		suffix = "  " + url
	}
	fmt.Fprintf(w, "✅ %-*s PR created%s\n", width, name, suffix)
}

func resultFail(w io.Writer, width int, name, note string, c *capture) {
	fmt.Fprintf(w, "❌ %-*s %s\n", width, name, note)
	// Under --verbose the output has already streamed past; don't repeat it.
	if !c.verbose && !c.empty() {
		fmt.Fprint(w, c.indented())
	}
}

func resultSkip(w io.Writer, width int, name, note string) {
	fmt.Fprintf(w, "⏭️  %-*s skipped: %s\n", width, name, note)
}

func printSummary(w io.Writer, processed, failed, skipped int) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== Summary ===")
	fmt.Fprintf(w, "Succeeded: %d\n", processed)
	fmt.Fprintf(w, "Failed:    %d\n", failed)
	fmt.Fprintf(w, "Skipped:   %d\n", skipped)
}
