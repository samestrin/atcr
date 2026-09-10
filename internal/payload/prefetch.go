package payload

import (
	"github.com/samestrin/atcr/internal/astgroup"
)

// Context-aware pre-fetching (Epic 35.16.8).
//
// A reviewer receives the changed regions and nothing else, so a defect whose
// mechanism lives in a file the diff never touched is unreachable: the model
// cannot cite what it was never shown, and internal/fanout's grounding gate
// would drop the finding even if it guessed. This file retrieves that missing
// context by REFERENCE — the call sites that consume a changed symbol — rather
// than by resemblance, because a consumer is related to the code it calls by
// reference, not by looking like it.

// maxChangedSymbols caps how many distinct symbols one diff contributes to the
// reference lookup. The cap bounds the `git grep` argv and, transitively, the
// candidate file set every later stage parses — the AC4 latency budget is spent
// per candidate file, so bounding the symbol set is what bounds the run.
const maxChangedSymbols = 40

// changedSymbol is one symbol the diff touched, with the shape a consumer would
// have to agree with.
//
// Signature carries the declaration header (from astgroup.FileSkeleton) rather
// than the name alone: AC5 is about a changed signature or return shape, and a
// reviewer handed only a name cannot tell whether a call site still agrees with
// it. It is empty when the file has no parser or the declaration could not be
// sliced — a graceful degradation, never an error.
//
// Mocked marks a symbol discovered in a changed TEST file on a line that mocks,
// patches, stubs or fakes something (AC6). Those symbols are retrieved so the
// REAL implementation sits next to the test that replaces it, which is what
// makes a mock that does not model the real behavior legible to a reader.
type changedSymbol struct {
	Name      string
	Signature string
	Mocked    bool
}

// extractChangedSymbols returns the symbol set the diff touched in one file.
//
// src is the file's HEAD text, ranges its head-side changed line ranges, root
// its parsed tree, and isTest whether it is a test file (which enables the
// mock-cue scan). A file with no parser passes a zero root and contributes only
// its mock cues.
func extractChangedSymbols(src string, ranges []LineRange, root astgroup.Node, isTest bool) []changedSymbol {
	// Stub: T1 is not implemented yet. Returning nil is a deliberate wrong
	// answer so the RED test fails on behavior while the package still compiles
	// (the pre-commit `go vet` gate rejects a test naming an absent symbol).
	return nil
}
