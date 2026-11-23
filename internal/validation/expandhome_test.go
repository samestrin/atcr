package validation

import (
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for in, want := range map[string]string{
		"~":        home,
		"~/td":     filepath.Join(home, "td"),
		"~/a/b.md": filepath.Join(home, "a", "b.md"),
		"td":       "td",
		"/abs/~":   "/abs/~",
		"a/~/b":    "a/~/b",
		"":         "",
		"./~":      "./~",
		"~" + "/":  home,
	} {
		got, err := ExpandHome(in)
		if err != nil || got != want {
			t.Errorf("ExpandHome(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"~bob", "~bob/td", "~~"} {
		if _, err := ExpandHome(in); err == nil {
			t.Errorf("ExpandHome(%q): want an error, ~user is not expanded", in)
		}
	}
}
