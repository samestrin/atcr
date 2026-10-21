package modelslug

import (
	"strings"
	"testing"
)

func TestSanitize_KeepsCatalogSlugs(t *testing.T) {
	for _, s := range []string{
		"",
		"claude-sonnet-4-6",
		"openai/gpt-4o",
		"anthropic:claude-3.5-sonnet@20241022",
		"meta-llama/Llama-3.1-8B-Instruct+fp8",
		"~user/model_v2",
		strings.Repeat("a", MaxLen),
	} {
		if got := Sanitize(s); got != s {
			t.Errorf("Sanitize(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestSanitize_DropsUnsafeSlugs(t *testing.T) {
	for name, s := range map[string]string{
		"space":           "claude sonnet",
		"leading space":   " claude-sonnet-4-6",
		"trailing tab":    "claude-sonnet-4-6\t",
		"newline":         "claude\nsonnet",
		"NUL":             "claude\x00",
		"DEL":             "claude\x7f",
		"too long":        strings.Repeat("a", MaxLen+1),
		"email-like text": "jane.doe@example.com is my name, call me",
		"comma":           "a,b",
		"quote":           `a"b`,
		"backslash":       `a\b`,
		"percent":         "a%20b",
		"non-ascii":       "clàude",
		"unicode space":   "claude sonnet",
	} {
		if got := Sanitize(s); got != "" {
			t.Errorf("%s: Sanitize(%q) = %q, want empty", name, s, got)
		}
	}
}
