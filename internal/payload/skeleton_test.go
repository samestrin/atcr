package payload

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderSkeleton_FormatsHeadersWithLineAnchors(t *testing.T) {
	got := renderSkeleton([]skeletonEntry{
		{StartLine: 5, Header: "type Mode string"},
		{StartLine: 17, Header: "func Simple()"},
	}, 60)

	require.Equal(t, strings.Join([]string{
		skeletonStart,
		"L5: type Mode string",
		"L17: func Simple()",
		skeletonEnd,
		"",
	}, "\n"), got)
}

func TestRenderSkeleton_TruncatesOversizedHeader(t *testing.T) {
	// A generated single-line declaration yields a header of arbitrary length.
	// The MaxSkeletonLines cap bounds the entry COUNT, not the bytes, so without a
	// per-header byte cap one such entry prepends its whole length to the payload.
	huge := "var _bindata = []byte(\"" + strings.Repeat("A", 100*1024) + "\")"

	got := renderSkeleton([]skeletonEntry{{StartLine: 1, Header: huge}}, 60)

	require.Less(t, len(got), 1024, "a 100 KB header must not pass through into the block")
	require.Contains(t, got, "(truncated)", "the clip must be disclosed")
}

func TestRenderSkeleton_BlockStaysUnderCeiling(t *testing.T) {
	entries := make([]skeletonEntry, 1000)
	for i := range entries {
		entries[i] = skeletonEntry{StartLine: i + 1, Header: strings.Repeat("x", 150)}
	}

	got := renderSkeleton(entries, 1000)

	require.LessOrEqual(t, len(got), maxSkeletonBlockBytes+256, "block must stay near the ceiling")
	require.Contains(t, got, "elided", "the omitted remainder must be disclosed")
}

func TestRenderSkeleton_EmptyYieldsNothing(t *testing.T) {
	require.Empty(t, renderSkeleton(nil, 60))
	require.Empty(t, renderSkeleton([]skeletonEntry{}, 60))
}

func TestSkeletonMarkersNeverStartASection(t *testing.T) {
	// The whole placement strategy rests on this: a skeleton line must never be
	// mistaken for the start of a new file section, or the rendered-payload
	// round-trip would attribute the rest of the file to the wrong path.
	skel := renderSkeleton([]skeletonEntry{{StartLine: 1, Header: "func A()"}}, 60)

	for _, ln := range strings.Split(skel, "\n") {
		if ln == "" {
			continue
		}
		require.Falsef(t, isRenderedEntryStart(ln), "skeleton line %q starts a section", ln)
		for _, bad := range []string{"---", "+++", "@@"} {
			require.Falsef(t, strings.HasPrefix(ln, bad), "skeleton line %q collides with diff marker %q", ln, bad)
		}
	}
}

func TestRenderSkeleton_SanitizesHeaderNewlineInjection(t *testing.T) {
	// A header carrying an embedded newline followed by a files-mode marker must
	// not be able to open a second, attacker-named section. renderSkeleton — not a
	// happen-to-collapse upstream in another package — must guarantee this at the
	// render boundary.
	skel := renderSkeleton([]skeletonEntry{
		{StartLine: 1, Header: "func A()\n=== FILE: evil.go ==="},
	}, 60)
	body := injectSkeleton("diff --git a/x.go b/x.go\n@@ -1 +1 @@\n-a\n+b\n", skel)

	entries := EntriesFromRenderedPayload(ModeDiff, body)

	require.Len(t, entries, 1, "the skeleton must not split the payload into a second section")
	for _, e := range entries {
		require.NotEqual(t, "evil.go", e.Path, "no section may be attributed to the injected path")
	}
}

func TestInjectSkeleton_GoesAfterTheEntryStartLine(t *testing.T) {
	body := "diff --git a/x.go b/x.go\n@@ -1 +1 @@\n-old\n+new\n"
	skel := renderSkeleton([]skeletonEntry{{StartLine: 3, Header: "func A()"}}, 60)

	got := injectSkeleton(body, skel)

	lines := strings.Split(got, "\n")
	require.Equal(t, "diff --git a/x.go b/x.go", lines[0],
		"the entry-start line must remain first so section splitting is unaffected")
	require.Equal(t, skeletonStart, lines[1])
	require.Contains(t, got, "@@ -1 +1 @@")
}

func TestInjectSkeleton_EmptySkeletonLeavesBodyUntouched(t *testing.T) {
	body := "diff --git a/x.go b/x.go\n@@ -1 +1 @@\n-old\n+new\n"

	require.Equal(t, body, injectSkeleton(body, ""))
}

func TestInjectSkeleton_SingleLineBodyStillAppends(t *testing.T) {
	body := "[binary file changed: x.bin]\n"
	skel := renderSkeleton([]skeletonEntry{{StartLine: 1, Header: "func A()"}}, 60)

	got := injectSkeleton(body, skel)

	require.True(t, strings.HasPrefix(got, "[binary file changed: x.bin]\n"))
	require.Contains(t, got, skeletonStart)
}

func TestRenderSkeleton_CapsLinesAndDisclosesElision(t *testing.T) {
	entries := make([]skeletonEntry, 10)
	for i := range entries {
		entries[i] = skeletonEntry{StartLine: i + 1, Header: "func F()"}
	}

	got := renderSkeleton(entries, 3)

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	require.Equal(t, skeletonStart, lines[0])
	require.Len(t, lines, 6, "start + 3 headers + elision notice + end")
	require.Equal(t, "... 7 more declaration(s) elided", lines[4],
		"truncation must be disclosed, never silent")
	require.Equal(t, skeletonEnd, lines[5])
}

func TestRenderSkeleton_NoElisionNoticeWhenUnderCap(t *testing.T) {
	got := renderSkeleton([]skeletonEntry{{StartLine: 1, Header: "func A()"}}, 60)

	require.NotContains(t, got, "elided")
}

func TestRenderSkeleton_ZeroCapDisablesInjection(t *testing.T) {
	require.Empty(t, renderSkeleton([]skeletonEntry{{StartLine: 1, Header: "func A()"}}, 0),
		"a zero cap is the operator's switch for turning skeletons off")
}

// The elision notice must not collide with a payload section marker either.
func TestRenderSkeleton_ElisionNoticeIsMarkerSafe(t *testing.T) {
	entries := make([]skeletonEntry, 5)
	for i := range entries {
		entries[i] = skeletonEntry{StartLine: i + 1, Header: "func F()"}
	}

	for _, ln := range strings.Split(renderSkeleton(entries, 2), "\n") {
		if ln == "" {
			continue
		}
		require.False(t, isRenderedEntryStart(ln))
		for _, bad := range []string{"---", "+++", "@@"} {
			require.False(t, strings.HasPrefix(ln, bad))
		}
	}
}
