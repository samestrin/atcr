package reconcile

import (
	"encoding/json"
	"testing"
)

func TestAmbiguousID_StableAndOrderIndependent(t *testing.T) {
	a := AmbiguousID("a.go", 10, "problem one", "problem two")
	b := AmbiguousID("a.go", 10, "problem two", "problem one") // swapped order
	eq(t, a, b, "id is order-independent in the two problem texts")
	hasPrefix(t, a, "amb-", "id carries the amb- prefix")
	eq(t, len(a), len("amb-")+32, "128-bit hex digest")
}

// TestAmbiguousID_NulByteFieldBoundary asserts that a NUL byte embedded in a
// PROBLEM text cannot alias the \x00 field separator — two distinct inputs must
// never hash to the same ID.
func TestAmbiguousID_NulByteFieldBoundary(t *testing.T) {
	// "x\x00y" in problemA with "z" in problemB must differ from
	// "x" in problemA with "y\x00z" in problemB: the NUL in the text must not
	// collapse to the same separator byte that delimits fields.
	a := AmbiguousID("a.go", 10, "x\x00y", "z")
	b := AmbiguousID("a.go", 10, "x", "y\x00z")
	notEq(t, a, b, "NUL byte in problem text must not alias field delimiter")
}

func TestAmbiguousID_DistinctInputsDistinctIDs(t *testing.T) {
	base := AmbiguousID("a.go", 10, "p1", "p2")
	notEq(t, base, AmbiguousID("b.go", 10, "p1", "p2"), "file changes the id")
	notEq(t, base, AmbiguousID("a.go", 11, "p1", "p2"), "line changes the id")
	notEq(t, base, AmbiguousID("a.go", 10, "p1", "p3"), "problem text changes the id")
}

func TestAmbiguousHash_DigestsEmittedBytes(t *testing.T) {
	clusters := []AmbiguousCluster{
		{ID: "amb-1", File: "a.go", Line: 1, Similarity: 0.5, Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p1", Reviewer: "greta"},
			{File: "a.go", Line: 1, Problem: "p2", Reviewer: "kai"},
		}},
	}
	h1 := AmbiguousHash(clusters)
	eq(t, h1, AmbiguousHash(clusters), "hash is deterministic for identical input")
	hasPrefix(t, h1, "sha256:", "hash carries the sha256: prefix")

	// An empty sidecar still hashes the canonical "[]\n" bytes (never panics).
	empty := AmbiguousHash(nil)
	hasPrefix(t, empty, "sha256:", "empty hash prefixed")
	notEq(t, h1, empty, "empty differs from populated")
}

func TestHashBytes_StablePrefixedDigest(t *testing.T) {
	h := HashBytes([]byte("hello"))
	hasPrefix(t, h, "sha256:", "prefixed")
	eq(t, h, HashBytes([]byte("hello")), "deterministic")
	notEq(t, h, HashBytes([]byte("world")), "different input differs")
}

// TestFinding_JSONMarshalable is a CI guard: AmbiguousHash returns "" (not panics)
// on json.Marshal failure, but that path is only safe because all Finding and
// AmbiguousCluster fields are JSON-serializable today.  If a chan/func/cyclic
// field is ever added, this test fails in CI before a live panic can occur.
func TestFinding_JSONMarshalable(t *testing.T) {
	if _, err := json.Marshal(Finding{}); err != nil {
		t.Fatalf("Finding must be JSON-marshalable (AmbiguousHash returns \"\" otherwise): %v", err)
	}
	if _, err := json.Marshal(AmbiguousCluster{}); err != nil {
		t.Fatalf("AmbiguousCluster must be JSON-marshalable: %v", err)
	}
}

// TestAmbiguousHash_Golden guards byte-stability of the canonical sidecar
// encoding. Any change to field tags, ordering, indentation, or trailing
// newline that perturbs emitted bytes will break this hash and fail CI.
func TestAmbiguousHash_Golden(t *testing.T) {
	clusters := []AmbiguousCluster{
		{ID: "amb-1", File: "a.go", Line: 1, Similarity: 0.5, Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p1", Reviewer: "greta"},
			{File: "a.go", Line: 1, Problem: "p2", Reviewer: "kai"},
		}},
	}
	want := "sha256:c97fab233b4d4f34c21e8de29ed7e643523a530f129e93b56f23244555551e5e"
	got := AmbiguousHash(clusters)
	eq(t, got, want, "golden hash must match canonical encoding")
}
