package tdmigrate

import (
	"strings"
	"testing"
)

func TestCellEscapesMarkdown(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"`code`", "\\`code\\`"},
		{"*bold*", "\\*bold\\*"},
		{"[link](url)", "\\[link\\](url)"},
		{"a | b", "a / b"},
	}
	for _, c := range cases {
		got := cell(c.in)
		if got != c.want {
			t.Errorf("cell(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.Contains(got, "|") {
			t.Errorf("cell(%q) still contains a literal pipe: %q", c.in, got)
		}
	}
}
