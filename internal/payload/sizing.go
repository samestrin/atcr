package payload

// Per-agent payload sizing (Epic 19.10 F2/F3). Once each model's full context
// window is known (ContextWindowTokens, F1), the reviewer must size the input
// payload so estimated input tokens fit inside `window - output cap - overhead`,
// then convert that budget into a per-model chunk line count for the Epic 14.3
// chunker. Both derivations are deterministic from (model, outputTokens) and do
// no I/O, satisfying the Determinism NFR.
//
// This file is the fix for the confirmed dax boundary overflow in the 19.6 run:
// a 32768-token-window model was sized as if the whole window were available for
// input, then the never-reserved 8192 output cap pushed every call one token
// past the window (litellm reported the boundary as 24577 = 32768 - 8192 + 1).
// Reserving the output cap in the input sizing calculation prevents the
// exact-boundary class from recurring (AC2).

const (
	// conservativeBytesPerTokenNum / conservativeBytesPerTokenDen express the
	// deliberately conservative ~3.5 bytes/token ratio as an exact rational, so
	// the byte conversion uses integer math with no float rounding surprises. The
	// plan REJECTS the codebase's optimistic ~4.1 B/token assumption implied by
	// the payload_byte_budget comment (internal/registry/project.go:89, "512 KiB
	// ≈ 128k tokens"): over-reserving is acceptable, overflow is not (Conservatism
	// NFR). A larger bytes/token packs FEWER bytes into a given token budget, so
	// 3.5 (below 4.1) deliberately under-fills rather than risks overflow.
	conservativeBytesPerTokenNum = 7
	conservativeBytesPerTokenDen = 2

	// promptOverheadTokens is a generous fixed reservation for the persona +
	// instruction wrapper renderAgent wraps around every payload (system prompt,
	// persona preamble, mode framing). No such measurement exists in the codebase
	// yet, so this is estimated HIGH on purpose: an over-estimate only under-fills
	// the input budget (safe), while an under-estimate risks the exact overflow
	// this sprint fixes. ~4k tokens comfortably covers the wrapper. The separately
	// byte-capped sprint-plan SCOPE CONSTRAINT (internal/fanout buildSlots,
	// max_sprint_plan_bytes) is NOT part of this wrapper and is not counted here.
	promptOverheadTokens = 4096

	// avgBytesPerLine converts an effective input BYTE budget into a chunker
	// maxLines figure (F3). Chosen conservatively so the smallest roster window
	// (defaultContextWindowTokens = 32768) lands near the existing
	// DefaultMaxContextLines = 1500 line-budget anchor as a sanity cross-check:
	// 32768 - 8192 output - 4096 overhead = 20480 tokens ≈ 71680 bytes, and
	// 71680 / 48 ≈ 1493 ≈ 1500. A larger bytes/line yields fewer lines (more,
	// smaller chunks), which is safe under the Conservatism NFR.
	avgBytesPerLine = 48

	// minChunkLines is a positive floor for ChunkMaxLines so a pathologically
	// small resolved window never yields maxLines <= 0 — the value chunkDiff
	// treats as "disable chunking" (internal/fanout/chunker.go), the opposite of
	// what an overflow-prone tiny window needs.
	minChunkLines = 64
)

// EffectiveByteBudget returns the byte budget a model's payload must fit within
// so that estimated input tokens ≤ ContextWindowTokens(model, declared) -
// outputTokens - promptOverheadTokens. declared is the agent's own
// context_window_tokens (nil when it made no declaration) and is resolved ahead
// of the static table — see ContextWindowTokens for the tier order. It converts
// the reserved token budget to bytes using the
// conservative ~3.5 B/token ratio (rounding DOWN, so the byte budget never
// overshoots the token reservation). Returns 0 ("no budget available") when the
// reservation leaves zero or negative input tokens for the model — never a
// negative byte count.
//
// This closes the confirmed dax boundary overflow: for a 32768-token window with
// outputTokens = 8192, the reserved input tokens are strictly below 32768 - 8192
// = 24576, so the 24577 input + 8192 output > 32768 class cannot recur (F2/AC2).
//
// The 0 return is reachable for the current callers (which pass defaultMaxTokens
// 8192) when the resolved window is at or below outputTokens + promptOverheadTokens
// (12288 at the defaults). Registry validation permits declarations down to 1
// token, so an explicit declaration in that band now drives this path — but the
// function itself has always returned 0 for large enough outputTokens regardless
// of the declaration tier (EffectiveByteBudget(model, nil, 28672) returned 0 on
// the pre-epic 32768 floor too). Callers handle 0 as honest degradation rather
// than treating it as unreachable defense-in-depth (see the bulk path in
// internal/fanout).
func EffectiveByteBudget(model string, declared *int, outputTokens int) int64 {
	if outputTokens < 0 {
		outputTokens = 0
	}
	effectiveTokens := ContextWindowTokens(model, declared) - outputTokens - promptOverheadTokens
	if effectiveTokens <= 0 {
		return 0
	}
	return int64(effectiveTokens) * conservativeBytesPerTokenNum / conservativeBytesPerTokenDen
}

// ChunkMaxLines converts a model's effective input budget into a per-chunk diff
// line count for the Epic 14.3 chunker (chunkDiff). A small-window model gets a
// smaller maxLines (more, smaller chunks) and a large-window model a larger
// maxLines (fewer chunks) from the SAME diff, delivering the whole diff with zero
// files dropped (F3/AC3). The result is clamped to minChunkLines so a
// pathologically small window never returns a non-positive value that chunkDiff
// would read as "disable chunking".
//
// It consumes EffectiveByteBudget directly rather than introducing a parallel
// budget representation, so the same conservative ratio and output reservation
// govern both the shed-to-fit (bulk) and chunk-to-fit paths — and so a declared
// window (Epic 35.16.5.1) reaches the per-chunk line budget through exactly one
// resolution chain. declared is the agent's own context_window_tokens, nil when
// it made no declaration.
//
// NOTE ChunkMaxLines raises the per-chunk LINE budget with no direct
// payload_byte_budget clamp, but the entries fed to chunkDiff have already
// passed through ApplyByteBudgetPreferEscalated (review.go), so the chunk BYTES
// are still bounded by the global cap — the practical effect of a large
// declaration is fewer, larger chunks, never a chunk larger than
// payload_byte_budget. The unbounded case is payload_byte_budget: 0 (no global
// cap configured).
func ChunkMaxLines(model string, declared *int, outputTokens int) int {
	maxLines := int(EffectiveByteBudget(model, declared, outputTokens) / avgBytesPerLine)
	if maxLines < minChunkLines {
		return minChunkLines
	}
	return maxLines
}
