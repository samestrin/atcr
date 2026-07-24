package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/samestrin/atcr/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bulk of what was cmd/atcr's test suite relocated to the cli package alongside
// the command tree (Sprint 34.0 Task 03). These two tests cover only the thin
// shim that remains: that main() delegates to cli.Main (exit-code + stream
// passthrough) and reimplements none of the command tree itself.

// TestShim_DelegatesToCliMain exercises the exact entry point the shim calls —
// os.Exit(cli.Main(ctx, os.Stdout, os.Stderr)) — proving exit-code passthrough
// and that the shim-provided stdout is the writer subcommands render into. It
// does not re-test the command tree (that lives in the cli package); it locks the
// wiring the shim is responsible for.
func TestShim_DelegatesToCliMain(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"atcr", "version"}

	var stdout, stderr bytes.Buffer
	code := cli.Main(context.Background(), &stdout, &stderr)

	assert.Equal(t, 0, code, "`atcr version` must return exit code 0 through cli.Main")
	assert.Contains(t, stdout.String(), "atcr version",
		"version output must route to the stdout writer the shim passes into cli.Main")
}

// TestShim_ContainsNoCommandConstruction locks the thin-shim contract: all cobra
// command-tree construction must live in the cli package so the public binary and
// the private atcr-enterprise wrapper build an identical tree. If a future edit
// reintroduces command wiring into package main, this fails loudly instead of
// silently forking the two binaries' behavior.
func TestShim_ContainsNoCommandConstruction(t *testing.T) {
	data, err := os.ReadFile("main.go")
	require.NoError(t, err)
	src := string(data)

	assert.NotContains(t, src, "cobra.Command",
		"cmd/atcr/main.go must not construct cobra commands; it must delegate to the cli package")
	assert.Contains(t, src, "cli.Main(",
		"cmd/atcr/main.go must delegate to cli.Main")
}
