package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// The store holds review findings, so the tool creates its directory owner-only,
// the same 0o700 localdebt's ensureStoreDir uses for a real store.
func TestRun_CreatesTheStoreDirectoryOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply on Windows")
	}
	dir := filepath.Join(t.TempDir(), "fresh", ".atcr", "debt")
	var stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"-dir", dir, "-n", "1"}, &stderr), stderr.String())
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

// MkdirAll leaves an existing directory's mode alone, so a store directory made
// earlier with a looser umask is tightened to owner-only as well.
func TestRun_TightensAnExistingStoreDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply on Windows")
	}
	dir := filepath.Join(t.TempDir(), "debt")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Chmod(dir, 0o755))
	var stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"-dir", dir, "-n", "1"}, &stderr), stderr.String())
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
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

// Pointed at a live .atcr/debt by mistake, the tool would append 50k synthetic
// records to a real append-only store. A non-empty existing directory is refused
// unless -force says the caller means it.
func TestRun_RefusesANonEmptyExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-01.jsonl"), []byte("{}\n"), 0o600))
	var stderr bytes.Buffer
	require.Equal(t, 1, run([]string{"-dir", dir, "-n", "2"}, &stderr))
	require.Contains(t, stderr.String(), "not empty")
	require.Contains(t, stderr.String(), "-force")
	got, err := os.ReadFile(filepath.Join(dir, "2026-01.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(got), "the refused run wrote nothing")
}

// -force lifts the directory guard for a dir holding non-record files, but the
// generator itself still refuses a store that already holds records, so even a
// forced re-run cannot double the corpus.
func TestRun_ForceAllowsNonEmptyDirButNeverAppendsToRecords(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600))
	var stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"-dir", dir, "-n", "3", "-force"}, &stderr), stderr.String())

	stderr.Reset()
	require.Equal(t, 1, run([]string{"-dir", dir, "-n", "3", "-force"}, &stderr))
	require.Contains(t, stderr.String(), "already holds records")
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 3)
}

func TestRun_AcceptsAnExistingEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	require.Equal(t, 0, run([]string{"-dir", dir, "-n", "2"}, &stderr), stderr.String())
}

// -dir goes through the same validation.FilePath the review --output-dir does,
// on the resolved absolute path, before anything is created.
func TestRun_ValidatesTheDirectory(t *testing.T) {
	for _, dir := range []string{"/etc/atcr-debt", "/proc/atcr-debt", "/sys/atcr-debt"} {
		t.Run(dir, func(t *testing.T) {
			var stderr bytes.Buffer
			require.Equal(t, 2, run([]string{"-dir", dir, "-n", "1"}, &stderr))
			require.Contains(t, stderr.String(), "system directories")
		})
	}
}

func TestRun_RejectsCountAboveCeiling(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "debt")
	var stderr bytes.Buffer
	require.Equal(t, 1, run([]string{"-dir", dir, "-n", fmt.Sprint(localdebt.MaxSyntheticRecords + 1)}, &stderr))
	require.Contains(t, stderr.String(), fmt.Sprint(localdebt.MaxSyntheticRecords))
}
