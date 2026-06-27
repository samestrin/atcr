// Command td-migrate is a one-off aid for Epic 12.1: it converts the flat
// Markdown technical-debt table into a directory of per-item Markdown files
// (and regenerates the table from them). All logic lives in
// internal/tdmigrate; this entry point only wires stdio and the exit code.
package main

import (
	"os"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

func main() {
	os.Exit(tdmigrate.Main(os.Args[1:], os.Stdout, os.Stderr))
}
