package pathutil

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedHome(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func noHome() (string, error) { return "", errors.New("no home") }

func TestRelHome(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")

	assert.Equal(t, "~/go/bin/atcr",
		RelHome(filepath.Join(home, "go", "bin", "atcr"), fixedHome(home)),
		"a path under home renders with a ~/ prefix, forward-slash on every platform")
	assert.Equal(t, "~", RelHome(home, fixedHome(home)), "the home dir itself renders as ~")

	outside := filepath.FromSlash("/usr/local/bin/atcr")
	assert.Equal(t, outside, RelHome(outside, fixedHome(home)),
		"a path outside home falls back to the verbatim path")

	parent := filepath.FromSlash("/home")
	assert.Equal(t, parent, RelHome(parent, fixedHome(home)),
		`a path whose Rel is exactly ".." falls back verbatim, never "~/.."`)

	sibling := filepath.FromSlash("/home/testuser2/x")
	assert.Equal(t, sibling, RelHome(sibling, fixedHome(home)),
		"a sibling sharing home's name as a string prefix is outside home")

	dotted := filepath.Join(home, "..foo")
	assert.Equal(t, "~/..foo", RelHome(dotted, fixedHome(home)),
		`a child whose name starts with ".." is still under home`)
}

func TestRelHome_UnresolvableHome(t *testing.T) {
	p := filepath.FromSlash("/usr/local/bin/atcr")
	assert.Equal(t, p, RelHome(p, noHome), "an erroring home resolver returns the path verbatim")
	assert.Equal(t, p, RelHome(p, fixedHome("")), "an empty home is unresolvable: path is returned verbatim")
}

func TestExpandHome(t *testing.T) {
	home := filepath.FromSlash("/home/testuser")

	got, err := ExpandHome("~", fixedHome(home))
	require.NoError(t, err)
	assert.Equal(t, home, got, "~ alone expands to home")

	got, err = ExpandHome("~/x", fixedHome(home))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "x"), got, "~/x expands under home")

	got, err = ExpandHome("~/.bashrc", fixedHome(home))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".bashrc"), got)

	for _, p := range []string{filepath.FromSlash("/etc/profile"), "rel/path", "~user/x", "x~/y", ""} {
		got, err = ExpandHome(p, fixedHome(home))
		require.NoError(t, err)
		assert.Equal(t, p, got, "a path without a leading ~ or ~/ is returned unchanged: %q", p)
	}
}

func TestExpandHome_UnresolvableHome(t *testing.T) {
	for _, resolve := range []func() (string, error){noHome, fixedHome("")} {
		_, err := ExpandHome("~", resolve)
		assert.Error(t, err, "~ needs a home")
		_, err = ExpandHome("~/x", resolve)
		assert.Error(t, err, "~/x needs a home")

		p := filepath.FromSlash("/etc/profile")
		got, err := ExpandHome(p, resolve)
		require.NoError(t, err, "a plain path never needs home, so an unresolvable home is not an error")
		assert.Equal(t, p, got)
	}
}

func TestExpandHome_ResolvesHomeOnlyWhenNeeded(t *testing.T) {
	calls := 0
	resolve := func() (string, error) { calls++; return "/h", nil }
	_, _ = ExpandHome("plain", resolve)
	assert.Zero(t, calls, "a plain path does not look up home")
}
