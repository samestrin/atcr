// Command ingest-alibaba-benchmark builds the bundled benchmarks/standard-v1
// suite from Alibaba's aacr-bench dataset (github.com/alibaba/aacr-bench,
// Apache-2.0). It downloads dataset/positive_samples.json, takes a seeded
// sample of PR records, fetches each one's diff, and writes suite.json plus the
// diff files per the manifest contract in docs/benchmark.md.
//
// This is an authoring-time tool: its output is committed, so neither CI nor
// any benchmark run re-executes it. All logic lives in
// internal/benchmarkimport so it stays unit-testable; this shim only wires
// os.Args/streams to benchmarkimport.Run.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/samestrin/atcr/internal/benchmarkimport"
)

func main() {
	// The context is threaded through FetchDataset, FetchDiff, the retry backoff,
	// and gitexec's git children. Without a signal handler it can never be
	// cancelled, so Ctrl-C during a multi-gigabyte clone kills this process
	// outright and orphans the clone tree. NotifyContext converts the signal
	// into a cancellation those callees already honor, letting the run unwind
	// and exit non-zero through Run's own fail path.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := benchmarkimport.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)

	// Before os.Exit, which would skip a defer. Restoring the default signal
	// disposition also means a signal arriving during teardown terminates
	// normally instead of being swallowed.
	stop()
	os.Exit(code)
}
