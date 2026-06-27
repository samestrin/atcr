// Command td-migrate is a one-off entry point for the technical-debt storage
// migration (Epic 12.1). All logic lives in internal/tdmigrate so it is unit-
// testable; this shim only wires os.Args/streams to tdmigrate.Run. It is
// removable after the canonical cutover (Epic 18.0 / 12.3) and is deliberately
// named outside the planned `atcr debt` namespace.
package main

import (
	"os"

	"github.com/samestrin/atcr/internal/tdmigrate"
)

func main() {
	os.Exit(tdmigrate.Run(os.Args[1:], os.Stdout, os.Stderr))
}
