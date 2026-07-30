package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	cfg := parseArgs(os.Args[1:])

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

	// Note the exit status: a run that failed some repos still exits 0. The
	// per-repo results and the summary carry that information instead.
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
