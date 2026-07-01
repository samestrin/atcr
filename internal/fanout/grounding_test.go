package fanout

import (
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/stream"
)

func changedFixture() payload.ChangedLines {
	return payload.ChangedLines{
		"auth.go": {
			Ranges:      []payload.LineRange{{Start: 40, End: 45}},
			ChangedText: []string{"token := parseToken(r)", "return validate(token)"},
		},
	}
}

func TestGroundFindings_KeepsInRange(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 42, Category: "security", Evidence: "x"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("in-range: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_KeepsWithinTolerance(t *testing.T) {
	// line 47 is 2 past range end 45 -> within ±3
	in := []stream.Finding{{File: "auth.go", Line: 47, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("tolerance: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_DropsUngrounded(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 200, Category: "correctness", Evidence: "totally unrelated prose"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("ungrounded: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_KeepsViaEvidence(t *testing.T) {
	// wrong line, but EVIDENCE quotes a changed line -> fuzzy keep
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "security", Evidence: "return validate(token)"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("evidence: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_ExemptsOutOfScope(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "out-of-scope", Evidence: "pre-existing"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("out-of-scope: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_DropsUnknownFile(t *testing.T) {
	// A file the patch never touched is the fabricated-file hallucination class:
	// it must be dropped, not kept (AC2, clarification Q1 hard-drop).
	in := []stream.Finding{{File: "ghost.go", Line: 5, Category: "correctness", Evidence: "invented"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("unknown file: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_KeepsFileLevelOnChangedFile(t *testing.T) {
	// A file-level finding (line 0) on a file the patch changed is kept.
	in := []stream.Finding{{File: "auth.go", Line: 0, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("file-level: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_KeepsBinaryFile(t *testing.T) {
	// A present-but-empty FileChange (binary/mode-only) has no lines to check, so
	// the finding cannot be proven ungrounded — fail open.
	changed := payload.ChangedLines{"logo.png": {}}
	in := []stream.Finding{{File: "logo.png", Line: 3, Category: "correctness"}}
	out, dropped := groundFindings(in, changed)
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("binary: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_NormalizesPathPrefix(t *testing.T) {
	// A model citing the diff-side "b/auth.go" must match the bare head-path key,
	// so path-form drift does not cause a false drop under the unknown-file rule.
	in := []stream.Finding{{File: "b/auth.go", Line: 42, Category: "correctness"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("path normalize: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_OutOfScopeExemptOnUnknownFile(t *testing.T) {
	// out-of-scope stays exempt even for a file not in the patch (clarification Q2:
	// out-of-scope is annotated-not-promoted downstream, so it is never dropped here).
	in := []stream.Finding{{File: "ghost.go", Line: 5, Category: "out-of-scope", Evidence: "pre-existing"}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("out-of-scope unknown file: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_NilDataKeepsAll(t *testing.T) {
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness"}}
	out, dropped := groundFindings(in, nil)
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("nil data: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_EmptyMapKeepsAll(t *testing.T) {
	// A non-nil but empty ChangedLines map is a derivation-empty success path;
	// the gate must fail open exactly like nil data, not treat it as an active
	// empty patch that drops every finding.
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness"}}
	out, dropped := groundFindings(in, payload.ChangedLines{})
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("empty map: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_PrefersExactKeyOverDiffPrefix(t *testing.T) {
	// A real top-level directory named "a" or "b" must not be stripped away when
	// the exact key is present in the changed map.
	changed := payload.ChangedLines{
		"a/main.go": {Ranges: []payload.LineRange{{Start: 1, End: 5}}},
	}
	in := []stream.Finding{{File: "a/main.go", Line: 2, Category: "correctness"}}
	out, dropped := groundFindings(in, changed)
	if len(out) != 1 || dropped != 0 {
		t.Fatalf("exact key with a/ prefix: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_EvidenceCapTruncates(t *testing.T) {
	// Evidence is truncated to a bounded length before matching, so a model that
	// emits a huge blob cannot force unbounded work, and text beyond the cap is
	// ignored rather than used to fabricate a fuzzy match.
	const evidenceCap = 4096
	target := "return validate(token)"
	padding := strings.Repeat("x", evidenceCap)
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness", Evidence: padding + target}}
	out, dropped := groundFindings(in, changedFixture())
	if len(out) != 0 || dropped != 1 {
		t.Fatalf("evidence beyond cap: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}

func TestGroundFindings_DropsBoilerplateEvidence(t *testing.T) {
	// "if err != nil {" (15 chars) is ubiquitous Go boilerplate. Even when it is a
	// genuinely changed line, an out-of-range finding whose EVIDENCE is only that
	// boilerplate must NOT ground: the evidence floor sits above it. A distinctive
	// 22-char quote on the same file still grounds via the evidence fallback.
	changed := payload.ChangedLines{
		"auth.go": {
			Ranges:      []payload.LineRange{{Start: 40, End: 45}},
			ChangedText: []string{"if err != nil {", "return validate(token)"},
		},
	}
	boiler := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness", Evidence: "if err != nil {"}}
	if out, dropped := groundFindings(boiler, changed); len(out) != 0 || dropped != 1 {
		t.Fatalf("boilerplate evidence: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
	distinct := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness", Evidence: "return validate(token)"}}
	if out, dropped := groundFindings(distinct, changed); len(out) != 1 || dropped != 0 {
		t.Fatalf("distinctive evidence: kept=%d dropped=%d, want kept=1 dropped=0", len(out), dropped)
	}
}

func TestGroundFindings_EvidenceFloorCountsRunesNotBytes(t *testing.T) {
	// A 6-rune multibyte quote is 18 bytes: len() would count it at the byte floor
	// and let a short hallucinated multibyte snippet clear the gate, so the floor
	// must count runes. Six hiragana (3 bytes each) is well under the rune floor
	// and must be dropped, not grounded by its inflated byte length.
	multibyte := "あいうえおか" // 6 runes, 18 bytes
	changed := payload.ChangedLines{
		"auth.go": {
			Ranges:      []payload.LineRange{{Start: 40, End: 45}},
			ChangedText: []string{multibyte},
		},
	}
	in := []stream.Finding{{File: "auth.go", Line: 999, Category: "correctness", Evidence: multibyte}}
	if out, dropped := groundFindings(in, changed); len(out) != 0 || dropped != 1 {
		t.Fatalf("multibyte evidence: kept=%d dropped=%d, want kept=0 dropped=1", len(out), dropped)
	}
}
