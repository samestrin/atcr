package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTerminalErrorLine_ScrubsSecretShapedTokens covers the one error path that
// never reaches the redactor setupLogger installs: runMain's terminal print
// happens after ExecuteContext returns, when the request-scoped logger (and its
// redactor) no longer exist. Every structured log line is scrubbed; this line
// was not, so a token echoed by a subprocess into an error message reached
// stderr and CI logs verbatim.
//
// The cases are the shapes the root redactor already recognizes, asserted here
// at THIS call site rather than trusted from redact.go's own tests — the defect
// was never that Redact is wrong, it was that nothing called it here.
func TestTerminalErrorLine_ScrubsSecretShapedTokens(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		absent  string
		present string
	}{
		{
			name:    "bearer token echoed by a subprocess",
			err:     errors.New("--exec preflight failed: docker: Authorization: Bearer abc123DEF.ghi-jkl"),
			absent:  "abc123DEF.ghi-jkl",
			present: "--exec preflight failed",
		},
		{
			name:    "sk- key echoed by a daemon",
			err:     errors.New("sandbox preflight: docker daemon unreachable: sk-live-9f3a2b7c1d"),
			absent:  "sk-live-9f3a2b7c1d",
			present: "docker daemon unreachable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := terminalErrorLine(tc.err)
			assert.NotContains(t, got, tc.absent,
				"the terminal error print must be redacted like every other emitted line")
			assert.Contains(t, got, tc.present,
				"redaction must scrub the token, not swallow the operator's diagnostic")
		})
	}
}

// TestTerminalErrorLine_LeavesAnOrdinaryErrorIntact is the paired positive
// control. A redactor that returned "" — or one wired to relativize against a
// root it does not have — would satisfy the scrubbing assertions above while
// destroying every ordinary usage error, so the untouched case is pinned too.
func TestTerminalErrorLine_LeavesAnOrdinaryErrorIntact(t *testing.T) {
	const msg = `unknown flag: --nope
Usage:
  atcr review [flags]`
	assert.Equal(t, msg, terminalErrorLine(errors.New(msg)),
		"an error carrying no secret shape must render byte-identically")
	assert.Equal(t, "", terminalErrorLine(nil))
}
