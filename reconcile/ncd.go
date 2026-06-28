package reconcile

import (
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	ncdEncOnce sync.Once
	ncdEnc     *zstd.Encoder
)

// ncdEncoder returns the process-wide zstd encoder used for all NCD scoring. The
// encoder level is pinned so compressed sizes — and therefore NCD scores and any
// golden fixtures derived from them — are reproducible for a given
// klauspost/compress version. EncodeAll is safe for concurrent use, so a single
// shared encoder serves every caller without locking.
//
// SpeedFastest is deliberate, not a perf shortcut: NCD discriminates duplicates
// from unrelated findings only when single-string compression leaves enough
// redundancy for a near-duplicate concatenation to compress sharply. Higher zstd
// levels compress each finding so well individually that the concatenation gains
// little, collapsing the duplicate/unrelated gap (measured: ~0.35 separation at
// SpeedFastest vs ~0.03 at SpeedDefault on realistic finding text).
func ncdEncoder() *zstd.Encoder {
	ncdEncOnce.Do(func() {
		ncdEnc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	})
	return ncdEnc
}

// csize returns the zstd-compressed byte length of b. dst is caller-owned scratch
// whose contents are overwritten and whose capacity is reused; only the length of
// the result is read, so the compressed bytes themselves are never retained.
func csize(enc *zstd.Encoder, b, dst []byte) int {
	return len(enc.EncodeAll(b, dst[:0]))
}

// ncd computes the Normalized Compression Distance between a and b:
//
//	NCD(a,b) = (C(ab) - min(C(a), C(b))) / max(C(a), C(b))
//
// where C is the zstd-compressed length. NCD is ~0 for (near-)identical content
// and approaches 1 for unrelated content, independent of shared vocabulary —
// which is what lets it catch lexically-diverse duplicates that token overlap
// misses. This value is advisory (display/tests); the merge gate consumes the
// integer compressed sizes directly via classify so the decision boundary stays
// float-free and deterministic.
func ncd(a, b []byte) float64 {
	enc := ncdEncoder()
	scratch := make([]byte, 0, len(a)+len(b)+64)
	ca := csize(enc, a, scratch)
	cb := csize(enc, b, scratch)
	cab := concatSize(enc, a, b, nil, scratch)
	return ncdScore(cab, ca, cb)
}

// concatSize returns the smaller zstd-compressed size of (a‖b) and (b‖a). A real
// compressor's C(xy) is mildly asymmetric — the same pair can swing across a
// threshold purely by concatenation order — so taking the minimum makes the score
// order-independent (deterministic regardless of a finding's position in its
// cluster) and maximally sensitive to shared content. cat and dst are caller-owned
// scratch buffers: cat holds the concatenation, dst the compression output; both
// are reused and only the result length is read.
func concatSize(enc *zstd.Encoder, a, b, cat, dst []byte) int {
	cat = append(cat[:0], a...)
	cat = append(cat, b...)
	s1 := csize(enc, cat, dst)
	cat = append(cat[:0], b...)
	cat = append(cat, a...)
	s2 := csize(enc, cat, dst)
	if s2 < s1 {
		return s2
	}
	return s1
}

// ncdScore turns the three compressed sizes into the advisory NCD distance. The
// numerator is clamped at 0 so the rare case C(ab) < min(C(a),C(b)) (concatenation
// compressing below the smaller part alone) reports 0 rather than a negative
// distance. max==0 is unreachable for real input (zstd always emits a frame
// header) but is guarded to avoid a divide-by-zero.
func ncdScore(cab, ca, cb int) float64 {
	lo, hi := ca, cb
	if hi < lo {
		lo, hi = hi, lo
	}
	num := cab - lo
	if num < 0 {
		num = 0
	}
	if hi == 0 {
		return 0
	}
	return float64(num) / float64(hi)
}
