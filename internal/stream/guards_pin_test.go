package stream

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pins for guards a mutation-testing pass flagged as redundant (TD: sprint
// 35.16.11.2 post-review). Each test deletes nothing: it asserts the behavior
// the guard produces, so a future "cleanup" that removes the guard fails here
// instead of silently changing what the parsers accept.

// Guard v2.go: the len(env.Data)==0 || data=="null" pre-check ahead of
// checkExactKeys/json.Unmarshal. Without it a null data still errors later
// (Findings stays nil) — but with a DIFFERENT error path, so the guard's
// specific message is what this pins.
func TestParseV2Envelope_DataNullIsRejected(t *testing.T) {
	// Real JSON null reaches the guard's own branch.
	_, err := parseV2Envelope(`{"axi_format":"json","data":null}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing data.findings",
		"null data reports the missing-findings error, not a decode error")

	// The string "null" is rejected too, via the exact-keys decode.
	_, err = parseV2Envelope(`{"axi_format":"json","data":"null"}`)
	require.Error(t, err)
}

// Guard parser.go IsNoFindings: the no-space-after-token check. Without it the
// sentinel consumption would run straight into "[]" and consume it as an empty
// findings value, calling "NO FINDINGS[]" clean.
func TestIsNoFindings_SentinelFollowedByValueStaysUnclean(t *testing.T) {
	assert.False(t, IsNoFindings("NO FINDINGS[]"),
		"a sentinel glued to a value is not a clean review")
	assert.False(t, IsNoFindings("[]NO FINDINGS"),
		"a value glued to the sentinel is not a clean review")
}

// Guard v2.go flexInt.UnmarshalJSON: the int32 clamp. The outer decode clamps
// est_minutes again, so this only shows on 32-bit platforms — but the type's
// own contract is "always fits in int32", and this pin holds everywhere.
func TestFlexInt_ClampsToThirtyTwoBitRange(t *testing.T) {
	var big, small flexInt
	require.NoError(t, json.Unmarshal([]byte("1e300"), &big))
	assert.Equal(t, flexInt(math.MaxInt32), big, "1e300 clamps to MaxInt32")

	require.NoError(t, json.Unmarshal([]byte("-1e300"), &small))
	assert.Equal(t, flexInt(math.MinInt32), small, "-1e300 clamps to MinInt32")

	var hugeString flexInt
	require.NoError(t, json.Unmarshal([]byte(`"5000000000"`), &hugeString))
	assert.Equal(t, flexInt(math.MaxInt32), hugeString, "an out-of-range string number clamps too")
}
