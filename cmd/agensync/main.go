// Command agensync clones/migrates a project's AI-coding-agent configuration
// from one tool to one or more others.
package main

import (
	"fmt"
	"os"

	"github.com/YangTaeyoung/agensync/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
