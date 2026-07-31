package mkprs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// discoverRepos appends every repository found under targetDir to repos.
//
// This reproduces `find <dir> -type d -name .git -prune -print0` exactly:
//
//   - Only .git *directories* count. A submodule's .git is a file holding a
//     "gitdir:" pointer, so submodules are not repositories for our purposes --
//     dropping the IsDir check would silently start walking into them.
//   - Pruning stops descent into .git itself, not into the rest of the tree, so
//     a repository nested inside another repository is still discovered. Both
//     the parent and the child are returned.
//
// Unlike find, WalkDir yields entries in lexical order, so the result is
// deterministic.
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
