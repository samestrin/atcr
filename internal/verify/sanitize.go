package verify

import (
	"regexp"
	"strings"
	"unicode"
)

// Inter-agent context sanitization (Epic 36.4).
//
// Text written by one agent (the executor's Fix) is embedded in another agent's
// prompt (the skeptic's). The sentinel-tagged block in skeptic.go stops that text
// from closing its block early; sanitizeInterAgentText is the complementary content
// pass that removes payloads which could hijack the receiving agent from inside the
// block. It strips four classes, aggressively and line-at-a-time, because a dropped
// line of a fix costs the skeptic little while a surviving payload costs the verdict:
//
//  1. fenced code blocks (``` or ~~~, any length, any indent; an unterminated fence
//     strips to the end of the text),
//  2. shell/command lines (a `$ ` prompt, a shebang, a line that starts with a
//     common command name, or a pipe into a shell anywhere on the line),
//  3. lines carrying an instruction-override phrase ("ignore previous
//     instructions", "you are now", "new instructions:", chat role tags, ...),
//  4. control characters, bidi overrides and zero-width characters.
//
// Control characters are removed first so a zero-width character cannot split an
// override phrase past the line patterns. Text that matches none of the classes is
// returned unchanged (after CRLF normalization), so benign prose survives byte for
// byte. The function is idempotent.

// shellLineRe matches a line that reads as a command to run rather than prose.
// Command names are matched case-sensitively and only at the start of the line, so
// a capitalized sentence ("Run the parser") or a list item ("- curl it") survives.
var shellLineRe = regexp.MustCompile(`^\s*(?:\$\s|#!|(?:sudo|rm|curl|wget|bash|sh|zsh|chmod|chown|eval|exec|nc|ncat|python3?|perl|ruby|node|npm|npx|pip3?|git|docker|kubectl|ssh|scp|powershell)(?:\s|$))|\|\s*(?:sudo\s+)?(?:ba|z)?sh\b`)

// overrideRe matches the instruction-override phrases that try to redirect the
// receiving agent. Case-insensitive; the gaps are bounded so the first alternative
// does not span unrelated clauses of a long line.
var overrideRe = regexp.MustCompile(`(?i)` +
	`\b(?:ignore|disregard|forget|override|bypass)\b.{0,40}?\b(?:previous|prior|above|earlier|preceding|all|any|your|the|system)\b.{0,40}?\b(?:instructions?|prompts?|rules|directives|guidelines|context)\b` +
	`|\byou\s+are\s+now\b` +
	`|\bnew\s+instructions?\s*:` +
	`|\bsystem\s+prompt\b` +
	`|\bpretend\s+(?:to\s+be|you\s+are)\b` +
	`|</?\s*(?:system|assistant|user|human)\s*>` +
	`|^\s*(?:system|assistant|human)\s*:`)

// sanitizeInterAgentText returns s with fenced code blocks, shell/command lines,
// instruction-override lines and control characters removed. See the block comment
// above for the exact classes. Callers sanitize only the copy they embed in another
// agent's prompt; the stored value is left alone.
func sanitizeInterAgentText(s string) string {
	s = stripControlChars(normalizeNewlines(s))
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	fence := "" // the opening fence run while inside a fenced block
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if fence != "" {
			if run := fenceRun(trimmed); run != "" && run[0] == fence[0] && len(run) >= len(fence) && strings.TrimSpace(trimmed[len(run):]) == "" {
				fence = ""
			}
			continue
		}
		if run := fenceRun(trimmed); run != "" {
			fence = run
			continue
		}
		if shellLineRe.MatchString(line) || overrideRe.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// fenceRun returns the leading run of three or more backticks or tildes in s, or ""
// when s does not open with a fence.
func fenceRun(s string) string {
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return ""
	}
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return s[:n]
}

// stripControlChars removes C0/C1 control characters (keeping newline and tab),
// DEL, and Unicode format characters (category Cf: bidi overrides and isolates,
// zero-width spaces and joiners, the byte-order mark). Format characters are
// invisible to a human reader but not to a model, which is what makes them useful
// for hiding a payload.
func stripControlChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}
