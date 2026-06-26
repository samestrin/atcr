package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWireToolDefs_ExecGating(t *testing.T) {
	readOnly := wireToolDefs(false)
	names := map[string]bool{}
	for _, d := range readOnly {
		names[d.Name] = true
	}
	// The default read-only set never exposes execution tools.
	assert.False(t, names["run_tests"], "non-exec agent must not see run_tests")
	assert.False(t, names["run_script"], "non-exec agent must not see run_script")
	assert.True(t, names["read_file"], "read-only tools are always present")

	exec := wireToolDefs(true)
	execNames := map[string]bool{}
	for _, d := range exec {
		execNames[d.Name] = true
	}
	// An execution-enabled agent gets the read-only set PLUS the exec tools.
	assert.True(t, execNames["read_file"])
	assert.True(t, execNames["run_tests"], "exec agent must see run_tests")
	assert.True(t, execNames["run_script"], "exec agent must see run_script")
	assert.Greater(t, len(exec), len(readOnly))
}
