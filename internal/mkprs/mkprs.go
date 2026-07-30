package mkprs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Run is the whole program: parse, discover, process every repo, report.
//
// It exits the process directly on a usage error, the way a CLI should, and
// otherwise returns normally -- including when individual repos failed. The
// per-repo result lines and the closing summary carry that information instead.
func Run(args []string) {
	cfg := parseArgs(args)

	lg, err := newLogger(cfg.logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer lg.close()

	if cfg.message == "" {
		cfg.message = strings.Join(cfg.command, " ")
	}
	if cfg.title == "" {
		cfg.title = firstLine(cfg.message)
	}

	var repos []string
	for _, dir := range cfg.targetDirs {
		repos = discoverRepos(dir, repos)
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No target repositories found.")
		return
	}

	width := nameWidth(repos)
	var processed, failed, skipped int

	for _, repoPath := range repos {
		name := filepath.Base(repoPath)
		c := newCapture(name, cfg.verbose)

		r := processRepo(cfg, repoPath, c)
		c.flush()

		switch r.outcome {
		case outcomeSuccess:
			resultOK(os.Stdout, width, name, r.note)
			processed++
		case outcomeSkipped:
			resultSkip(os.Stdout, width, name, r.note)
			skipped++
		default:
			resultFail(os.Stdout, width, name, r.note, c)
			failed++
		}

		lg.record(cfg, r, c)
	}

	printSummary(os.Stdout, processed, failed, skipped)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
