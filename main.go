// Command mkprs runs a command in every repository under the given directories,
// then commits the result and opens a pull request for each repo that changed.
package main

import (
	"os"

	"github.com/jarrettgilliam/mkprs/internal/mkprs"
)

func main() {
	mkprs.Run(os.Args[1:])
}
