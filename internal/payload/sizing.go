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
	// DefaultOutputTokens is the output-token cap `atcr review` applies to a
	// reviewer call that resolves no other value (the LAST tier of
	// resolveMaxTokens: --max-tokens > the agent's max_tokens declaration >
	// this). It is THE single source for that default: internal/fanout's
	// defaultMaxTokens, internal/doctor's reviewDefaultMaxTokens and the
	// cli/doctor.go --max-tokens flag default all reference it, so the three
	// cannot drift. Generous on purpose: reasoning/thinking models spend output
	// budget on chain-of-thought before emitting visible content, so a tight cap
	// makes them finish mid-reasoning and return an empty review.
	DefaultOutputTokens = 8192

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
// The 0 return is reachable for the current callers when the resolved window is at
// or below outputTokens + promptOverheadTokens. Callers now pass a RESOLVED per-agent
// output cap (--max-tokens flag → the agent's max_tokens declaration → the built-in
// 8192), which registry validation permits anywhere in 1..MaxTokensCap — so the
// threshold moves with that cap rather than sitting at the fixed 12288 the embedded
// default produced. Registry validation permits window declarations down to 1
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

// InputRoomTokens reports how many tokens a model's resolved window leaves for
// INPUT once the fixed prompt overhead is removed, before any output reservation
// is taken out of it. Returns 0 when the window is at or below the prompt
// overhead — the window has no input room at all, never a negative number.
//
// It exists so a caller can size a reservation against what the window can
// actually fund — "reserve the output cap, but never more than half the input
// room" is a policy that needs the room as a number, and the only alternative is
// re-stating promptOverheadTokens outside this package. EffectiveByteBudget
// subtracts the same constant, so a reservation derived from this function and
// the budget derived from that one cannot disagree about the overhead. Its first
// caller is internal/verify's skeptic tool ceiling.
func InputRoomTokens(model string, declared *int) int {
	room := ContextWindowTokens(model, declared) - promptOverheadTokens
	if room < 0 {
		return 0
	}
	return room
}

// MinUsableReadBytes is the smallest tool-output budget a window must be able to
// fund for a tool-driven lane to read anything conclusive: one maximum-size tool
// result. Below it a lane is not reading a file, it is reading a fragment of one.
//
// It MIRRORS tools.DefaultMaxResultBytes, which this package cannot import —
// internal/tools sits above internal/payload in the dependency direction
// enforced by internal/boundaries_test.go, and inverting that for one constant
// would drag the dispatcher and its sandbox backend underneath the sizing layer.
// internal/verify's TestMinTrustworthyCeilingMirrorsTheDispatcherCap pins the
// two literals together, so the duplication cannot drift silently.
const MinUsableReadBytes int64 = 64 * 1024

// WindowFundsAUsableRead reports whether a window can derive a tool-output
// ceiling of at least MinUsableReadBytes under the half-room reservation policy
// — the same policy internal/verify's skepticToolBudget applies, expressed here
// so a pre-flight diagnostic can ask the question without importing the lane
// that answers it at run time.
//
// outputTokens is the agent's own max_tokens declaration, or nil for the
// built-in DefaultOutputTokens; passing the same value the lane resolves is what
// keeps a warning about a config from describing a different config than the one
// the run will use.
func WindowFundsAUsableRead(model string, declared *int, outputTokens *int) bool {
	outCap := DefaultOutputTokens
	if outputTokens != nil && *outputTokens > 0 {
		outCap = *outputTokens
	}
	reserved := min(outCap, InputRoomTokens(model, declared)/2)
	return EffectiveByteBudget(model, declared, reserved) >= MinUsableReadBytes
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
// NOTE ChunkMaxLines itself derives the LINE budget from the window alone; the
// operator's chunk_byte_budget is applied on top of it by ClampLinesToByteBudget
// at the fan-out call site (settings are not this function's to know). That key
// is distinct from payload_byte_budget precisely so this clamp and the global
// file-shedding pass can be tuned independently; unset, it inherits the payload
// budget, which is the coupling that used to be unavoidable. Before
// that clamp existed, a legal 10,000,000-token declaration derived ~728,000
// lines (~34.9 MB) per chunk — the chunk BYTES were bounded only indirectly, by
// the entries having passed through ApplyByteBudgetPreferEscalated in review.go.
// With the clamp the practical effect of a large declaration stays "fewer,
// larger chunks", and the still-unbounded case is payload_byte_budget: 0 (no
// global cap configured), where the operator has declined to set a ceiling.
// ClampLinesToByteBudget narrows a per-chunk line budget to the lines that fit
// within byteBudget, using the same avgBytesPerLine ratio ChunkMaxLines derived
// it with. A byteBudget of 0 means "no cap configured" (the settings-tier
// convention everywhere else) and passes maxLines through untouched.
//
// This is the line-budget twin of fanout's appliedByteBudget: the per-model
// derivation belongs to the model, but the global payload_byte_budget is a
// settings-tier ceiling, so it is applied at the call site where settings are
// known rather than threaded into ChunkMaxLines.
//
// It exists because ContextWindowTokensCap admits a 10,000,000-token
// declaration, which derives ~728,000 lines (~34.9 MB) per chunk — past any
// real proxy request-body limit. chunk_byte_budget is the operator's own
// statement of how many bytes may ride one call, so honoring it here keeps a
// large declaration meaning "fewer, larger chunks" rather than "one chunk no
// endpoint will accept".
//
// The result is floored at minChunkLines for the same reason ChunkMaxLines is:
// chunkDiff reads a non-positive maxLines as "disable chunking", the exact
// opposite of what a tight budget needs.
func ClampLinesToByteBudget(maxLines int, byteBudget int64) int {
	if byteBudget <= 0 || maxLines <= 0 {
		return maxLines
	}
	allowed := int(byteBudget / avgBytesPerLine)
	if allowed >= maxLines {
		return maxLines
	}
	if allowed < minChunkLines {
		return minChunkLines
	}
	return allowed
}

func ChunkMaxLines(model string, declared *int, outputTokens int) int {
	maxLines := int(EffectiveByteBudget(model, declared, outputTokens) / avgBytesPerLine)
	if maxLines < minChunkLines {
		return minChunkLines
	}
	return maxLines
}
