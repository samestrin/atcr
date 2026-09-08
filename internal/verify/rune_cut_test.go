package verify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/samestrin/atcr/internal/tools"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emDash is three bytes in UTF-8 (U+2014). Real source text is full of runes
// like it; the clamp tests that shipped used pure-ASCII fixtures, which is why
// the walk below was never executed.
const emDash = "—"

// TestRuneCeilCut covers the boundary arithmetic directly. The function is a
// one-screen helper reached only through boundedDispatcher, and its walk was
// entirely uncovered — mutation-verified: neutralising it left the whole suite
// green, as did widening the n >= len(s) boundary.
func TestRuneCeilCut(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"n equals length returns everything", "abc", 3, "abc"},
		{"n beyond length returns everything", "abc", 9, "abc"},
		{"n zero returns nothing", "abc", 0, ""},
		{"n negative returns nothing", "abc", -5, ""},
		{"empty string", "", 4, ""},
		{"ascii cut is exact", "abcdef", 2, "ab"},
		{"cut on a rune boundary is exact", "ab" + emDash + "cd", 2, "ab"},
		// The cut lands one and two bytes INTO the em dash. Slicing there would
		// emit a partial rune; the boundary is walked FORWARD so the caller is
		// never handed less than it asked for.
		{"cut one byte into a rune walks forward", "ab" + emDash + "cd", 3, "ab" + emDash},
		{"cut two bytes into a rune walks forward", "ab" + emDash + "cd", 4, "ab" + emDash},
		{"cut at the last rune walks to the end", "a" + emDash, 2, "a" + emDash},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runeCeilCut(tc.s, tc.n)
			assert.Equal(t, tc.want, got)
			assert.True(t, utf8.ValidString(got), "a cut must never split a rune")
			if tc.n > 0 && tc.n < len(tc.s) {
				assert.GreaterOrEqual(t, len(got), tc.n,
					"the cut must reach the allowance, never fall short of it — see boundedDispatcher")
			}
		})
	}
}

// TestBoundedDispatcher_MultiByteCutStillReachesTheAllowance is the reason the
// cut rounds UP rather than down.
//
// clampDispatcher deliberately allows budget+1 bytes: internal/fanout/loop.go
// trips on `ToolBytes > ToolBudgetBytes`, a STRICTLY-greater test, so delivering
// at most the budget exactly would leave it false forever and the trip would
// never fire. A cut that walked the offset DOWN to a rune boundary returned
// fewer bytes than the allowance, ToolBytes landed at exactly the budget, and
// the tool_budget_bytes trip silently slipped a turn — defeating, on ordinary
// UTF-8 source text, the very mechanism the clamp exists to preserve.
func TestBoundedDispatcher_MultiByteCutStillReachesTheAllowance(t *testing.T) {
	const budget = int64(10)

	// The tenth byte of this content is the middle of an em dash, so the
	// allowance (budget+1 = 11) cannot be met by an exact slice.
	content := strings.Repeat("a", 9) + emDash + strings.Repeat("b", 40)
	require.Greater(t, len(content), int(budget)+1)

	d := clampDispatcher(&fakeDispatcher{result: tools.ToolResult{Content: content, OriginalBytes: len(content)}}, budget)

	out, err := d.Execute(context.Background(), "read", json.RawMessage(`{}`))
	require.NoError(t, err)

	assert.True(t, out.Truncated, "the result was cut, so it must say so")
	assert.Equal(t, len(content), out.OriginalBytes, "the transcript records what the tool actually produced")
	assert.True(t, utf8.ValidString(out.Content), "the content is serialised into a JSON body — a split rune is a provider-side reject")
	assert.Greater(t, int64(len(out.Content)), budget,
		"the delivered bytes must exceed the budget or loop.go's strictly-greater trip never fires")
}

// TestBoundedDispatcher_AsciiCutIsUnchanged pins that rounding up costs nothing
// on the ASCII path: the allowance is met exactly, as it always was.
func TestBoundedDispatcher_AsciiCutIsUnchanged(t *testing.T) {
	const budget = int64(10)
	content := strings.Repeat("a", 40)

	d := clampDispatcher(&fakeDispatcher{result: tools.ToolResult{Content: content, OriginalBytes: len(content)}}, budget)

	out, err := d.Execute(context.Background(), "read", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, int(budget)+1, len(out.Content),
		"budget+1 exactly: the smallest overrun that still lets the trip fire")
}

// TestBoundedDispatcher_ResultThatFitsIsNotMarkedTruncated guards the other
// side of the rounding. Walking forward can reach the end of the content; when
// nothing was actually removed, the result must not claim it was.
func TestBoundedDispatcher_ResultThatFitsIsNotMarkedTruncated(t *testing.T) {
	d := clampDispatcher(&fakeDispatcher{result: tools.ToolResult{Content: "ab" + emDash}}, 100)

	out, err := d.Execute(context.Background(), "read", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.False(t, out.Truncated)
	assert.Equal(t, "ab"+emDash, out.Content)
	assert.Zero(t, out.OriginalBytes, "nothing was cut, so nothing is restated")
}

// TestRuneCeilCut_InvalidUTF8DoesNotBypassTheClamp bounds the forward walk.
// Content that is not valid UTF-8 has no RuneStart byte to walk to; an unbounded
// search would run to the end and hand back the WHOLE result, defeating the
// ceiling on precisely the input least worth trusting.
func TestRuneCeilCut_InvalidUTF8DoesNotBypassTheClamp(t *testing.T) {
	// 0x80 is a continuation byte: never a rune start, so there is no boundary
	// anywhere after the offset.
	s := "ab" + strings.Repeat("\x80", 500)

	got := runeCeilCut(s, 4)
	assert.LessOrEqual(t, len(got), 4+utf8.UTFMax,
		"the overshoot is bounded by the longest UTF-8 encoding, never by the length of the content")
}
