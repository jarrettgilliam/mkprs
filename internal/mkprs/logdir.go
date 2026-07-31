package mkprs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const tsvHeader = "repo_path\tstatus\tbranch\tcommit_sha\tpr_url\tnotes\n"

// logger writes the per-repo logs and summary.tsv that --log asks for. A nil
// logger is valid and does nothing: absent --log, nothing touches the disk.
type logger struct {
	dir     string
	summary *os.File
	warn    io.Writer
}

func newLogger(dir string, warn io.Writer) (*logger, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create log directory: %s", dir)
	}

	summary, err := os.Create(filepath.Join(dir, "summary.tsv"))
	if err != nil {
		return nil, fmt.Errorf("could not create log directory: %s", dir)
	}
	if _, err := summary.WriteString(tsvHeader); err != nil {
		// No logger will exist, so nothing else can close this.
		_ = summary.Close()
		return nil, fmt.Errorf("could not create log directory: %s", dir)
	}

	return &logger{dir: dir, summary: summary, warn: warn}, nil
}

func (l *logger) close() {
	if l == nil {
		return
	}
	_ = l.summary.Close()
}

// logFileFor picks a free <name>.log. Two repos can share a basename under
// different target dirs; suffix rather than let the second clobber the first.
func (l *logger) logFileFor(name string) string {
	candidate := filepath.Join(l.dir, name+".log")
	for n := 2; ; n++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(l.dir, fmt.Sprintf("%s-%d.log", name, n))
	}
}

// tsvField keeps a value on one field of one row: TSV has no quoting rules.
func tsvField(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ").Replace(s)
}

// record writes one <repo>.log plus one summary.tsv row.
func (l *logger) record(cfg *config, r *repoResult, c *capture) {
	if l == nil {
		return
	}

	// A repoResult built anywhere but processRepo can carry no outcome at all.
	// An unrecorded result must never read as success.
	status, note := "failed", ""
	if r.outcome != nil {
		status, note = r.outcome.String(), r.outcome.note()
	}

	// On success the note is the PR URL; it belongs in pr_url, not in notes too.
	prURL, tsvNote := "", note
	if s, ok := r.outcome.(outcomeSuccess); ok {
		prURL, tsvNote = s.prURL, ""
	}

	command := r.resolvedCommand
	if command == "" {
		command = strings.Join(cfg.command, " ")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "repo:     %s\n", r.path)
	fmt.Fprintf(&b, "branch:   %s\n", cfg.branch)
	fmt.Fprintf(&b, "command:  %s\n", command)
	fmt.Fprintf(&b, "status:   %s\n", status)
	fmt.Fprintf(&b, "commit:   %s\n", dashIfEmpty(r.commitSHA))
	fmt.Fprintf(&b, "note:     %s\n", dashIfEmpty(note))
	b.WriteString("----------------------------------------\n")
	b.WriteString(c.String())

	dest := l.logFileFor(filepath.Base(r.path))
	if err := os.WriteFile(dest, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(l.warn, "Warning: could not write %s: %v\n", dest, err)
	}

	fmt.Fprintf(l.summary, "%s\t%s\t%s\t%s\t%s\t%s\n",
		tsvField(r.path),
		status,
		tsvField(cfg.branch),
		r.commitSHA,
		tsvField(prURL),
		tsvField(tsvNote))
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
