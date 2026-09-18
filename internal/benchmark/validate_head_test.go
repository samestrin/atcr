package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LoadRepoState's doc asserts "every filesystem precondition is checked at load",
// and names the reason: "A tier whose whole purpose is detecting unwinnable cases
// must not ship one silently." The two preconditions that actually decide whether a
// case is WINNABLE were the two it did not check — that each expected finding's
// file exists in the head state, and that its line range falls inside that file.
//
// They cannot be checked at load, because the head state does not exist until the
// case is materialized. So they are checked HERE, immediately after
// materialization and before the first paid completer call for the case.
func TestValidateAgainstHead_RejectsALineRangePastTheEndOfTheFile(t *testing.T) {
	c := materializableCase(t)
	// The head file is three lines long. Citing 9999 makes the expectation
	// unsatisfiable: no reviewer can settle it, so the case scores every reviewer
	// zero on that finding for a reason that has nothing to do with the reviewer.
	c.ExpectedFindings[0].LineStart = 9999
	c.ExpectedFindings[0].LineEnd = 9999

	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err, "materialization itself is fine; the case is unwinnable, not unbuildable")

	err = ValidateAgainstHead(c, mc.Root)
	require.Error(t, err, "an expectation citing a line the head file does not have is unwinnable and must be refused")
	assert.Contains(t, err.Error(), "apply-case", "the error names the case")
	assert.Contains(t, err.Error(), "pkg/example.py", "and the file")
	assert.Contains(t, err.Error(), "9999", "and the line that does not exist")
}

// The other half: a file the head state does not contain at all.
func TestValidateAgainstHead_RejectsAFileMissingFromTheHeadState(t *testing.T) {
	c := materializableCase(t)
	c.ExpectedFindings[0].File = "pkg/never_existed.py"

	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	err = ValidateAgainstHead(c, mc.Root)
	require.Error(t, err, "an expectation about a file that is not in the head tree can never be settled")
	assert.Contains(t, err.Error(), "pkg/never_existed.py")
}

// A directory is not a file. Without the mode check a case citing `pkg` would pass
// the existence test and then be bounds-checked against a line count of zero,
// producing a confusing diagnostic for a different defect.
func TestValidateAgainstHead_RejectsADirectoryCitedAsAFile(t *testing.T) {
	c := materializableCase(t)
	c.ExpectedFindings[0].File = "pkg"

	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	err = ValidateAgainstHead(c, mc.Root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")
}

// The gate must not fire on a good case, or it rejects the shipped suite. Both the
// in-range expectation and a last-line one are accepted: an off-by-one in the
// bounds check would reject the final line of every file.
func TestValidateAgainstHead_AcceptsAWinnableCase(t *testing.T) {
	c := materializableCase(t)
	c.ExpectedFindings = append(c.ExpectedFindings, ExpectedFinding{
		ID: "last", File: "pkg/example.py", LineStart: 3, LineEnd: 3,
		OutsideDiff: boolPtr(true), Category: "correctness", Summary: "s",
	})

	mc, err := MaterializeCase(context.Background(), c, t.TempDir())
	require.NoError(t, err)

	require.NoError(t, ValidateAgainstHead(c, mc.Root),
		"a three-line file must accept an expectation on line 3")
}

// The whole shipped suite must satisfy its own gate, or the tier ships the defect
// this check exists to detect.
func TestValidateAgainstHead_TheShippedSuitePasses(t *testing.T) {
	m, err := LoadRepoState("../../benchmarks/repo-state-v1")
	require.NoError(t, err)
	require.NotEmpty(t, m.Cases)

	for _, c := range m.Cases {
		dest := filepath.Join(t.TempDir(), "repo")
		require.NoError(t, os.MkdirAll(dest, 0o755))
		mc, merr := MaterializeCase(context.Background(), c, dest)
		require.NoError(t, merr, "case %q", c.ID)
		assert.NoError(t, ValidateAgainstHead(c, mc.Root), "shipped case %q must be winnable", c.ID)
	}
}
