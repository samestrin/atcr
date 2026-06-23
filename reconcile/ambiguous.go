package reconcile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// AmbiguousID is the stable content-addressed id for a gray-zone pair. It is
// derived from the location and the two PROBLEM texts (order-independent), so the
// id is byte-identical on the initial reconcile and on an adjudicated
// re-invocation of the same sources — the property that lets a host reference a
// cluster across runs without persisting a counter.
func AmbiguousID(file string, line int, problemA, problemB string) string {
	lo, hi := problemA, problemB
	if hi < lo {
		lo, hi = hi, lo
	}
	h := sha256.Sum256([]byte(file + "\x00" + strconv.Itoa(line) + "\x00" + lo + "\x00" + hi))
	// 128 bits: a collision would alias two distinct gray pairs to one id and let
	// one merge decision collapse the wrong pair — the one outcome the design
	// forbids — so spend the bytes.
	return "amb-" + hex.EncodeToString(h[:16])
}

// HashBytes returns the "sha256:<hex>" digest used for adjudication baseline
// binding.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// AmbiguousHash returns the digest of the exact bytes the reconciler emits for
// the ambiguous sidecar, recorded in the summary so a host can copy it verbatim
// into an adjudication file (the reconciler computes the hash; the host never
// does).
func AmbiguousHash(clusters []AmbiguousCluster) string {
	var buf bytes.Buffer
	if err := renderIndentedJSON(&buf, clusters); err != nil {
		panic(fmt.Sprintf("reconcile: AmbiguousHash: unreachable JSON render error: %v", err))
	}
	return HashBytes(buf.Bytes())
}

// renderIndentedJSON writes v as 2-space-indented JSON with a trailing newline.
// It is the canonical encoding for the reconciled sidecar artifacts, so the
// AmbiguousHash digest binds to the exact emitted bytes.
func renderIndentedJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
