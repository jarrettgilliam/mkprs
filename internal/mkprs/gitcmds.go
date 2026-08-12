package mkprs

// gitArgs is one git invocation's arguments. Every git command mkprs runs is
// named below, so a call site reads as intent rather than as git flags.
type gitArgs []string

func fetch() gitArgs { return gitArgs{"fetch", "origin", "--quiet", "--prune"} }

func createBranch(name, base string) gitArgs {
	return gitArgs{"checkout", "-b", name, base, "--quiet"}
}

func checkoutBranch(name string) gitArgs { return gitArgs{"checkout", name, "--quiet"} }

func deleteBranch(name string) gitArgs { return gitArgs{"branch", "-D", name} }

func stageAll() gitArgs { return gitArgs{"add", "-A"} }

// nothingStaged succeeds when the index holds no changes: --quiet makes diff
// exit 0 for an empty diff and 1 otherwise.
func nothingStaged() gitArgs { return gitArgs{"diff", "--cached", "--quiet"} }

func commit(message string) gitArgs { return gitArgs{"commit", "-q", "-m", message} }

func push(branch string) gitArgs { return gitArgs{"push", "-u", "origin", branch, "--quiet"} }

func remoteOriginURL() gitArgs { return gitArgs{"config", "--get", "remote.origin.url"} }

func status() gitArgs { return gitArgs{"status", "--porcelain"} }

func originHeadRef() gitArgs {
	return gitArgs{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"}
}

func currentHeadRef() gitArgs { return gitArgs{"symbolic-ref", "--short", "HEAD"} }

func localBranchExists(branch string) gitArgs { return verifyRef("refs/heads/" + branch) }

func originBranchExists(branch string) gitArgs {
	return verifyRef("refs/remotes/origin/" + branch)
}

func verifyRef(ref string) gitArgs { return gitArgs{"rev-parse", "--verify", "--quiet", ref} }

func commitsAhead(base, branch string) gitArgs {
	return gitArgs{"rev-list", "--count", base + ".." + branch}
}

func repoRoot() gitArgs { return gitArgs{"rev-parse", "--show-toplevel"} }

func checkFormat(branch string) gitArgs {
	return gitArgs{"check-ref-format", "refs/heads/" + branch}
}
