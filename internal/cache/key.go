// Package cache implements payload-level diff caching for the review fan-out
// (Epic 5.2). A reviewer's raw output is content-addressed by the payload it
// saw, its model, and its persona, so a re-run over an unchanged diff replays
// the prior result and skips the LLM API call entirely.
//
// Granularity note: atcr concatenates every changed file into one payload per
// mode before any agent is called, so the cache unit is per-agent-per-payload,
// not per-file. Changing even one file in the diff alters the payload digest and
// is a full miss for that agent — true per-file incremental caching would
// require re-architecting the fan-out engine and is out of scope here.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashText returns the canonical "sha256:<hex>" digest of s — the same format
// used elsewhere in atcr (reconcile/ambiguous.go) so cache keys read uniformly
// against other content hashes in artifacts and logs.
func HashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Key derives the content-addressed cache key for one reviewer call from the
// digest of the full rendered prompt the model received, the model id, and a
// tuning token (the temperature). promptHash is pre-computed via HashText so the
// large prompt text is hashed once at agent-build time rather than on every
// lookup.
//
// The three inputs are joined with a NUL separator (which cannot appear in a hex
// digest, a model id, or a numeric tuning token) before the outer hash, so no
// boundary ambiguity can map two distinct triples onto the same key.
//
// Keying on the rendered prompt (not the raw payload/persona) is deliberate: the
// prompt subsumes payload + persona + the per-agent scope focus + the base/head
// refs, so any input that changes what the model sees invalidates the entry.
// Deliberately excluded (Epic 5.2): min_severity and max_findings are
// deterministic post-LLM filters applied after the call, so the same cached
// response is valid regardless of those thresholds.
func Key(promptHash, model, tuning string) string {
	h := sha256.New()
	h.Write([]byte(promptHash))
	h.Write([]byte{0})
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(tuning))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
