package mkprs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// discoverRepos appends every repository found under targetDir to repos, in
// lexical order.
//
//   - Only .git *directories* count. A submodule's .git is a file holding a
//     "gitdir:" pointer, so dropping the IsDir check would start walking into
//     submodules.
//   - A repo's whole subtree is pruned, so a repo nested inside another is not
//     returned alongside it. Naming the inner one as a target still works: it is
//     then the walk's own root, and nothing has pruned it.
//
// An error means the target could not be interpreted, and the run stops before
// any repo is touched -- see targetError. A target that is understood but holds
// no repos is not an error: `~/repos/*` picking up a plain notes/ folder
// alongside the repos is ordinary.
func discoverRepos(targetDir string, repos []string) ([]string, error) {
	info, err := os.Stat(targetDir)
	switch {
	case os.IsNotExist(err):
		return repos, fmt.Errorf("target directory does not exist: %s", targetDir)
	case err != nil:
		return repos, fmt.Errorf("target directory cannot be read: %s: %w", targetDir, err)
	}

	before := len(repos)

	// A file holds no repos and is not walked -- naming one must not quietly
	// process every repo in the directory that happens to contain it. `~/repos/*`
	// sweeps up a README alongside the repos, so this is ordinary rather than a
	// mistake, and the inside-a-repo check below still applies to where it sits.
	inside := targetDir
	if !info.IsDir() {
		inside = filepath.Dir(targetDir)
	} else {
		err = filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// An unreadable subtree shouldn't abort discovery of the rest.
				return nil //nolint:nilerr
			}
			if !d.IsDir() {
				return nil
			}

			// The question is asked of each directory rather than of the .git
			// entry itself, because SkipDir returned from a directory skips
			// *that* directory -- returning it from .git prunes only .git, which
			// is the whole bug this avoids.
			//
			// Lstat, not Stat: DirEntry.IsDir does not follow symlinks, so a
			// symlinked .git has always been excluded here, and Stat would
			// quietly start including it.
			if info, err := os.Lstat(filepath.Join(path, ".git")); err == nil && info.IsDir() {
				repos = append(repos, path)
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			return repos, fmt.Errorf("error while searching %s: %w", targetDir, err)
		}
	}

	// Only a target that found nothing pays for this, so a target holding many
	// repos is untouched. A repo root is found by the walk above, so it never
	// reaches here.
	if len(repos) == before {
		if root, err := git(inside, "rev-parse", "--show-toplevel"); err == nil && root != "" {
			return repos, fmt.Errorf(
				"target is inside repository %s: %s\nmkprs runs commands at the repo root, so pass that instead",
				root, targetDir)
		}
	}

	return repos, nil
}

// dedupeRepos removes repos reached from more than one target, keeping the
// first occurrence, and returns the copies it discarded -- one entry per copy,
// so a repo named three times appears twice here.
//
// First occurrence rather than a sort: order is lexical *within* each target
// dir, in the order the targets were given, and sorting the combined list would
// quietly change that.
func dedupeRepos(repos []string) (unique, dropped []string) {
	seen := make(map[string]bool, len(repos))
	for _, repo := range repos {
		if seen[repo] {
			dropped = append(dropped, repo)
			continue
		}
		seen[repo] = true
		unique = append(unique, repo)
	}
	return unique, dropped
}
