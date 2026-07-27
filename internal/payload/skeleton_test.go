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
