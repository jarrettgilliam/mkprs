package mkprs

import (
	"fmt"
	"io"
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
//   - Pruning stops descent into .git itself, not into the rest of the tree, so
//     a repo nested inside another repo is discovered too, and both are returned.
func discoverRepos(targetDir string, repos []string, warn io.Writer) []string {
	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(warn, "Warning: Target directory does not exist: %s\n", targetDir)
		return repos
	}

	err = filepath.WalkDir(targetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree shouldn't abort discovery of the rest.
			return nil //nolint:nilerr
		}
		if d.Name() != ".git" {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		repos = append(repos, filepath.Dir(path))
		return fs.SkipDir
	})
	if err != nil {
		fmt.Fprintf(warn, "Warning: error while searching %s: %v\n", targetDir, err)
	}

	return repos
}
