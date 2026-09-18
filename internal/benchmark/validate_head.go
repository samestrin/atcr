package benchmark

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// ValidateAgainstHead checks the two preconditions that actually decide whether a
// case is WINNABLE: every expected finding's file exists in the materialized head
// tree, and its line range falls inside that file.
//
// It is NOT part of LoadRepoState, and that is not an oversight. The head state
// does not exist until the case is materialized — it is the base tree with the
// case's diff applied — so the loader cannot reach these facts however eagerly it
// reads. LoadRepoState's doc claims "every filesystem precondition is checked at
// load" and names the reason ("A tier whose whole purpose is detecting unwinnable
// cases must not ship one silently"); this function is where that promise is
// actually kept, for the two preconditions the loader structurally cannot check.
//
// Called by the runner immediately after MaterializeCase and BEFORE the case's
// first paid completer call, so an unwinnable case costs a materialization rather
// than a panel. Until it existed the only thing catching this shape was an
// assertion inside materialize_test.go, which covers the bundled suite and nothing
// an operator passes through --suite-path.
//
// Line counting is deliberately byte-level rather than a scanner over the decoded
// text: the expectations index the file as a reviewer's editor would, and a file
// whose last line carries no trailing newline still has that line.
func ValidateAgainstHead(c RepoStateCase, root string) error {
	// One read per distinct file, not per finding: a case planting four
	// expectations in one file would otherwise read it four times.
	lines := map[string]int{}
	for _, f := range c.ExpectedFindings {
		n, ok := lines[f.File]
		if !ok {
			// filepath.Join cleans the path, and f.File was validated by
			// isSafeRelPOSIXPath at load, so it cannot escape root.
			p := filepath.Join(root, filepath.FromSlash(f.File))
			fi, err := os.Lstat(p)
			if err != nil {
				return fmt.Errorf("case %q expected_finding %q cites file %q, which is not in the materialized head state: %w; "+
					"the expectation can never be settled, so the case would score every reviewer zero for a reason unrelated to the reviewer",
					c.ID, f.ID, f.File, err)
			}
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("case %q expected_finding %q cites %q, which is not a regular file in the head state",
					c.ID, f.ID, f.File)
			}
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return fmt.Errorf("case %q expected_finding %q: reading head file %q: %w", c.ID, f.ID, f.File, rerr)
			}
			n = countLines(data)
			lines[f.File] = n
		}
		// LineEnd only: Validate already established LineStart >= 1 and
		// LineEnd >= LineStart at load, so the upper bound is the whole check.
		if f.LineEnd > n {
			return fmt.Errorf("case %q expected_finding %q cites %s:%d-%d, but the head state's %q has %d line(s); "+
				"no reviewer can cite a line that does not exist, so the expectation is unwinnable",
				c.ID, f.ID, f.File, f.LineStart, f.LineEnd, f.File, n)
		}
	}
	return nil
}

// countLines returns the number of lines in data, counting a final line that
// carries no trailing newline. An empty file has zero lines, which is what makes
// any expectation against it fail the bounds check above.
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}
