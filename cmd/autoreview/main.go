package main

import (
	"fmt"
	"os"

	"github.com/uinaf/autoreview/internal/buildinfo"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(buildinfo.Version())
		return
	}

	fmt.Fprintln(os.Stderr, "autoreview is under active development; use --version for build information")
	os.Exit(2)
}
