package main

import (
	"os"

	"github.com/jarrettgilliam/mkprs/internal/mkprs"
)

func main() {
	os.Exit(mkprs.Run(os.Args[1:], os.Stdout, os.Stderr))
}
