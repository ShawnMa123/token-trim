// Command rtk-go is a local command-output trimmer for Codex CLI.
package main

import (
	"os"

	"github.com/ShawnMa123/token-trim/go/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
