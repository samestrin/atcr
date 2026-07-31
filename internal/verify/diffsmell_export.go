package verify

// diffsmell_export.go is the EXPORTED seam over diffsmell.go's package-local
// analyzer. Epic 35.3 deliberately scoped the diff-smell gate to the in-process
// fix-selection path and listed "any subprocess or CLI shell-out" as out of
// scope, so every type in diffsmell.go is unexported. A downstream consumer that
// shells out to `atcr verify diff` needs a stable, marshalable shape, and
// `cli` is a different package — hence this shim rather than a rename.
//
// It is a pure translation layer: no detection logic lives here, and
// diffsmell.go is untouched, so the ported analyzer and its corpus tests stay
// the single source of truth for what a smell IS.
//
// The JSON field names mirror upstream llm-tools `diff-smell` (`smell.type`,
// `smell.severity`, `summary.verdict`, …) so a consumer already parsing that
// tool's output can point at atcr without reshaping.

// Verdict values returned in DiffSmellSummary.Verdict.
const (
	VerdictClean    = smellVerdictClean
	VerdictSoftOnly = smellVerdictSoftOnly
	VerdictHard     = smellVerdictHard
)

// Severity values returned in DiffSmell.Severity. HARD is an unambiguous
// reward-hack fingerprint; SOFT means "a human should glance at this".
const (
	SeverityHard = smellSeverityHard
	SeveritySoft = smellSeveritySoft
)

// DiffSmell is one over-simplification fingerprint found in a diff.
type DiffSmell struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	File     string `json:"file"`
	Evidence string `json:"evidence"`
}

// DiffSmellFiles lists the changed files split by role.
type DiffSmellFiles struct {
	Test []string `json:"test"`
	Impl []string `json:"impl"`
}

// DiffSmellSummary aggregates the scan.
type DiffSmellSummary struct {
	TestFiles int            `json:"test_files"`
	ImplFiles int            `json:"impl_files"`
	Hard      int            `json:"hard"`
	Soft      int            `json:"soft"`
	ByType    map[string]int `json:"by_type"`
	Verdict   string         `json:"verdict"`
}

// DiffSmellResult is the full diff-smell scan output.
type DiffSmellResult struct {
	Files   DiffSmellFiles   `json:"files"`
	Smells  []DiffSmell      `json:"smells"`
	Summary DiffSmellSummary `json:"summary"`
}

// LooksLikeUnifiedDiff reports whether text is plausibly a unified diff.
// Callers must pre-filter with it: AnalyzeDiff assumes real diff input, and
// free-form content fed to it would be mis-attributed rather than parsed.
func LooksLikeUnifiedDiff(text string) bool {
	return looksLikeUnifiedDiff(text)
}

// AnalyzeDiff scans a unified diff for over-simplification fingerprints. It
// never returns nil.
//
// NOTE: unlike the fix-selection gate (evaluateFixSmell), this applies NO
// policy suppression — in particular a diff touching only test files raises the
// HARD `test_only` smell unconditionally, because a standalone caller has no
// finding whose own path could justify the exemption.
func AnalyzeDiff(diff string) *DiffSmellResult {
	res := analyzeDiff(diff)

	// Every slice/map is materialized non-nil so the JSON form is [] / {} rather
	// than null: a consumer indexing `smells` or `by_type` must not have to
	// nil-check a field that is merely empty. analyzeDiff already guarantees
	// non-nil Files/ByType, but Smells is built by append, so a clean scan would
	// otherwise marshal as null.
	out := &DiffSmellResult{
		Smells: make([]DiffSmell, 0, len(res.Smells)),
		Files: DiffSmellFiles{
			Test: append([]string{}, res.Files.Test...),
			Impl: append([]string{}, res.Files.Impl...),
		},
		Summary: DiffSmellSummary{
			TestFiles: res.Summary.TestFiles,
			ImplFiles: res.Summary.ImplFiles,
			Hard:      res.Summary.Hard,
			Soft:      res.Summary.Soft,
			ByType:    make(map[string]int, len(res.Summary.ByType)),
			Verdict:   res.Summary.Verdict,
		},
	}
	// A direct struct conversion, not a field-by-field literal: Go permits it
	// because the two shapes are identical (json tags are ignored in struct
	// conversions), and that makes the coupling COMPILE-TIME. Adding or renaming
	// a field on `smell` without mirroring it here breaks the build instead of
	// silently dropping the new field from every consumer's JSON.
	for _, s := range res.Smells {
		out.Smells = append(out.Smells, DiffSmell(s))
	}
	for k, v := range res.Summary.ByType {
		out.Summary.ByType[k] = v
	}
	return out
}
