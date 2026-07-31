package mkprs

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// pullRequest is what to open, independent of how it gets opened.
type pullRequest struct {
	Base     string
	Head     string
	Title    string
	Body     string
	Reviewer string
}

// prOpener opens a pull request and returns its URL. Everything the attempt
// emits is written to log, which ends up in the repo's capture.
//
// This is the seam that lets the gh CLI be swapped for direct API calls without
// touching processRepo.
type prOpener interface {
	open(repoPath string, pr pullRequest, log io.Writer) (string, error)
}

// ghCLI opens pull requests by shelling out to the GitHub CLI.
type ghCLI struct{}

// ghArgs translates a pullRequest into gh's argv.
//
// --body is passed even when empty: gh prompts interactively without it, which
// would hang a batch run.
func ghArgs(pr pullRequest) []string {
	args := []string{
		"pr", "create",
		"--base", pr.Base,
		"--head", pr.Head,
		"--title", pr.Title,
		"--body", pr.Body,
	}
	if pr.Reviewer != "" {
		args = append(args, "--reviewer", pr.Reviewer)
	}
	return args
}

func (ghCLI) open(repoPath string, pr pullRequest, log io.Writer) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("'gh' (GitHub CLI) is not installed")
	}

	// gh prints the PR URL on stdout; that URL is the success line. Both streams
	// are recorded but never echoed live, even under --verbose.
	var stdout bytes.Buffer
	cmd := exec.Command("gh", ghArgs(pr)...)
	cmd.Dir = repoPath
	cmd.Stdout = &stdout
	cmd.Stderr = log
	err := cmd.Run()

	fmt.Fprintf(log, "%s\n", strings.TrimSuffix(stdout.String(), "\n"))
	if err != nil {
		return "", fmt.Errorf("failed to create PR")
	}

	return lastLine(stdout.String()), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	return lines[len(lines)-1]
}
