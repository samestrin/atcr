// Command refresh-manifest regenerates the bundled synthetic manifest
// (internal/quickstart/synthetic.json) from a live OpenAI-compatible /models
// response read on stdin, writing the updated manifest JSON to stdout. All logic
// lives in internal/quickstart so it stays unit-testable; this shim only wires
// os.Args/streams to quickstart.RunRefresh. It is invoked by the scheduled
// refresh workflow (.github/workflows/refresh-synthetic-manifest.yml).
package main

import (
	"os"

	"github.com/samestrin/atcr/internal/quickstart"
)

func main() {
	os.Exit(quickstart.RunRefresh(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
