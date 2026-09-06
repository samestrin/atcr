package reconcile

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxAnchorsPerFinding bounds how many anchors one finding contributes to a
// Tier 4 lookup. A finding naming more identifiers than this is almost
// certainly prose about a whole subsystem rather than a citation of one
// construct, and searching every token would trade a precise lookup for an
// index sweep. Truncation is applied AFTER the sort, so it drops the lexically-
// last anchors deterministically rather than whichever ones the scanner
// happened to reach first (AC2).
const maxAnchorsPerFinding = 8

// minAnchorLen is the shortest token accepted as an anchor. Two-character
// identifiers (id, ok, fn) are too common across a tree to localize a finding,
// and a spurious single match on one would produce a confident wrong
// PathSuggestion — the exact failure Tier 1-3 were tuned to avoid.
const minAnchorLen = 3

// extractAnchorSet is the Tier 4 (Epic 35.16.6.5 T1) deterministic anchor
// extractor: given ONE body of finding prose it returns the identifier-shaped
// tokens that text appears to be talking about, which the repo-wide symbol index
// (T2) is then searched for.
//
// PROBLEM and FIX are extracted SEPARATELY and are not interchangeable. A FIX
// routinely names a construct the reviewer wants CREATED ("extract the retry
// loop into `splitRetryHelper`"), which is absent from the tree by definition —
// so a FIX anchor can never be evidence that a finding is fabricated. Only
// PROBLEM anchors may authorize a no-match verdict; FIX anchors may still
// contribute a resolution when they name existing code. Merging them into one
// set, as an earlier revision did, made the proposed remedy's own name the
// evidence for discarding the finding that proposed it.
//
// truncated reports that the returned set is not a faithful reading of what the
// text named — either more identifiers were named than maxAnchorsPerFinding
// admits, or the call scan lost fidelity on a span (see collectCallAnchors). A
// caller must never reach a no-match verdict on a truncated set: the one anchor
// that would have matched may be among the ones it did not faithfully recover.
//
// Read that as a claim about those two losses ONLY, never as "truncated=false
// means the set is faithful". One unfaithful reading is known to return FALSE:
// a mixed name with no underscore truncates to its Latin tail and can be
// CONFIDENTLY misattributed to another declaration of that tail — measured,
// `使用ParseConfig() here` yields anchors=[ParseConfig] truncated=false. That
// case is disclosed at isWordBoundary's doc (the `ตัวแปรParseConfig` paragraph)
// and is tracked as follow-on work; it is deliberately NOT folded in here,
// because the two losses above are ones the scan detects as it makes them and
// this one is not detected at all. A caller that needs "the set is faithful"
// rather than "these two losses did not occur" does not have it from this flag.
//
// A second unfaithful reading is also known to return FALSE, and this one is
// ACCEPTED rather than tracked: a qualifier between same-script prose and the
// name. `設定._解析()` records `_解析` with truncated=false — trailingSegment
// strips `設定.`, and because 設定 and 解析 share a script no word boundary
// fires, so the leading-underscore suppression in collectCallAnchors is never
// consulted. `_解析` is exactly the confidently-misattributable fragment that
// suppression exists to stop, reached by a third mechanism (a qualifier
// between same-script prose and the name) rather than by a boundary. Closing
// it means widening the boundary rule, which the epic's own scope rules out.
//
// The flag deliberately covers BOTH losses through one channel. The cap drops
// whole anchors; the call scan can instead return a token the reviewer did not
// write (spaceless prose glued to a call name) or return nothing for a span it
// could not decide. All three make "searched everything the text named and found
// none of it" a false statement, which is the only claim `truncated` exists to
// block. Splitting them into separate flags would give validate.go three
// conditions to get right instead of one, and the two newer losses reached
// production precisely because they had no channel at all.
//
// It is a pure function of its inputs — no model call, no filesystem, no clock,
// no map-iteration order — so the same finding text always yields the same
// anchor set (AC2). A summarizing or interpreting pass here is exactly where a
// fabricated finding could be paraphrased into something that matches, which is
// why the extraction path is deliberately mechanical (mirroring 35.16.7's
// claim-ledger precedent).
//
// Three span kinds are scanned, in the order a reviewer is most likely to have
// marked an identifier deliberately:
//
//   - backtick spans   — `foo`, the conventional code span in reviewer prose
//   - quoted spans     — "foo" and 'foo'
//   - call shapes      — foo(, an identifier immediately followed by an open paren
//
// Each raw span is reduced to its trailing segment (stream.ValidatePath ->
// ValidatePath) because the symbol index keys on the declared name, not the
// qualified call site. The result is kept only if it is identifier-shaped AND
// carries an identifier signal (see hasIdentifierSignal): prose in quotes ("file
// not found") and bare lowercase English words (`handler`) are rejected, so a
// finding with no identifier-shaped text yields ZERO anchors rather than a noisy
// guess. Returning nothing is the correct answer there — the caller treats "no
// anchors" as "could not check", never as "checked and found nothing".
//
// The returned slice is deduped and lexically sorted; nil when nothing
// qualifies.
func extractAnchorSet(text string) (anchors []string, truncated bool) {
	anchors, s := scanProblemAnchors(text)
	return anchors, s.truncated()
}

// truncated is the flat flag extractAnchorSet returns, as ONE definition. Both
// the wrapper and validate.go need it, and spelling `capped || lostSpan` twice
// is how the two would drift when a third loss is added.
func (s anchorScan) truncated() bool {
	return s.capped || s.lostSpan
}

// scanProblemAnchors is extractAnchorSet with the scan's fidelity-loss detail
// preserved, the PROBLEM-side sibling of scanFixAnchors.
//
// The PROBLEM set is never narrowed — a glued member may still be the subject,
// and dropping it would manufacture the no-match verdict this tier exists to
// withhold — so the anchors it returns are exactly extractAnchorSet's. What the
// scan carries that the flat flag cannot is `unaccounted`: a loss with NO member
// to point at, whose name is unknowable. locate() refuses when two precise
// anchors DISAGREE, so its verdict rests on the set being COMPLETE as well as
// faithful, and a silenced span is exactly the member whose answer is unknown.
// validate.go must therefore be able to tell that loss apart from the cap, which
// `truncated` folds it in with.
func scanProblemAnchors(text string) ([]string, anchorScan) {
	s := scanAnchors(text)
	return s.anchors, s
}

// anchorScan is one extraction's full result, kept unflattened for the one
// consumer that must tell the two losses apart.
//
// `truncated` is deliberately ONE flag on the SET, and stays that way: the
// no-match direction reads it and must get exactly one condition right (see
// extractAnchorSet's doc). This struct does not add a second flag beside it —
// it records WHICH anchors a fidelity loss touched, which is a different
// question and the only one a per-anchor repair can be built on.
type anchorScan struct {
	// anchors is the deduped, sorted, capped set - what extractAnchorSet returns.
	anchors []string
	// capped reports that maxAnchorsPerFinding dropped whole anchors. The set is
	// a PREFIX of what the text named and the dropped members are unknowable.
	capped bool
	// lostSpan reports that the call scan lost fidelity on at least one span,
	// whether or not that span contributed an anchor (a silenced span
	// contributes none).
	lostSpan bool
	// unaccounted reports that at least one such loss left NO member behind, so
	// it cannot be repaired by dropping one, AND no clean span in the same text
	// vouched for the token that loss would have named, AND that token survives
	// the anchor cap into `anchors` — the post-cap set locate is actually given.
	// What that span would have named is then unknowable, exactly as the cap's
	// dropped anchors are.
	//
	// The second clause is what separates it from lostSpan. A silenced span
	// whose destroyed name is cited faithfully two words earlier lost fidelity
	// (lostSpan) but lost nothing UNKNOWABLE (not unaccounted) — the name is
	// sitting in `anchors`. scanAnchors reconciles the two the same way, and in
	// the same loop, that it reconciles `imprecise` against `clean`.
	unaccounted bool
	// imprecise holds the tokens a glued span contributed AND no clean span (a
	// delimited citation or an unglued call) did. Every token in it may
	// be an unfaithful reading of what the reviewer wrote; every member of
	// anchors NOT in it is faithful WITH RESPECT TO THE LOSSES THIS SCAN DETECTS
	// — the undetected mixed-no-underscore Latin-tail reading disclosed at
	// extractAnchorSet's doc is not covered by that claim. It is keyed on what
	// the scan recorded, not on the post-cap slice, so it can name a token the
	// cap later dropped — harmless, since it is only ever consulted as a set to
	// exclude.
	imprecise map[string]struct{}
}

func scanAnchors(text string) anchorScan {
	seen := make(map[string]struct{})
	clean := make(map[string]struct{})
	for _, d := range anchorDelimiters {
		collectDelimitedAnchors(text, byte(d), seen, clean)
	}
	imprecise := make(map[string]struct{})
	silenced := make(map[string]struct{})
	lostSpan := collectCallAnchors(text, seen, clean, imprecise, silenced)
	// A token a clean span also contributed was read faithfully at least once,
	// so the glued reading is not the only evidence for it: it is not
	// imprecise. Only a token whose EVERY contribution came from a glued span
	// stays in the set.
	//
	// `silenced` is reconciled by the SAME loop and for the same reason, one
	// step further: `unaccounted` claims the destroyed name is UNKNOWABLE, and a
	// clean citation of that exact token is the evidence that refutes it — the
	// name is not unknowable, it is sitting in `anchors`. Only a token nothing
	// vouched for keeps the claim.
	//
	// Both reconciliations happen HERE, after the call scan, and not inline
	// where the loss is detected: `clean` is still being filled while
	// collectCallAnchors runs (an unglued call contributes to it too), so an
	// inline decision would depend on whether the clean citation happened to sit
	// before or after the silenced span in the text.
	for tok := range clean {
		delete(imprecise, tok)
	}
	// lostSpan is deliberately NOT reconciled. `truncated` is built from it and
	// the no-match direction reads that, so clearing it would make tier4NoMatch
	// reachable where it was not before. The span really did lose fidelity; what
	// is retracted is only the claim that what it lost is unknowable.
	scan := anchorScan{lostSpan: lostSpan, imprecise: imprecise}
	if len(seen) == 0 {
		// No anchor survived at all, so nothing can vouch for anything: the
		// reconciliation loop below can never decrement, and every recorded loss
		// keeps its claim. Both producers of the verdict still route through
		// reconcileSilenced, so the predicate has exactly one definition.
		scan.unaccounted = reconcileSilenced(silenced, clean, scan.anchors)
		return scan
	}
	out := make([]string, 0, len(seen))
	for tok := range seen {
		out = append(out, tok)
	}
	sort.Strings(out)
	if len(out) > maxAnchorsPerFinding {
		scan.anchors, scan.capped = out[:maxAnchorsPerFinding], true
	} else {
		scan.anchors = out
	}
	scan.unaccounted = reconcileSilenced(silenced, clean, scan.anchors)
	return scan
}

// reconcileSilenced decides which recorded losses keep their unaccounted claim,
// and reports whether any survives.
//
// The `clean` conjunct is currently DEFENSIVE, not behaviourally reachable:
// separating it from the anchors conjunct needs a token that is at once a
// silence subject and contributed only by a glued (never clean) span, and the
// boundary rules make that shape unreachable today — see
// TestReconcileSilenced_BothConditionsAreRequired's own doc, which pins the
// conjunct's MEANING precisely because the unit test is not a behavioural pin.
// It stays because membership in `anchors` says a token was collected while
// `clean` says it was read faithfully, and only the second is evidence about
// what the reviewer wrote: if the boundary rules ever widen, a glued
// mis-reading would otherwise start vouching for the very loss it is an
// instance of.
//
// A loss is retracted only when the token it destroyed was BOTH cited cleanly
// (`clean`) and survives into `anchors` — the post-cap set `locate` is actually
// given. Both halves are load-bearing and neither implies the other:
//
//   - Without the `clean` half, a glued or silenced re-reading of the name would
//     vouch for the very loss it is an instance of.
//   - Without the `anchors` half, the CAP breaks the argument. maxAnchorsPerFinding
//     slices a sorted set, so a name cited perfectly in backticks can be dropped
//     from the set anyway — and then `unaccounted` would be cleared for a token
//     locate never sees. The completeness check exists precisely so a set missing
//     a member cannot stamp a suggestion sourced from the survivors alone;
//     clearing it there converts a withheld suggestion into a WRONG one, which
//     this package rules worse than no suggestion at all.
//
// Reconciling against the post-cap set makes the retraction mean what its own
// justification says: the destroyed name is not unknowable, because it is
// sitting in the set locate will read.
func reconcileSilenced(silenced, clean map[string]struct{}, anchors []string) bool {
	if len(silenced) == 0 {
		return false
	}
	remaining := len(silenced)
	for _, tok := range anchors {
		if _, vouched := clean[tok]; !vouched {
			continue
		}
		if _, lost := silenced[tok]; lost {
			remaining--
		}
	}
	return remaining > 0
}

// extractFixAnchors returns the FIX anchors that may ground a PathSuggestion.
//
// The FIX set feeds resolve's SECONDARY anchors, which may only LOCALIZE a
// finding whose subject already matched somewhere in the tree — never route one
// out (resolve's primaryMatched guard). So the two losses extractAnchorSet folds into
// `truncated` cost different things here and may not be answered alike:
//
//   - The CAP is a PREFIX. Which anchors it dropped is unknowable, so no
//     statement about the remainder is safe and the set is abandoned whole.
//     This is the case the flat `if truncated` test was written for.
//
//   - A call-scan fidelity loss that CONTRIBUTED a member is per-SPAN. That
//     member may not be what the reviewer wrote, but every other member still
//     is — so exactly those members are dropped from the USABLE set and the
//     rest stand. Dropped is not discarded: see the completeness note below.
//
//   - A call-scan fidelity loss that contributed NO member (a silenced span, or
//     a glued span whose token failed the shape or signal test) has the cap's
//     standing, not the glued one's, and abandons the set whole.
//
// That last case is the one worth stating plainly, because "it contributed
// nothing, so it drops nothing" is a tempting and WRONG reading of it. Every
// anchor still present is indeed faithful — but locate() does not only ask
// whether the members present are faithful. It refuses to answer when two
// precise anchors DISAGREE, so its verdict depends on the set being complete as
// well as faithful, and the span that was silenced is exactly the one whose
// answer is unknowable. Measured: a FIX of “調用_ParseConfig() then `parseTree`
// “ with _ParseConfig and parseTree declared in different files yields
// "pkg/tree.go" if the silence is ignored, where the faithful set would have
// refused on disagreement — precisely the "a wrong guess that suggests the
// wrong file is worse than no suggestion" rule resolve states for itself.
//
// Where a member IS dropped, dropping is the safe direction and nilling never
// was. Losing a secondary anchor can only cost a suggestion the finding would
// otherwise have carried; it can never move an outcome toward tier4NoMatch,
// because the secondary locate is unreachable unless a primary anchor already
// matched. Measured: a FIX naming one genuine Japanese snake_case call
// alongside a precise ASCII anchor went tier4Resolved -> tier4Inconclusive
// under the flat test, losing a correct PathSuggestion to a loss that never
// touched the anchor that produced it.
//
// A dropped member is dropped from what may SOURCE a suggestion, not from what
// the FIX named. The completeness argument the member-less case rests on cuts
// here too — locate() refuses when two precise anchors DISAGREE, and a dropped
// name is just as absent from that comparison as a silenced one — so the
// dropped members ride along as VETO evidence (droppedFixAnchors, consumed by
// symbolIndex.resolve). Measured: a FIX of "call `parseTree` instead of
// データ_解析()" with the two names declared in DIFFERENT files stamped
// "pkg/tree.go" while the members were merely discarded, where the complete set
// refuses. Vetoing costs a suggestion and can never route a finding out, so
// this stays the safe direction while closing the gap.
//
// Splitting it that way — dropped may not vote, may still veto — is what keeps
// the per-anchor drop rather than abandoning the set whole the way the cap and
// the member-less case do, which is the recorded decision for this seam.
//
// An anchor contributed by BOTH a glued span and a clean one (a backticked or
// quoted citation, or an unglued call of the same name) is KEPT: the clean
// contribution is proof the scan read the name faithfully at least once, so the
// glued reading is not the only evidence for it. Only a token whose every
// contribution came from a glued span is dropped.
func extractFixAnchors(text string) []string {
	anchors, _ := scanFixAnchors(text)
	return anchors
}

// scanFixAnchors is extractFixAnchors with the scan's fidelity-loss detail
// preserved, for the one caller (validate.go) that must report WHICH loss
// fired - cap, member-less, or per-anchor drop - rather than only narrow the
// set.
func scanFixAnchors(text string) ([]string, anchorScan) {
	s := scanAnchors(text)
	if s.capped || s.unaccounted {
		return nil, s
	}
	if len(s.imprecise) == 0 {
		return s.anchors, s
	}
	out := make([]string, 0, len(s.anchors))
	for _, tok := range s.anchors {
		if _, bad := s.imprecise[tok]; !bad {
			out = append(out, tok)
		}
	}
	if len(out) == 0 {
		return nil, s
	}
	return out, s
}

// droppedFixAnchors returns the members scanFixAnchors narrowed out of the
// usable set: the anchors every contribution of which came from a glued span.
// Sorted (it walks the already-sorted anchors), nil when none.
//
// A dropped member may not SOURCE a suggestion — the glued reading may not be
// what the reviewer wrote, which is why it leaves the usable set. It is still
// part of what the FIX named, so it may still CONTRADICT one: see resolve's
// droppedSecondary argument. Discarding it outright left the set incomplete in
// exactly the way the `unaccounted` arm nils the whole set to avoid.
//
// It reports nil on the two arms scanFixAnchors abandons the set for (capped,
// unaccounted), mirroring that early return: nothing was narrowed away there,
// so there is no per-anchor drop, and there is no located file for a dropped
// name to contradict. It also intersects with the POST-cap anchors, since
// imprecise is keyed on what the scan recorded and can name a token the cap
// removed.
func (s anchorScan) droppedFixAnchors() []string {
	if s.capped || s.unaccounted || len(s.imprecise) == 0 {
		return nil
	}
	var out []string
	for _, tok := range s.anchors {
		if _, bad := s.imprecise[tok]; bad {
			out = append(out, tok)
		}
	}
	return out
}

// anchorDelimiters are the paired characters a reviewer uses to mark a literal
// identifier in prose. Each is its own closer, and each is scanned in its OWN
// pass over the text (see extractAnchors) rather than in one interleaved pass.
//
// The separate passes are load-bearing, not stylistic. Apostrophes are pervasive
// in ordinary review prose ("the parser's cache", "doesn't handle nil"), so a
// single interleaved scan mis-pairs them constantly, and each mis-paired span
// swallows every delimiter it spans — including the backtick spans that carry
// the real identifiers. Per-delimiter passes contain that damage to the quote
// pass alone: a mis-paired span there always contains whitespace, so it fails
// isIdentifierShaped and contributes nothing.
const anchorDelimiters = "`\"'"

// collectDelimitedAnchors scans text for spans delimited by d and records the
// qualifying anchor from each. It stops at an opener with no closer after it,
// because no further pair of d can exist past that point.
//
// That early stop is contained to THIS delimiter's pass. A lone apostrophe
// mid-sentence ends only the apostrophe pass; the backtick pass over the same
// text is unaffected and still finds the identifiers after it. That containment
// is the whole reason extractAnchorSet runs one pass per delimiter.
func collectDelimitedAnchors(text string, d byte, seen, clean map[string]struct{}) {
	for i := 0; i < len(text); i++ {
		if text[i] != d {
			continue
		}
		close := strings.IndexByte(text[i+1:], d)
		if close < 0 {
			return // no closer remains anywhere after i: nothing left to pair
		}
		if tok := recordedAnchorForm(text[i+1 : i+1+close]); recordAnchor(tok, seen) {
			clean[tok] = struct{}{} // a delimited span is a faithful contribution
		}
		i += close + 1 // resume after the closer, never inside the span
	}
}

// collectCallAnchors scans text for an identifier immediately followed by '(',
// the call shape a reviewer writes when naming a function without marking it up
// (BuildFileIndex() is called once per finding). The identifier run is read
// backwards from the paren, so a qualified call (x.Parse() ) still yields its
// trailing segment via addAnchor.
//
// The run also terminates at a spaceless/spacing WORD BOUNDARY, because
// isQualifiedIdentRune alone does not terminate at a word boundary in a script
// that writes without inter-word spaces. `在配置中调用ParseConfig()` would
// otherwise yield the pseudo-token `在配置中调用ParseConfig` — identifier-shaped,
// carrying a signal from the interior e->C transition, and declared nowhere —
// which resolve reads as tier4NoMatch, deleting a real finding and durably
// charging the reviewer a phantom.
//
// The boundary is narrow on purpose: see isWordBoundary for why two different
// spaceless scripts do NOT break the run (`データ_解析` is one name), and why an
// underscore sitting on the boundary that does fire yields no anchor at all
// rather than a fragment.
//
// lostSpan reports that at least one span was NOT faithfully recovered, which
// extractAnchorSet folds into its `truncated` return so validate.go cannot reach
// a no-match verdict on the set. Two spans set it, and both are the SAME
// undecidability seen from different sides:
//
//   - SILENCED — the underscore-on-the-boundary case above, which contributes no
//     anchor. The silence keeps a misattributable fragment out of locate, but it
//     is per-SPAN while its safety argument ("no anchor keeps the finding") is
//     per-FINDING. With a co-cited anchor that is absent from the tree, silencing
//     the one span that would have matched flips the whole finding to no-match —
//     measured: a PROBLEM naming both `設定_loadFile()` and a co-cited absent
//     `retryOnce` went tier4Resolved -> tier4NoMatch.
//
//   - GLUED — a run that crossed between two spaceless scripts and ended up
//     holding an underscore. Because such a crossing does not break the run, the
//     accepted span is either one snake_case name (`データ_解析`) or prose welded
//     to one (`設定を解析_処理`, where the tree declares `解析_処理`), and nothing
//     in the text separates them. The anchor is still contributed — dropping it
//     would take the `データ_解析` direction back out — but the set is marked
//     imprecise, so the glued reading can no longer DELETE a finding while the
//     genuine reading can still resolve one.
//
// The asymmetry is deliberate: silence is right where a fragment could be
// confidently misattributed, and an imprecise anchor is right where the token may
// well be the real name. What neither may do is stand as proof that the tree was
// searched for what the reviewer actually wrote.
//
// lostSpan reports a per-SPAN loss and is what `truncated` is built from.
//
// silencedInto receives, for each loss that left NO member behind, the token
// that loss would have named — the full run's trailing segment in its recorded
// form. It is a RECORD, not a verdict: this function cannot decide whether the
// name is unknowable, because `clean` is still being filled while it runs.
// scanAnchors subtracts `clean` from it afterwards and derives `unaccounted`
// from what survives. Both producers of the claim write here, so the
// reconciliation cannot close one path and leave the other short.
//
// impreciseInto receives the subset of `seen` that a glued span contributed,
// so a consumer that can repair per-anchor has the names and one that cannot
// still has the flag. clean receives every token a span contributed FAITHFULLY
// (an unglued call; the delimited scan marks its own), so scanAnchors can keep
// a name that was read cleanly at least once out of the imprecise set.
//
// unaccounted reports that at least one loss left NO member behind — a silenced
// span whose fragment could have qualified (or carries a combining mark, proof
// the break landed mid-word), a silenced span whose FULL run could have
// qualified where the break dropped a spaceless-script prefix, or a glued span
// whose token failed the shape or signal test. A silence where neither the
// fragment nor (where it is consulted) the full run could EVER have qualified
// set nothing here: it lost nothing. The distinction matters to extractFixAnchors
// and nowhere else: a loss with a member can be repaired by dropping that
// member, and a loss without one cannot be repaired at all, because what the
// span would have named is unknowable.
//
// Disclosed cost of the full-run question: the same unaccounted=true flows
// through scanAnchors into scanFixAnchors, which abandons the FIX anchor set
// WHOLE on it. So for a spaceless-prefix short tail — bare or qualified — an
// intact, precise, ASCII sibling anchor is discarded along with the silenced
// one, and atcr_tier4_fix_set_unaccounted_total increments. Measured: a FIX of
// “call `parseTree` instead of 設定_a()“ yielded [parseTree] before the
// widening and nothing after, with the scan still seeing parseTree in both.
//
// The cost is narrower than it reads, and only this narrow: it is paid when
// NOTHING in the same text cites the destroyed name cleanly. scanAnchors
// reconciles the silence against `clean`, so “call `parseTree` instead of
// `設定_a` in 設定_a()“ keeps BOTH anchors and increments nothing — the name is
// in the set, so nothing about it is unknowable. The measured example above
// still pays it, because there 設定_a is named only by the span that lost it.
//
// That is the SAFE direction and is why it is disclosed rather than fixed here:
// an abandoned FIX set can only leave a suggestion unstamped, never route a
// finding out. locate(nil) fails, so the secondary branch cannot fire, and a
// matched primary anchor yields tier4Inconclusive ("could not check") rather
// than tier4NoMatch ("checked and found nothing"), which is the only outcome
// that sidecar-routes anything.
func collectCallAnchors(text string, seen, clean, impreciseInto, silencedInto map[string]struct{}) (lostSpan bool) {
	for i := 0; i < len(text); i++ {
		if text[i] != '(' {
			continue
		}
		start := i
		runScript := scriptUnset
		atBoundary := false
		boundaryDroppedSpaceless := false
		crossedSpaceless := false
		for start > 0 {
			r, size := utf8.DecodeLastRuneInString(text[:start])
			if !isQualifiedIdentRune(r) {
				break
			}
			if s := spacelessScriptOf(r); s != scriptNeutral {
				if isWordBoundary(runScript, s) {
					atBoundary = true
					// Which side the break DROPPED, which the silence guard
					// below needs and the fragment cannot report. isWordBoundary
					// fires only between a spaceless script and a spacing one,
					// so one side is always each; s is the side being left
					// behind, and a non-negative s indexes spacelessScripts.
					boundaryDroppedSpaceless = s >= 0
					break // the run has left the call name
				}
				if runScript != scriptUnset && s != runScript {
					crossedSpaceless = true
				}
				runScript = s
			}
			start -= size
		}
		if start == i {
			continue // "(" with no identifier before it
		}
		// Both underscore tests below read the RECORDED anchor, never the raw
		// span. The raw span is what the backwards run stopped on; the anchor is
		// what recordAnchor will actually key the tree search on, and the two
		// differ by exactly the runes that make these guards leak: a leading
		// script-neutral rune the break landed on (U+30FC, a combining mark) and
		// a qualifier trailingSegment strips. Measured against the raw span:
		// `parseー_解析()` escaped as the fragment `ー_解析`, `parse._解析()` as
		// `_解析`, and `データ_モジュール.解析()` was marked imprecise although its
		// only underscore lives in the stripped qualifier.
		//
		// The qualifier case NOT caught here: `設定._解析()` — the same shape as
		// `parse._解析()`, but 設定 and 解析 share a script, so no boundary
		// fires, atBoundary stays false, and `_解析` is recorded with the set
		// reported faithful. Accepted, not fixed: closing it means widening the
		// boundary rule, which the epic's scope rules out. Also disclosed at
		// extractAnchorSet's doc.
		anchor := recordedAnchorForm(text[start:i])
		if atBoundary && leadsWithUnderscore(anchor) {
			// Silence is a LOSS only when something could have been lost. A
			// fragment that could never have qualified (`_解` is two runes, a
			// digit leads) left nothing behind, so the set is still a faithful
			// read of every name the text could have contributed. The one
			// exception is a leading combining mark: a mark never starts a
			// word, so sitting at the break it proves the run stopped mid-word
			// — the full name may have qualified, and that IS a loss with no
			// member to point at.
			r, _ := utf8.DecodeRuneInString(anchor)
			fragmentQualified := isIdentifierShaped(anchor) && hasIdentifierSignal(anchor)
			lost := fragmentQualified || unicode.In(r, unicode.Mn, unicode.Mc)
			// The subject of the loss is the string the disjunct that FIRED
			// actually judged, which is not the same string for all three.
			// The fragment disjunct asks its question of `anchor` and accepts
			// it as a name in its own right, so `anchor` is what that loss
			// destroyed. The leading-combining-mark disjunct and the
			// boundaryDroppedSpaceless branch below both reason explicitly
			// about the FULL name the break truncated, so theirs is the full
			// run's trailing segment.
			//
			// Keying the record on one string for all three would record a
			// subject no predicate judged: measured, "`_解析` is broken;
			// parse_解析() fails" is silenced on the FRAGMENT `_解析`, which
			// the backticks cite and the anchor set already holds — recording
			// the full run `parse_解析` there would leave the flag standing
			// for a name the reviewer spelled out.
			destroyed := anchor
			if !fragmentQualified {
				destroyed = recordedAnchorForm(text[fullRunStart(text, start):i])
			}
			// The fragment is the right subject only when the fragment is what
			// the break could have cost. When the break DROPPED a spaceless-
			// script prefix, what was destroyed is the whole span name, and a
			// prefix split off a 1-2 rune tail leaves a fragment that fails
			// minAnchorLen while the full name is shaped, signalled and
			// perfectly searchable — `設定_a` behind the fragment `_a`. Ask the
			// question of the FULL run in that case: no boundary reduction,
			// because that reduction is exactly what threw the evidence away.
			//
			// trailingSegment IS applied, and must be. isQualifiedIdentRune
			// admits '.', so fullRunStart walks straight back through any
			// qualifier and hands isIdentifierShaped `pkg.設定_a`, which it
			// rejects on the '.' — disabling the guard for every qualified
			// spelling of the very shape it was added for. The declared name
			// the break destroyed is the trailing segment `設定_a`, so this
			// branch and the fragment branch above now ask their question of
			// the same reduction rather than of two different strings.
			//
			// The qualifier strip ONLY, deliberately not recordedAnchorForm:
			// this branch asks whether something COULD have qualified, and the
			// NFC fold is a normalization of a token that will be recorded, not
			// a reduction that decides shape. Adding it here would change the
			// rune count isIdentifierShaped's minAnchorLen reads on a composing
			// sequence, which is a separate measurement this repair has not
			// made.
			//
			// Deliberately NOT applied when the break dropped a SPACING-script
			// prefix (`parse_解`). There the boundary rule is reading its own
			// design case — a spaced-out Latin word running into a name — and
			// widening the guard to it would refuse a no-match verdict for a
			// set whose every member is a faithful reading.
			if !lost && boundaryDroppedSpaceless {
				full := trailingSegment(text[fullRunStart(text, start):i])
				lost = isIdentifierShaped(full) && hasIdentifierSignal(full)
			}
			if lost {
				lostSpan = true
				// silence: a loss with no member to point at. Record WHICH
				// token it would have named rather than declaring the loss
				// unaccounted here — scanAnchors reconciles the set against
				// `clean` once the scan is done, and only a token nothing
				// cited faithfully survives as unknowable.
				//
				// `destroyed` was chosen above to match whichever disjunct
				// fired. Both are in recordedAnchorForm, because `clean` is
				// keyed on that form — an NFD-spelled citation must be able to
				// vouch for its own NFC-spelled call. The fold is used ONLY as
				// a map key; the shape decisions above still read the unfolded
				// run, for the rune-count reason stated there.
				silencedInto[destroyed] = struct{}{}
			}
			continue // undecidable: see isWordBoundary
		}
		glued := crossedSpaceless && strings.Contains(anchor, "_")
		if glued {
			lostSpan = true // glued or genuine, and nothing here can tell
		}
		qualified := recordAnchor(anchor, seen)
		if qualified && !glued {
			clean[anchor] = struct{}{} // an unglued call is a faithful contribution
		}
		if glued {
			if qualified {
				impreciseInto[anchor] = struct{}{}
			} else {
				// The span lost fidelity and its token failed the shape or
				// signal test, so there is no member a per-anchor repair could
				// drop. Same standing as a silence, and recorded the same way.
				//
				// This is the SECOND producer of the unaccounted claim, and it
				// routes through the same set so the reconciliation cannot be a
				// partial predicate that closes the silence path and leaves
				// this one short. In practice it never reconciles away: a token
				// that failed recordAnchor can never have entered `clean`,
				// which only ever receives tokens recordAnchor accepted. That
				// is the correct outcome, not an oversight — an unshaped token
				// is not a name anything could have cited faithfully.
				silencedInto[anchor] = struct{}{}
			}
		}
	}
	return lostSpan
}

// fullRunStart continues the backwards identifier run from the index a word
// boundary stopped it at, ignoring boundaries, and returns where the whole run
// begins. It is how collectCallAnchors recovers the span name a break destroyed:
// the same walk the caller performs, minus the one rule under suspicion.
func fullRunStart(text string, start int) int {
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if !isQualifiedIdentRune(r) {
			break
		}
		start -= size
	}
	return start
}

// The three sentinel values spacelessScriptOf and the backwards run use
// alongside an index into spacelessScripts.
//
// scriptNeutral is the one that carries weight: a rune that says nothing about
// which script the run is in. It covers every non-letter the identifier class
// admits (digits, '_', '.', combining marks) AND letters whose script is
// Common, which is where U+30FC KATAKANA-HIRAGANA PROLONGED SOUND MARK lives.
// That mark is in most everyday Japanese loanword identifiers - データ,
// ユーザー, サーバー, ロード, パーサー - and classifying it as a script of its
// own would split `データ_解析` at its own vowel mark.
const (
	scriptUnset   = -3
	scriptNeutral = -2
	scriptSpacing = -1
)

// spacelessScripts are the scripts that do not put spaces between words, so
// ordinary prose in them runs straight into an embedded identifier with nothing
// between the two. They are the only scripts the backwards call scan can use as
// a boundary signal, and they are exactly the scripts that need one.
var spacelessScripts = []*unicode.RangeTable{
	unicode.Han,
	unicode.Hiragana,
	unicode.Katakana,
	unicode.Hangul,
	unicode.Thai,
	unicode.Lao,
	unicode.Khmer,
	unicode.Myanmar,
	unicode.Tibetan,
}

// spacelessScriptOf classifies r for the backwards run: an index into
// spacelessScripts, scriptSpacing for a letter from a script that separates
// words with spaces, or scriptNeutral for anything that carries no script
// signal at all.
//
// Neutral is not the same as "not a letter". Digits, '_', '.' and combining
// marks are neutral because they occur inside names in every script — but so
// is a Script=Common LETTER, and U+30FC (the katakana-hiragana prolonged sound
// mark) is one. Treating it as its own script splits `データ_解析` at the mark
// inside its own first word.
func spacelessScriptOf(r rune) int {
	if !unicode.IsLetter(r) || unicode.Is(unicode.Common, r) {
		return scriptNeutral
	}
	for i, t := range spacelessScripts {
		if unicode.Is(t, r) {
			return i
		}
	}
	return scriptSpacing
}

// isWordBoundary reports whether the run, currently in script `run`, has left
// the call name on reaching a rune of script `next` (both already classified by
// spacelessScriptOf, neither scriptNeutral).
//
// The rule is deliberately NOT "stop at any script change", and it is not "stop
// at any spaceless-script change" either. It fires only where the two sides
// CANNOT be one word: between a spaceless script and a space-separating one.
//
//   - A space-separating script beside Latin is one name. `नाम_load()` is a
//     single identifier, and truncating it to `_load` sends validate.go's
//     PathSuggestion at whatever else declares that fragment.
//
//   - Two DIFFERENT spaceless scripts are also one name, far more often than
//     they are a boundary. Japanese identifiers mix Han and Kana routinely —
//     `データ_解析`, `サバ_接続`, `解析_データ` — and breaking between them
//     leaves a fragment that still carries a signal through its underscore.
//     Ordinary Japanese PROSE glued to such a name (`設定を解析()`) is caught
//     anyway, but ONLY while the glued run stays caseless and underscore-free:
//     hasIdentifierSignal then rejects it and no anchor is produced. Measured,
//     not assumed — `設定を解析()` yields no anchor.
//
//     Where the name carries an underscore the exemption fails: `設定を解析_処理()`
//     yields the glued `設定を解析_処理`. That is the same undecidability as
//     below, one script-pair over — the run is either prose plus `解析_処理` or
//     the single name `設定を解析_処理`, and no rule separates them.
//
//     It is NOT silenced, because the silencing test below (a leading '_')
//     cannot fire when no break occurred. collectCallAnchors instead marks the
//     EXTRACTION imprecise, which is what keeps the glued reading from deleting
//     a finding while the genuine reading can still resolve one. Before that
//     signal existed the glued token reached resolve as ordinary evidence and
//     returned tier4NoMatch.
//
//     An earlier revision of this paragraph called that outcome "no worse than
//     before this rule existed, where the same input produced the fragment
//     `解析_処理` — a fragment can additionally be misattributed, which the
//     glued form cannot." That comparison was measured and is FALSE. It reaches
//     past the rule's own parent to the pre-rune ASCII scan. Against the parent,
//     `解析_処理` was not a fragment at all: collectSourceIdentifiers treats '_'
//     as a word byte, so `func 解析_処理(` harvests exactly that one token, and
//     the parent resolved it to the correct file. The glued form replaced the
//     BEST outcome with the worst one. Do not restate a comparison here without
//     running it.
//
// What remains undecidable is an underscore straddling the one boundary this
// does fire on: `调用_ParseConfig()` is either prose glued onto `_ParseConfig`
// or the tail of the single name `调用_ParseConfig`, and `設定_loadFile()` is
// either prose or one mixed Han-Latin name. Nothing in the text separates them.
// collectCallAnchors answers those with SILENCE — it contributes no anchor when
// the accepted span begins with '_' — because a fragment could be CONFIDENTLY
// misattributed, which is the one outcome nothing downstream can undo. The
// silence is likewise reported as imprecise: it keeps the fragment out of
// locate, but it is not evidence that the tree was searched.
//
// The one case still answered wrongly is a mixed name with no underscore
// (`ตัวแปรParseConfig`), which truncates to its Latin tail and can be
// CONFIDENTLY misattributed to another declaration of that tail. That is
// tracked as follow-on work; it is not new here — the ASCII byte scan this
// replaced produced the same tail.
func isWordBoundary(run, next int) bool {
	if run == scriptUnset || run == next {
		return false
	}
	return run == scriptSpacing || next == scriptSpacing
}

// recordedAnchorForm reduces a raw span to the exact token addAnchor would
// record for it. It is the single definition of "the anchor that is actually
// recorded", and every predicate that means to talk about that anchor —
// addAnchor itself and both underscore guards in collectCallAnchors — goes
// through it, so the three cannot drift apart again.
func recordedAnchorForm(raw string) string {
	return foldAnchorForm(trailingSegment(strings.TrimSpace(raw)))
}

// leadsWithUnderscore reports whether tok begins with '_' once the leading
// script-neutral runes are skipped.
//
// The skip is the point. collectCallAnchors' silencing guard fires at a
// spaceless/spacing boundary, and the break lands on the first rune the run
// could classify — which is NOT necessarily the underscore. U+30FC and a
// combining mark are both script-neutral, so either can sit between the break
// and the underscore, and a test on the first rune alone lets the fragment
// through. Nothing script-bearing may be skipped: reaching a rune with a script
// means the token starts with a real name, not with an orphaned underscore.
func leadsWithUnderscore(tok string) bool {
	for _, r := range tok {
		if r == '_' {
			return true
		}
		if spacelessScriptOf(r) != scriptNeutral {
			return false
		}
	}
	return false
}

// recordAnchor records an ALREADY-reduced token if it qualifies, reporting
// whether it did.
//
// Both scans call it with a token they reduced through recordedAnchorForm
// themselves: the call scan has already reduced the span to ask its underscore
// questions of the recorded anchor, so handing the raw span back would fold
// and re-scan it a second time — and worse, would leave the guard's idea of
// the anchor and the recorded one as two separately computed values that a
// later edit could let diverge. Passing the token makes them the same value,
// not merely the same helper's output.
//
// The bool is what lets a per-span fidelity loss be attributed to a NAME: a
// span can lose fidelity and still contribute nothing (its token fails the
// shape or signal test), and such a span has no member to drop. It also marks
// the faithful contributions: a span that records outside the glued path is
// evidence the name was read cleanly at least once.
func recordAnchor(tok string, seen map[string]struct{}) bool {
	if !isIdentifierShaped(tok) || !hasIdentifierSignal(tok) {
		return false
	}
	seen[tok] = struct{}{}
	return true
}

// foldAnchorForm puts one token in NFC, the single normalization form every
// anchor and every symbol-index key is stored under.
//
// It exists because a Tier 4 lookup is a byte-exact Go map lookup: `módulo` typed
// as o+U+0301 and `módulo` typed as U+00F3 are the SAME identifier to a compiler
// and to a human, and two different keys to a map. Source files carry whichever
// form their author's editor produced; reviewer models emit NFC. Left unfolded,
// an NFD-spelled name misses an NFC-built index, resolve returns tier4NoMatch for
// a construct plainly in the tree, and a real finding is deleted and charged to
// the reviewer as a phantom — an ordinary case in any Spanish, Portuguese,
// French, or Vietnamese codebase.
//
// NFC (never NFD) because it is what Go source is conventionally written in and
// what the models emit, so the common path is a no-op. Both sides must call this;
// folding either alone leaves exactly the mismatch it is meant to close.
//
// It is deliberately NOT a case fold: the anchor alphabet is case-sensitive
// (isIdentifierShaped and hasIdentifierSignal both read case), and folding case
// here would collide distinct declarations.
func foldAnchorForm(tok string) string {
	if norm.NFC.IsNormalString(tok) {
		return tok // overwhelmingly the common path: no allocation
	}
	return norm.NFC.String(tok)
}

// trailingSegment reduces a qualified reference to the declared name the symbol
// index keys on: "stream.ValidatePath" -> "ValidatePath", "Host::Parser" ->
// "Parser", "obj->run" -> "run". Separators are stripped left-to-right so the
// LAST segment wins regardless of which qualifier style the reviewer used.
func trailingSegment(s string) string {
	for _, sep := range [...]string{"->", "::", ".", ":", "#", "/"} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	return s
}

// isQualifiedIdentRune reports whether r may appear in a qualified identifier
// run scanned backwards from a call paren. The class is rune-based — letters,
// digits, combining marks (the same alphabet isIdentifierShaped admits) plus
// '_' — so a non-ASCII call name is captured WHOLE rather than truncated at a
// multibyte rune's continuation byte into a fragment that names something
// else. '.' is included so a package- or receiver-qualified call is captured
// whole and reduced by trailingSegment.
func isQualifiedIdentRune(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return true
	case unicode.In(r, unicode.Mn, unicode.Mc):
		return true
	case r == '_', r == '.':
		return true
	}
	return false
}

// isIdentifierShaped reports whether tok is a bare identifier of usable length:
// a letter or underscore followed by letters, digits, underscores, or combining
// marks. A span containing whitespace, punctuation, or any other character (a
// quoted English phrase, a sentence fragment, a path) fails here.
//
// Letters are tested with unicode.IsLetter, not an ASCII range. Go, Python and
// TypeScript all admit non-ASCII identifiers, and an ASCII-only test silently
// dropped them — which is worse than it sounds here, because a finding whose
// real subject was never extracted can still be judged "checked and found
// nothing" on whatever co-cited ASCII anchor happened to miss.
//
// Combining marks (Mn and Mc) are admitted at non-initial positions for the
// same reason: a macOS- or git-normalised file spells café as e + U+0301, and
// Devanagari names carry vowel signs (नाम is न + ा + म), so a mark-rejecting
// filter drops the name of a declaration the grammar (isDeclNameRune,
// symbolindex.go) admits — the two must agree, or a grammar-admitted
// declaration never reaches presentInSource. Admission here is only the SHAPE
// half, though: a caseless-script name still carries no identifier signal
// (hasIdentifierSignal), so it reaches present from the harvest but anchors a
// finding only when it also carries an underscore. Me (enclosing marks) stays
// rejected: it is outside ECMAScript ID_Continue, and a leading mark is never
// legal, exactly as a leading digit is not.
func isIdentifierShaped(tok string) bool {
	if utf8.RuneCountInString(tok) < minAnchorLen {
		return false
	}
	for i, r := range tok {
		switch {
		case unicode.IsLetter(r), r == '_':
		case unicode.IsDigit(r), unicode.In(r, unicode.Mn, unicode.Mc):
			if i == 0 {
				return false // an identifier never starts with a digit or combining mark
			}
		default:
			return false
		}
	}
	return true
}

// hasIdentifierSignal reports whether an identifier-shaped token actually looks
// like code rather than an ordinary English word that happened to be quoted.
// The signal is an INTERNAL case transition (camelCase, PascalCase past the
// first letter) or an underscore (snake_case) — the two conventions that
// separate a declared name from prose.
//
// The transition must be internal, at index > 0. A merely-capitalized single
// word — `Timeout`, `Error`, `Cannot`, `Unresolved` — is how reviewers quote
// user-facing strings and sentence-initial prose, and admitting it was doubly
// harmful: it manufactured no-match "evidence" out of English, and a spurious
// unique hit on a same-named symbol became a confidently wrong PathSuggestion.
//
// A single-case word (`handler`, `Close`, `Parse`) carries no signal and is
// rejected even inside backticks. That is deliberately conservative: such a word
// matches a symbol somewhere in almost any tree. The cost is a missed Tier 4
// resolution for a finding that names only a single-word symbol, which degrades
// to today's Tier 1-3 behavior — never to a wrong answer.
func hasIdentifierSignal(tok string) bool {
	if strings.Contains(tok, "_") {
		return true
	}
	prevLower := false
	for i, r := range tok {
		if i > 0 && prevLower && unicode.IsUpper(r) {
			return true
		}
		// Only a cased rune moves the transition tracker: an uncased rune
		// (a digit or a combining mark) carries prevLower across unchanged,
		// so the NFD spelling of a camelCase name (cafe + U+0301 + Bar) keeps
		// the signal its NFC spelling has.
		switch {
		case unicode.IsLower(r):
			prevLower = true
		case unicode.IsUpper(r):
			prevLower = false
		}
	}
	return false
}
