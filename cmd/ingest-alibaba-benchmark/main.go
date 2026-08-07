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

	"github.com/samestrin/atcr/internal/benchmarkimport"
)

func main() {
	os.Exit(benchmarkimport.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
