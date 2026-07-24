// Command atcr is the Agent Team Code Review CLI: it fans a code change out to a
// panel of LLM reviewer personas and reconciles their findings into a single
// deduplicated, confidence-scored deliverable.
//
// All command-tree construction and process-lifecycle logic (signal handling,
// telemetry draining, exit-code mapping) lives in the importable top-level
// package github.com/samestrin/atcr/cli so it is unit-testable and can be
// reused by the private atcr-enterprise wrapper (a separate module, which is
// why the seam is top-level and NOT under internal/). This file is a thin shim
// (mirroring
// cmd/td-migrate/main.go): it only wires os.Stdout/os.Stderr and a background
// context into cli.Main and exits with the returned code.
package main

import (
	"context"
	"os"

	"github.com/samestrin/atcr/cli"
)

func main() {
	os.Exit(cli.Main(context.Background(), os.Stdout, os.Stderr))
}
