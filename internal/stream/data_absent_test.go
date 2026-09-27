package stream

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An envelope with NO data key must report "missing data.findings", the same
// error a null data does — not a raw "unexpected end of JSON input" leaking
// from the exact-keys decode of a nil RawMessage. Guards the absent-key case
// the v2.go empty-data comment claims (but does not deliver) for empty/null.
func TestParseV2Envelope_DataAbsentIsRejected(t *testing.T) {
	_, err := parseV2Envelope(`{"axi_format":"json"}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing data.findings",
		"absent data key must report the missing-findings error, not a decode error")

	// An empty-object data falls through to the nil-Findings check and reports
	// the same missing-findings error (pre-existing behavior, unchanged).
	_, err = parseV2Envelope(`{"axi_format":"json","data":{}}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing data.findings")
}
