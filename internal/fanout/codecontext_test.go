package fanout

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/hookobs"
	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/registry"
)

const twoFileDiff = `diff --git a/alpha.go b/alpha.go
--- a/alpha.go
+++ b/alpha.go
@@ -1 +1 @@
-old
+new
diff --git a/beta.go b/beta.go
--- a/beta.go
+++ b/beta.go
@@ -1 +1 @@
-old
+new
`

func TestCodeContextFor_SplitsPayloadPerFile(t *testing.T) {
	t.Parallel()

	got := codeContextFor("diff", twoFileDiff)

	require.Len(t, got, 2)
	assert.Equal(t, "alpha.go", got[0].Path)
	assert.Equal(t, "beta.go", got[1].Path)
	var joined strings.Builder
	for _, r := range got {
		joined.WriteString(r.Body)
	}
	assert.Equal(t, twoFileDiff, joined.String(), "bodies must reconstruct the payload exactly")
}

// TestCodeContextFor_DegradesToNil: this runs on the review's critical path, so
// every shape it cannot attribute must produce nil rather than an error or a
// panic. An audit helper must not be able to fail the review it observes.
func TestCodeContextFor_DegradesToNil(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ mode, text string }{
		"empty payload":   {"diff", ""},
		"prose payload":   {"diff", "not a diff\n"},
		"unknown mode":    {"nonsense", twoFileDiff},
		"empty mode":      {"", twoFileDiff},
		"whitespace only": {"diff", "\n\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, codeContextFor(tc.mode, tc.text))
		})
	}
}

// TestCodeContextFor_BodiesAliasPayload: the engine threads these onto every
// invocation of every agent. Copying the bodies would duplicate the whole
// payload per agent, which on a chunked review of a large diff is the
// difference between free and expensive.
func TestCodeContextFor_BodiesAliasPayload(t *testing.T) {
	t.Parallel()

	for _, r := range codeContextFor("diff", twoFileDiff) {
		assert.True(t, strings.Contains(twoFileDiff, r.Body),
			"body must be a substring of the payload, not a copy")
	}
}

// TestBuildSlots_ChunkedCodeContextMatchesTheChunk is the claim the whole seam
// rests on: a chunk-slot's code context reports the files THAT CHUNK was sent,
// not the review's whole file list. The chunked strategy re-derives its splits
// from flattened text, so this is the case where a per-review answer would be
// wrong — and where an auditor asking "what did this call see" would be
// misled rather than merely under-informed.
func TestBuildSlots_ChunkedCodeContextMatchesTheChunk(t *testing.T) {
	cfg := twoAgentConfig("http://unused")
	cfg.Project = &registry.ProjectConfig{Agents: []string{"greta"}}
	cfg.Settings.ReviewStrategy = "chunked"
	mcl := 5 // tiny budget so a two-file diff splits one file per chunk
	g := cfg.Registry.Agents["greta"]
	g.MaxContextLines = &mcl
	cfg.Registry.Agents["greta"] = g

	diff := fileSeg("a.go", 6) + fileSeg("b.go", 6)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.Len(t, slots, 2)

	require.Len(t, slots[0].Primary.CodeContext, 1, "chunk 0 saw exactly one file")
	assert.Equal(t, "a.go", slots[0].Primary.CodeContext[0].Path)
	require.Len(t, slots[1].Primary.CodeContext, 1, "chunk 1 saw exactly one file")
	assert.Equal(t, "b.go", slots[1].Primary.CodeContext[0].Path)
	assert.Contains(t, slots[0].Primary.CodeContext[0].Body, "diff --git a/a.go",
		"the body must be that file's slice of the payload, verbatim")
}

// TestBuildSlots_BulkCodeContextCoversEveryFile: the bulk path sends the whole
// diff in one call, so its record must list every file.
func TestBuildSlots_BulkCodeContextCoversEveryFile(t *testing.T) {
	cfg := twoAgentConfig("http://unused") // ReviewStrategy "" => bulk
	diff := fileSeg("a.go", 3) + fileSeg("b.go", 3)
	payloads := map[string]modePayload{"blocks": {Text: diff, FileCount: 2}}

	slots, _, err := buildSlots(cfg, payloads, ReviewRange{Base: "a", Head: "b"}, "", "", true)
	require.NoError(t, err)
	require.NotEmpty(t, slots)

	got := make([]string, 0, 2)
	for _, r := range slots[0].Primary.CodeContext {
		got = append(got, r.Path)
	}
	assert.Equal(t, []string{"a.go", "b.go"}, got)
}

// TestInvokeAgent_StampsCodeContext covers the second half of the seam: the
// engine is the only layer that knows which slice of the diff a given agent
// received (the chunked strategy gives each chunk-slot a different subset), so
// if it does not stamp it here nothing downstream can recover it.
func TestInvokeAgent_StampsCodeContext(t *testing.T) {
	t.Parallel()
	rec := &ctxRecordingCompleter{}
	e := NewEngine(rec)
	refs := []hookobs.CodeRef{{Path: "alpha.go", Body: "diff --git a/alpha.go b/alpha.go\n"}}

	res := e.invokeAgent(context.Background(), Agent{
		Name:        "security-reviewer",
		CodeContext: refs,
		Invocation:  llmclient.Invocation{Model: "test/model-a", Prompt: "p"},
	})
	require.Empty(t, res.Err)

	calls := rec.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, refs, calls[0].CodeContext,
		"invokeAgent must report the files this agent actually saw")
}

// TestInvokeAgent_NoCodeContextStaysNil: a bare Agent (doctor, direct
// construction, a test) carries none, and must not manufacture an empty slice.
func TestInvokeAgent_NoCodeContextStaysNil(t *testing.T) {
	t.Parallel()
	rec := &ctxRecordingCompleter{}
	e := NewEngine(rec)

	res := e.invokeAgent(context.Background(), Agent{
		Name:       "security-reviewer",
		Invocation: llmclient.Invocation{Model: "test/model-a", Prompt: "p"},
	})
	require.Empty(t, res.Err)

	require.Len(t, rec.calls(), 1)
	assert.Nil(t, rec.calls()[0].CodeContext)
}
