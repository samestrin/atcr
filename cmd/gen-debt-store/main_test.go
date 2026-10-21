package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
)

func TestRun_WritesTheRequestedStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".atcr", "debt")
	var stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"-dir", dir, "-n", "40"}, &stderr), stderr.String())
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 40)
}

// Append refuses to recreate a missing store root (a guard against writing into
// a deleted checkout), so the tool creates the directory it was pointed at.
func TestRun_CreatesAMissingStoreDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh", ".atcr", "debt")
	var stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"-dir", dir, "-n", "2"}, &stderr), stderr.String())
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2)
}

func TestRun_UsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"no dir":     {},
		"stray arg":  {"-dir", t.TempDir(), "extra"},
		"bad flag":   {"-nope"},
		"bad number": {"-dir", t.TempDir(), "-n", "many"},
	} {
		t.Run(name, func(t *testing.T) {
			var stderr bytes.Buffer
			require.Equal(t, 2, run(args, &stderr))
			require.NotEmpty(t, stderr.String())
		})
	}
}

func TestRun_GenerateFailureIsExitOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	var stderr bytes.Buffer
	require.Equal(t, 1, run([]string{"-dir", file, "-n", "1"}, &stderr))
	require.Contains(t, stderr.String(), "gen-debt-store:")

	stderr.Reset()
	require.Equal(t, 1, run([]string{"-dir", t.TempDir(), "-n", "-1"}, &stderr))
	require.Contains(t, stderr.String(), "non-negative")
}
