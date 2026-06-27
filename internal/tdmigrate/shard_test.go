package tdmigrate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"epic-11.2":                "epic-11.2",
		"11.0_executing_reviewers": "11.0_executing_reviewers",
		"llmclient OpenAI-compatible tool handling": "llmclient-openai-compatible-tool-handling",
		"  ///  ": "section",
	}
	for in, want := range cases {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShardFilename_CollisionDisambiguates(t *testing.T) {
	used := map[string]bool{}
	a := Shard{Date: "2026-06-26", Label: "x"}
	b := Shard{Date: "2026-06-26", Label: "x"}
	n1 := ShardFilename(a, used)
	n2 := ShardFilename(b, used)
	if n1 == n2 {
		t.Fatalf("collision not disambiguated: both %q", n1)
	}
	if n1 != "2026-06-26_x.yaml" || n2 != "2026-06-26_x-2.yaml" {
		t.Errorf("unexpected names: %q, %q", n1, n2)
	}
}

func sampleShards(t *testing.T) []Shard {
	t.Helper()
	shards, err := ParseREADME(sample9Col + "\n" + sample11Col)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return shards
}

func TestWriteAndLoadShards_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := sampleShards(t)
	written, err := WriteShards(dir, orig)
	if err != nil {
		t.Fatalf("WriteShards: %v", err)
	}
	if len(written) != len(orig) {
		t.Fatalf("wrote %d files, want %d", len(written), len(orig))
	}
	loaded, err := LoadShards(dir)
	if err != nil {
		t.Fatalf("LoadShards: %v", err)
	}
	if !reflect.DeepEqual(canonicalize(orig), canonicalize(loaded)) {
		t.Errorf("disk round-trip mismatch:\norig=%+v\nloaded=%+v", orig, loaded)
	}
}

func TestWriteShards_IdempotentPrune(t *testing.T) {
	dir := t.TempDir()
	// A stale shard from a hypothetical prior run must be pruned.
	stale := filepath.Join(dir, "9999-01-01_stale.yaml")
	if err := os.WriteFile(stale, []byte("date: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteShards(dir, sampleShards(t)); err != nil {
		t.Fatalf("WriteShards: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale shard not pruned: %v", err)
	}
}

func TestWriteShards_SkipsUnrelatedYAML(t *testing.T) {
	dir := t.TempDir()
	// A non-shard YAML file (e.g., user config) must not be pruned.
	unrelated := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(unrelated, []byte("server: localhost\nport: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteShards(dir, sampleShards(t)); err != nil {
		t.Fatalf("WriteShards: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated YAML was pruned: %v", err)
	}
}

func TestMarshalShard_MultilineBlockScalar(t *testing.T) {
	s := Shard{
		Date: "2026-06-26", SourceType: "Sprint", Label: "x",
		Items: []Item{{
			Group: "1", Status: StatusOpen, Severity: "LOW",
			File: "f.go:1", Problem: "line one\nline two", Fix: "do it",
			Category: "correctness", EstMinutes: 5, Source: "src",
		}},
	}
	data, err := MarshalShard(s)
	if err != nil {
		t.Fatalf("MarshalShard: %v", err)
	}
	// A multi-line value should be emitted as a block scalar, not a quoted blob
	// with literal \n escapes.
	if !strings.Contains(string(data), "problem: |-") && !strings.Contains(string(data), "problem: |") {
		t.Errorf("multi-line problem not a block scalar:\n%s", data)
	}
}
