package report

import (
	"bytes"
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestRenderAXI_SanitizesViaGoAxi pins that the standard TOON path cleans text
// with go-axi rather than the legacy hand-rolled stripper: go-axi drops an
// invalid UTF-8 byte, where the legacy path writes U+FFFD.
func TestRenderAXI_SanitizesViaGoAxi(t *testing.T) {
	findings := []reconcile.JSONFinding{
		{Severity: "HIGH", File: "a.go", Line: 1, Confidence: "MEDIUM", Problem: "bad \xff byte"},
	}
	var b bytes.Buffer
	require.NoError(t, Render(&b, findings, FormatAXI))
	assert.Contains(t, b.String(), goaxi.SanitizeString("bad \xff byte"))
	assert.NotContains(t, b.String(), "�", "the standard path must not use the legacy U+FFFD replacement")
}
