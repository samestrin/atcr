package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
