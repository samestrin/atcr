// Command gen-debt-store builds the synthetic local debt store the debt read
// paths are measured over (epic 35.21 AC7): n deterministic, distinct, open
// records appended through localdebt.Append, ~940 bytes each, so the default
// 50,000 records weigh about what the 35.13 measurement corpus weighed.
//
//	go run ./cmd/gen-debt-store -dir "$TMPDIR/store/.atcr/debt"
//	/usr/bin/time -v atcr debt dashboard --dir "$TMPDIR/store/.atcr/debt" >/dev/null
//
// This is a measurement tool: nothing in CI or the shipped binary runs it. All
// logic lives in localdebt.GenerateSynthetic so it stays unit-testable; this shim
// only wires os.Args/streams to run.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/samestrin/atcr/internal/localdebt"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("gen-debt-store", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "store directory to append to (required; use an empty one for a store of exactly -n records)")
	n := fs.Int("n", 50_000, "number of records to append")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" || fs.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "usage: gen-debt-store -dir <store dir> [-n records]")
		return 2
	}
	// Append refuses to recreate a missing store root, which guards real
	// checkouts; a measurement store is built from nothing, so create it here.
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-debt-store:", err)
		return 1
	}
	if err := localdebt.GenerateSynthetic(*dir, *n); err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-debt-store:", err)
		return 1
	}
	return 0
}
