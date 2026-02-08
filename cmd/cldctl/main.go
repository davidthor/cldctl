// Package main provides the cldctl CLI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/davidthor/cldctl/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
