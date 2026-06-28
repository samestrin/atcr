package astgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCrossCompileZeroCGO proves the wazero-backed reconciler keeps the project's
// out-of-the-box cross-compilation guarantee: the atcr binary builds for a
// foreign GOOS/GOARCH with CGO_ENABLED=0 (AC1 "zero CGO dependencies" + the
// functional criterion). wazero is pure Go, so there is no C toolchain in the
// path. Skipped under -short.
func TestCrossCompileZeroCGO(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cross-compile build under -short")
	}

	root := repoRootFromHere(t)
	cmd := exec.Command("go", "build", "-o", os.DevNull, "./cmd/atcr")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "zero-CGO cross-compile to linux/arm64 failed:\n%s", out)
}

// repoRootFromHere walks up to the directory containing go.mod for the root
// module (the one whose module path has no trailing segment after atcr).
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if data, rerr := os.ReadFile(filepath.Join(dir, "go.mod")); rerr == nil &&
				len(data) > 0 && hasModuleLine(string(data), "module github.com/samestrin/atcr\n") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (root module go.mod)")
		}
		dir = parent
	}
}

func hasModuleLine(content, line string) bool {
	return len(content) >= len(line) && (content[:len(line)] == line ||
		indexOf(content, "\n"+line) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
