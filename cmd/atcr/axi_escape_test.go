package main

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/samestrin/atcr/internal/report"
)

// AC 04-02: a pinning/regression test guaranteeing NO ANSI/OSC escape sequence
// ever reaches --axi stdout. It is the runtime backstop to renderAXI's structural
// guarantee (toonEscape drops every control byte but the five valid TOON escapes):
// the TOON/JSON payload is escape-free by construction, and this test fails loudly
// if a future change ever regresses that. Styled after
// TestDriftLine_StripsControlChars / TestRenderPersonaSearch_StripsControlChars for
// discoverability alongside the existing sanitization precedent.

// axiEscapePattern matches any terminal escape-sequence introducer that could ride
// the payload: the 7-bit ESC byte (0x1b) — which introduces every ANSI CSI (\x1b[),
// OSC (\x1b]), DCS, APC, and charset sequence — and the 8-bit C1 controls
// (U+0080–U+009F), which include the single-byte CSI (U+009B) and OSC (U+009D)
// forms a terminal honors identically. This mirrors exactly what renderAXI's
// toonEscape strips (unicode.IsControl covers 0x00–0x1f and 0x7f–0x9f), so a
// regression that bypassed toonEscape and leaked EITHER a raw ESC OR a raw C1 byte
// is caught — not just the two 7-bit CSI/OSC forms the AC names as its baseline
// (AC 04-02 Edge Case 2). The C0 whitespace controls the TOON payload legitimately
// uses as structure — newline row separators (\n), tabs, CR — are deliberately NOT
// in this set, so a well-formed payload never false-positives.
var axiEscapePattern = regexp.MustCompile(`\x1b|[\x{80}-\x{9f}]`)

// findEscapeSequence returns the byte offset of the first ANSI/OSC escape
// introducer in s and whether one was found (offset -1 when none).
func findEscapeSequence(s string) (int, bool) {
	if loc := axiEscapePattern.FindStringIndex(s); loc != nil {
		return loc[0], true
	}
	return -1, false
}

// requireNoEscapeSequence fails the test with a byte-offset diagnostic if s carries
// any ANSI/OSC escape introducer — the AC 04-02 Error Scenario 1 message shape, so
// a regression names exactly which byte offset (and the surrounding bytes) tripped
// the detector rather than passing silently.
func requireNoEscapeSequence(t *testing.T, s, label string) {
	t.Helper()
	if off, found := findEscapeSequence(s); found {
		t.Fatalf("unexpected escape sequence in %s at byte offset %d: %q", label, off, s[off:min(off+8, len(s))])
	}
}

// TestAXIEscapeDetector_FlagsOSC8PositiveControl is the known-bad control case: it
// reproduces osc8()'s EXACT output (quickstart.go:459-461) — the one place atcr
// provably emits raw OSC-8 escapes on an interactive path — and confirms the
// detector flags it. This proves the detector actually works: the zero-match
// assertions in the review/resume tests below are only meaningful because this
// positive control shows the same detector fails on a real escape sequence.
func TestAXIEscapeDetector_FlagsOSC8PositiveControl(t *testing.T) {
	fixture := osc8("https://example.com")
	off, found := findEscapeSequence(fixture)
	require.True(t, found, "the detector must flag osc8()'s raw OSC-8 escape pattern")
	require.Equal(t, 0, off, "osc8 output begins with the ESC introducer")
}

// TestAXIEscapeDetector_FlagsBothCSIAndOSC covers AC 04-02 Edge Case 2: a payload
// carrying both a CSI (\x1b[) and an OSC (\x1b]) sequence must have BOTH detected —
// the detector must not be narrowly scoped to only one of the two forms.
func TestAXIEscapeDetector_FlagsBothCSIAndOSC(t *testing.T) {
	_, csi := findEscapeSequence("color \x1b[31m red \x1b[0m done")
	_, osc := findEscapeSequence("link \x1b]8;;https://x\x1b\\ text")
	assert.True(t, csi, "a CSI escape (\\x1b[) must be detected")
	assert.True(t, osc, "an OSC escape (\\x1b]) must be detected")
}

// TestAXIEscapeDetector_FlagsC1Introducers proves the detector also catches the
// 8-bit C1 single-byte introducers a terminal honors identically to their 7-bit
// ESC forms — CSI (U+009B) and OSC (U+009D) — closing the gap where a raw C1 byte
// could leak past a detector scoped to only `\x1b[`/`\x1b]` (4.5.A MEDIUM). This is
// the C1 analogue of the osc8() positive control.
func TestAXIEscapeDetector_FlagsC1Introducers(t *testing.T) {
	_, csi8 := findEscapeSequence("data 31m x")   // 8-bit CSI (C1)
	_, osc8b := findEscapeSequence("data 8;;u x") // 8-bit OSC (C1)
	assert.True(t, csi8, "the 8-bit C1 CSI introducer (U+009B) must be detected")
	assert.True(t, osc8b, "the 8-bit C1 OSC introducer (U+009D) must be detected")
}

// TestAXIRender_CraftedFindingFieldEscapeStripped is AC 04-02 Edge Case 1: a review
// finding whose free-text fields were crafted (e.g. by a compromised persona/
// catalog entry) to carry raw CSI/OSC escapes must not smuggle those bytes,
// unescaped, into the --axi findings payload. Rendered through the real FormatAXI
// encoder, the escape bytes are stripped by toonEscape (TOON defines no \x/\u
// escape), so no raw escape rides the machine-consumed payload — while the
// surrounding legitimate text survives.
func TestAXIRender_CraftedFindingFieldEscapeStripped(t *testing.T) {
	findings := []reconcile.JSONFinding{{
		Severity:   "HIGH",
		File:       "a.go",
		Line:       10,
		Problem:    "danger \x1b[31mRED\x1b[0m alert",       // crafted CSI color codes
		Fix:        "audit \x1b]8;;https://evil\x1b\\ link", // crafted OSC hyperlink
		Category:   "security",
		EstMinutes: 15,
		Evidence:   "see \x1b[1mbold\x1b[0m",
		Confidence: "high",
	}}
	var buf bytes.Buffer
	require.NoError(t, report.Render(&buf, findings, report.FormatAXI))
	out := buf.String()

	requireNoEscapeSequence(t, out, "renderAXI crafted-field payload")
	// Assert the residual-but-safe form: the ESC byte of "\x1b[31mRED" is stripped
	// while the rest of the run ("[31mRED") survives, so this distinguishes "ESC
	// specifically removed, text kept" from "whole field silently dropped" or "field
	// passed through intact" — a signal a bare Contains("RED") would not give.
	assert.Contains(t, out, "[31mRED", "the ESC byte is stripped but the surrounding text survives")
	assert.Contains(t, out, "alert")
}

// TestAXIStdout_NoEscapeSequences_Review is AC 04-02 Scenario 1: a real
// `atcr review --axi` run's captured stdout carries zero ANSI/OSC escape bytes.
func TestAXIStdout_NoEscapeSequences_Review(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce")

	// A plain `review --axi` (exit 0) — deliberately NOT gated on --fail-on/exit-1,
	// so this escape-pinning test does not break for the unrelated reason of a
	// changed findings-gate or mock verdict; it asserts only the escape-free property.
	code, stdout, _ := execCmdSplit(t, "review", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "review --axi completes -> exit 0")
	require.NotEmpty(t, stdout, "review --axi emits the run-summary payload")
	requireNoEscapeSequence(t, stdout, "review --axi stdout")
}

// TestAXIStdout_NoEscapeSequences_Resume is AC 04-02 Scenario 2: a real
// `atcr review --resume --axi` run's captured stdout carries zero ANSI/OSC escapes.
func TestAXIStdout_NoEscapeSequences_Resume(t *testing.T) {
	isolate(t)
	t.Setenv(testReviewKeyEnv, "secret")
	initGitRepoWithChange(t)
	srv := liveMockProvider(t)
	liveReviewConfig(t, srv.URL, "bruce", "robin")
	base := gitRevParse(t, "HEAD^")
	head := gitRevParse(t, "HEAD")
	writeResumeReviewFixture(t, "2026-06-18_demo", base, head, []string{"bruce", "robin"}, []string{"bruce"})

	code, stdout, _ := execCmdSplit(t, "review", "--resume", "latest", "--axi", "--base", "HEAD^")
	require.Equal(t, 0, code, "resume completes -> exit 0")
	require.NotEmpty(t, stdout, "resume --axi emits the run-summary payload")
	requireNoEscapeSequence(t, stdout, "resume --axi stdout")
}
