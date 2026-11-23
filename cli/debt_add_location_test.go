package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebtAdd_RejectsNegativeLineAndOutsidePaths is the EST-8260 probe:
// `--file a.go:-3` and `--file ../../etc/passwd:1` were stored as given.
// `debt validate` already reports such paths as outside the repository; add
// now refuses them before anything is written.
func TestDebtAdd_RejectsNegativeLineAndOutsidePaths(t *testing.T) {
	for loc, want := range map[string]string{
		"a.go:-3":            "1-based",
		"a.go:-0":            "1-based",
		"../../etc/passwd:1": "outside the repository",
		"../sibling/a.go":    "outside the repository",
		"/etc/passwd:1":      "outside the repository",
		"a/../../b.go:2":     "outside the repository",
	} {
		t.Run(loc, func(t *testing.T) {
			dir := emptyDebtStore(t)
			_, err := runDebt(t, "add", "--dir", dir,
				"--severity", "LOW", "--file", loc,
				"--problem", "p", "--fix", "f", "--category", "c")
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err))
			assert.Contains(t, err.Error(), want)
			assert.Empty(t, readDebtStore(t, dir), "a rejected add writes nothing")
		})
	}
}

func TestDebtAdd_InRepoLocationsStillAccepted(t *testing.T) {
	for _, loc := range []string{"a.go:3", "internal/x/y.go", "a/./b.go:1", "cmd/atcr/autofix.go:248-260", "see docs: the thing"} {
		t.Run(loc, func(t *testing.T) {
			dir := emptyDebtStore(t)
			_, err := runDebt(t, "add", "--dir", dir,
				"--severity", "LOW", "--file", loc,
				"--problem", "p", "--fix", "f", "--category", "c")
			require.NoError(t, err)
			assert.Len(t, readDebtStore(t, dir), 1)
		})
	}
}
