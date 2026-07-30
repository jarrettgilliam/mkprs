package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// createPR opens the pull request with gh and returns its URL.
//
// The base is still hardcoded to main, matching the shell version; making it
// follow the repo's default branch is tracked separately.
func createPR(cfg *config, repoPath string, c *capture) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("'gh' (GitHub CLI) is not installed")
	}

	args := []string{
		"pr", "create",
		"--base", "main",
		"--head", cfg.branch,
		"--title", cfg.title,
		"--body", cfg.body,
	}
	if cfg.reviewer != "" {
		args = append(args, "--reviewer", cfg.reviewer)
	}

	// gh prints the PR URL on stdout; that URL is the success line. Both streams
	// are recorded but never echoed live, even under --verbose.
	var stdout bytes.Buffer
	cmd := exec.Command("gh", args...)
	cmd.Dir = repoPath
	cmd.Stdout = &stdout
	cmd.Stderr = c.raw()
	err := cmd.Run()

	fmt.Fprintf(c.raw(), "%s\n", strings.TrimSuffix(stdout.String(), "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to create PR")
	}

	return lastLine(stdout.String()), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	return lines[len(lines)-1]
}
