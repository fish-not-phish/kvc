package main

import (
	"fmt"
	"os"

	"github.com/fish-not-phish/kvc/cmd"
)

// version is set to the current release. Override at build time with
// -ldflags "-X main.version=<value>" (the Makefile and install.sh do this
// when a git tag is available, falling back to this default otherwise).
var version = "v1.0.2"

func main() {
	cmd.Version = version
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kvc:", err)
		os.Exit(1)
	}
}
