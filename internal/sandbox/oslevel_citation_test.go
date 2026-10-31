package sandbox

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dockerLineCitation matches a docker.go line citation such as "docker.go:80-88".
var dockerLineCitation = regexp.MustCompile(`docker\.go:\d`)

// TestOSLevelFiles_CiteDockerGoBySymbolNotLine pins the fix for citation drift:
// a line number into docker.go goes stale the moment docker.go gains a line
// (epic 86 T6 added 14), so the OS-level backend and its caller cite
// DockerBackend symbols by name instead.
func TestOSLevelFiles_CiteDockerGoBySymbolNotLine(t *testing.T) {
	files, err := filepath.Glob("oslevel*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	files = append(files, filepath.Join("..", "..", "cli", "autofix.go"))

	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.Empty(t, dockerLineCitation.FindAllString(string(raw), -1),
			"%s cites docker.go by line number; cite the symbol by name", f)
	}
}
