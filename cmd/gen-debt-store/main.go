// Command gen-debt-store builds the synthetic local debt store the debt read
// paths are measured over (epic 35.21 AC7): n deterministic, distinct, open
// records appended through localdebt.Append, ~940 bytes each, so the default
// 50,000 records weigh about what the 35.13 measurement corpus weighed.
//
//	go run ./cmd/gen-debt-store -dir "$TMPDIR/store/.atcr/debt"
//	/usr/bin/time -v atcr debt dashboard --dir "$TMPDIR/store/.atcr/debt" >/dev/null
//
// The directory must be new or empty (pass -force to accept one holding other
// files); a store that already holds records is always refused, so pointing the
// tool at a live .atcr/debt cannot pollute it. -dir is checked with the same
// validation.FilePath as review --output-dir, and -n is capped at
// localdebt.MaxSyntheticRecords.
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
	"path/filepath"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/validation"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("gen-debt-store", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "store directory to create (required; must be new or empty)")
	n := fs.Int("n", 50_000, fmt.Sprintf("number of records to write (at most %d)", localdebt.MaxSyntheticRecords))
	force := fs.Bool("force", false, "accept a non-empty -dir that holds no records")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" || fs.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "usage: gen-debt-store -dir <store dir> [-n records] [-force]")
		return 2
	}
	// Validate the resolved path, as review --output-dir does: a relative path
	// resolves clean and passes, a system directory is a usage error.
	abs, err := filepath.Abs(*dir)
	if err == nil {
		err = validation.FilePath(abs)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-debt-store:", err)
		return 2
	}
	if !*force {
		if err := requireEmptyDir(abs); err != nil {
			_, _ = fmt.Fprintln(stderr, "gen-debt-store:", err)
			return 1
		}
	}
	// Append refuses to recreate a missing store root, which guards real
	// checkouts; a measurement store is built from nothing, so create it here,
	// owner-only like ensureStoreDir.
	if err := os.MkdirAll(abs, 0o700); err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-debt-store:", err)
		return 1
	}
	if err := localdebt.GenerateSynthetic(abs, *n); err != nil {
		_, _ = fmt.Fprintln(stderr, "gen-debt-store:", err)
		return 1
	}
	return 0
}

// requireEmptyDir refuses an existing directory with anything in it. Any read
// error passes through: a missing path is created below, and a path that is not a
// directory is reported by MkdirAll.
func requireEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty; refusing to write a synthetic store into it (pass -force if it holds no records)", dir)
	}
	return nil
}
