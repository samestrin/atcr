package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnchorDir_CorruptLatestPointerSurfacesCause(t *testing.T) {
	// A corrupt/tampered .atcr/latest must surface ReadLatest's invalid-id
	// message, not be misreported as an absent pointer.
	isolate(t)
	require.NoError(t, os.MkdirAll(".atcr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".atcr", "latest"), []byte("../escape\n"), 0o644))
	_, err := anchorDir("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid review id")
}

func TestAnchorDir_SeparatorDetectionIsPlatformUniform(t *testing.T) {
	// The path-vs-id contract must not depend on filepath.Separator: a
	// relative path written with either separator is an explicit path on
	// every platform, never a bare review id.
	for _, arg := range []string{"reviews/2026-06-11_x", `reviews\2026-06-11_x`} {
		dir, err := anchorDir(arg)
		require.NoError(t, err, "arg %q must be treated as an explicit path", arg)
		require.Equal(t, arg, dir, "explicit path %q must be used verbatim", arg)
	}
}
