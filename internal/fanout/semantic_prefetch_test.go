package fanout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/semindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.12.1 T2: the similarity retriever is BUILT here, from the resolved
// context_prefetch.semantic block, because payload must not import registry.
// These tests pin the three things only this package can prove: a config-free
// run never reaches the network and is unchanged, every outbound query passes
// the secret_guard policy first, and one fan-out retrieves once for every agent.

// countingTransport counts every request that reaches http.DefaultTransport,
// the transport semindex.Client uses.
type countingTransport struct {
	n    atomic.Int64
	next http.RoundTripper
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.next.RoundTrip(r)
}

// countDefaultTransport swaps http.DefaultTransport for a counter for the
// test's lifetime. The test must not run in parallel.
func countDefaultTransport(t *testing.T) *countingTransport {
	t.Helper()
	ct := &countingTransport{next: http.DefaultTransport}
	prev := http.DefaultTransport
	http.DefaultTransport = ct
	t.Cleanup(func() { http.DefaultTransport = prev })
	return ct
}

// semanticFanoutRepo is prefetchFanoutRepo plus a lookalike nothing references,
// so a similarity hit has a tracked, unchanged file to land on.
func semanticFanoutRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	fanoutGit(t, dir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.go"),
		[]byte("package p\n\nfunc ReadStore(path string) ([]byte, error) {\n\treturn nil, nil\n}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "similar.go"),
		[]byte(semanticSimilarSrc), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "seed the store and a lookalike")
	base = fanoutGit(t, dir, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "store.go"),
		[]byte("package p\n\nfunc ReadStore(path string) (string, error) {\n\treturn \"\", nil\n}\n"), 0o644))
	fanoutGit(t, dir, "add", "-A")
	fanoutGit(t, dir, "commit", "-q", "-m", "change the store return shape")
	head = fanoutGit(t, dir, "rev-parse", "HEAD")
	return dir, base, head
}

const semanticSimilarSrc = "package p\n\nfunc ReadCache(key string) (string, error) {\n\treturn \"\", nil\n}\n"

// fakeIndex plays both the embedding endpoint and qdrant on loopback http,
// which semindex allows without allow_insecure_http.
type fakeIndex struct {
	srv *httptest.Server

	mu      sync.Mutex
	embeds  int
	queries int
	inputs  [][]string
}

func newFakeIndex(t *testing.T, repo string) *fakeIndex {
	t.Helper()
	f := &fakeIndex{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/embeddings"):
			f.embeds++
			var req struct {
				Input []string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.inputs = append(f.inputs, req.Input)
			type datum struct {
				Index     int       `json:"index"`
				Embedding []float64 `json:"embedding"`
			}
			var resp struct {
				Data []datum `json:"data"`
			}
			for i := range req.Input {
				resp.Data = append(resp.Data, datum{Index: i, Embedding: make([]float64, semindex.VectorSize)})
			}
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasSuffix(r.URL.Path, "/points/query"):
			f.queries++
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": []any{
				map[string]any{"score": 0.9, "payload": map[string]any{
					"file_path": filepath.Join(repo, "similar.go"), "start_line": 3, "end_line": 5,
					"name":    "ReadCache",
					"content": "func ReadCache(key string) (string, error) {\n\treturn \"\", nil\n}",
				}},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func enableSemantic(cfg *ReviewConfig, url string) {
	cfg.Settings.SemanticPrefetch = registry.SemanticPrefetch{
		Enabled: true, Endpoint: url, EmbeddingEndpoint: url, Collection: "atcr", TopK: 3,
	}
}

// prepareSemantic runs PrepareReview into a fresh output dir and returns the
// manifest and the rendered blocks-mode payload.
func prepareSemantic(t *testing.T, cfg *ReviewConfig, repo, base, head string) (manifest, rendered []byte) {
	t.Helper()
	req := reviewReq(repo, repo, base, head)
	req.OutputDir = filepath.Join(t.TempDir(), "review")
	prep, err := PrepareReview(context.Background(), cfg, req)
	require.NoError(t, err)
	manifest, err = os.ReadFile(filepath.Join(prep.Dir, "manifest.json"))
	require.NoError(t, err)
	matches, err := filepath.Glob(filepath.Join(prep.Dir, "payload", "*.txt"))
	require.NoError(t, err)
	require.NotEmpty(t, matches, "PrepareReview must write the rendered payload")
	for _, m := range matches {
		b, err := os.ReadFile(m)
		require.NoError(t, err)
		rendered = append(rendered, b...)
	}
	return manifest, rendered
}

func TestSemanticPrefetch_ConfigFreeRunMakesNoRequestAndIsUnchanged(t *testing.T) {
	repo, base, head := semanticFanoutRepo(t)
	ct := countDefaultTransport(t)

	// No context_prefetch.semantic config at all.
	freeCfg := twoAgentConfig("http://unused")
	require.Nil(t, semanticRetrieverOption(context.Background(), freeCfg, repo),
		"a config-free run must not construct a retriever")
	freeManifest, freePayload := prepareSemantic(t, freeCfg, repo, base, head)

	// A block that names an endpoint but is not enabled. The resolver hands back
	// the zero value, so this is the same run as one built before the tier.
	offCfg := twoAgentConfig("http://unused")
	offCfg.Settings.SemanticPrefetch = registry.SemanticPrefetch{
		Endpoint: "http://127.0.0.1:1", EmbeddingEndpoint: "http://127.0.0.1:1", Collection: "atcr",
	}
	require.Nil(t, semanticRetrieverOption(context.Background(), offCfg, repo))
	offManifest, offPayload := prepareSemantic(t, offCfg, repo, base, head)

	assert.Zero(t, ct.n.Load(), "a config-free run must issue zero HTTP requests")

	// The baseline "without the feature": the payload built straight from the
	// options buildPayloads wired before the similarity tier existed.
	rb := payload.NewRangeBuilder(context.Background(), repo, base, head,
		payload.WithEscalation(payload.ResolveEscalationConfig(escalationOverrides(freeCfg.Registry.PayloadEscalation))),
		payload.WithMaxClaimBytes(freeCfg.Settings.ResolvedMaxClaimBytes()),
		payload.WithMaxPrefetchBytes(freeCfg.Settings.ResolvedMaxPrefetchBytes()))
	want, err := rb.BuildEntries(payload.PayloadMode("blocks"))
	require.NoError(t, err)
	got, _, err := buildPayloads(context.Background(), freeCfg, repo, base, head, false)
	require.NoError(t, err)
	assert.Equal(t, want, got["blocks"].Entries, "the rendered payload must be byte-identical to a run without the feature")
	assert.NotContains(t, string(freePayload), "similarity to", "no similarity snippet may appear")

	assert.Equal(t, string(freePayload), string(offPayload), "rendered payloads must be byte-identical")
	assert.Equal(t, string(freeManifest), string(offManifest), "manifests must be byte-identical")
}

func TestSemanticPrefetch_EnabledRetrievesOnceForEveryAgent(t *testing.T) {
	repo, base, head := semanticFanoutRepo(t)
	idx := newFakeIndex(t, repo)

	cfg := twoAgentConfig("http://unused")
	// Two agents on two payload modes, so two mode payloads are built from the
	// one range.
	kai := cfg.Registry.Agents["kai"]
	kai.Payload = "files"
	cfg.Registry.Agents["kai"] = kai
	enableSemantic(cfg, idx.srv.URL)
	ct := countDefaultTransport(t)

	payloads, rb, err := buildPayloads(context.Background(), cfg, repo, base, head, false)
	require.NoError(t, err)
	require.Len(t, payloads, 2, "PRECONDITION: two payload modes")

	idx.mu.Lock()
	assert.Equal(t, 1, idx.embeds, "one query set is embedded once per fan-out")
	assert.Equal(t, 1, idx.queries, "each query is searched once per fan-out")
	assert.Equal(t, int64(2), ct.n.Load(),
		"the counter the config-free test reads at zero must see every retrieval request")
	require.Len(t, idx.inputs, 1)
	assert.Equal(t, []string{"func ReadStore(path string) (string, error)"}, idx.inputs[0])
	idx.mu.Unlock()

	var sections []string
	for mode, mp := range payloads {
		var section string
		for _, e := range mp.Entries {
			if e.Path == payload.PrefetchContextPath {
				section = e.Body
			}
		}
		require.Containsf(t, section, "similar.go:3-5 (similarity to ReadCache)", "mode %s", mode)
		sections = append(sections, section)
	}
	assert.Equal(t, sections[0], sections[1], "every agent sees one ordering of one snippet set")
	assert.True(t, rb.PrefetchStatus().Present)
}

// recordingRetriever stands in for semindex.Client behind the guard.
type recordingRetriever struct {
	calls   int
	queries []string
}

func (r *recordingRetriever) Retrieve(_ context.Context, queries []string, _ int) ([]semindex.Snippet, error) {
	r.calls++
	r.queries = append([]string(nil), queries...)
	return nil, nil
}

func TestSemanticQueryGuard_EachPolicyOnACredentialLiteral(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // the built-in ruleset decides deterministically
	leaky := `func Connect(key string = "` + guardAWSKey + `") error`
	clean := "func ReadStore(path string) (string, error)"

	for _, tc := range []struct {
		policy  string
		calls   int
		want    []string
		blocked bool
	}{
		{policy: secretGuardOff, calls: 1, want: []string{leaky, clean}},
		{policy: secretGuardWarn, calls: 1, want: []string{leaky, clean}},
		{policy: secretGuardRedact, calls: 1, want: []string{
			strings.Replace(leaky, guardAWSKey, payload.SecretPlaceholder, 1), clean,
		}},
		{policy: secretGuardBlock, calls: 0, blocked: true},
	} {
		t.Run(tc.policy, func(t *testing.T) {
			inner := &recordingRetriever{}
			g := &guardedRetriever{policy: tc.policy, inner: inner}
			var err error
			stderr := captureStderr(t, func() {
				_, err = g.Retrieve(context.Background(), []string{leaky, clean}, 3)
			})
			require.Equal(t, tc.calls, inner.calls)
			assert.NotContains(t, stderr, guardAWSKey, "a warning must never print the secret")
			if tc.blocked {
				require.Error(t, err)
				var se *semindex.Error
				require.True(t, errors.As(err, &se), "block must fail open through the retriever error path")
				assert.Equal(t, semanticDegradeSecretGuard, se.Class, "block records a degrade reason")
				assert.Contains(t, stderr, "secret_guard: block")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, inner.queries)
			switch tc.policy {
			case secretGuardOff:
				assert.Empty(t, stderr, "off scans nothing and says nothing")
			case secretGuardWarn:
				assert.Contains(t, stderr, "secret_guard: warn")
			case secretGuardRedact:
				assert.NotContains(t, strings.Join(inner.queries, "\n"), guardAWSKey)
				assert.Contains(t, stderr, "secret_guard: redact")
			}
		})
	}
}

func TestSemanticQueryGuard_PoliciesEndToEnd(t *testing.T) {
	hideGitleaks(t)
	repo, base, _ := semanticFanoutRepo(t)
	// The changed declaration header itself holds the credential, as a parameter
	// name, so it lands in the outbound query text.
	require.NoError(t, os.WriteFile(filepath.Join(repo, "store.go"),
		[]byte("package p\n\nfunc ReadStore("+guardAWSKey+" string) (string, error) {\n\treturn \"\", nil\n}\n"), 0o644))
	fanoutGit(t, repo, "add", "-A")
	fanoutGit(t, repo, "commit", "-q", "-m", "plant")
	head := fanoutGit(t, repo, "rev-parse", "HEAD")

	run := func(t *testing.T, policy string) (*fakeIndex, map[string]modePayload) {
		t.Helper()
		idx := newFakeIndex(t, repo)
		cfg := twoAgentConfig("http://unused")
		enableSemantic(cfg, idx.srv.URL)
		cfg.Settings.SecretGuard = policy
		var (
			payloads map[string]modePayload
			err      error
		)
		captureStderr(t, func() {
			payloads, _, err = buildPayloads(context.Background(), cfg, repo, base, head, false)
		})
		require.NoError(t, err, "no policy aborts the payload build")
		require.NotEmpty(t, payloads)
		return idx, payloads
	}

	idx, _ := run(t, secretGuardWarn)
	require.Len(t, idx.inputs, 1)
	require.Contains(t, strings.Join(idx.inputs[0], "\n"), guardAWSKey,
		"PRECONDITION: warn sends the query, credential and all")

	idx, _ = run(t, secretGuardRedact)
	require.Len(t, idx.inputs, 1, "redact still retrieves")
	assert.NotContains(t, strings.Join(idx.inputs[0], "\n"), guardAWSKey, "redact must not send the credential")
	assert.Contains(t, strings.Join(idx.inputs[0], "\n"), payload.SecretPlaceholder)

	idx, payloads := run(t, secretGuardBlock)
	assert.Zero(t, idx.embeds+idx.queries, "block must send nothing to the index")
	for mode, mp := range payloads {
		for _, e := range mp.Entries {
			assert.NotContainsf(t, e.Body, "similarity to", "mode %s: block yields zero similarity snippets", mode)
		}
	}
}

// A gitleaks that fails leaves the scan untrusted. block must then fail closed
// even on clean queries, while warn and redact proceed on the built-in rules,
// as the payload guard does (TestSecretGuard_ScannerFailure).
func TestSemanticQueryGuard_ScannerFailure(t *testing.T) {
	failingGitleaks := func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "gitleaks"), []byte("#!/bin/sh\nexit 2\n"), 0o755))
		t.Setenv("PATH", dir)
	}
	leaky := `func Connect(key string = "` + guardAWSKey + `") error`
	clean := "func ReadStore(path string) (string, error)"

	t.Run("block fails closed on clean queries", func(t *testing.T) {
		failingGitleaks(t)
		inner := &recordingRetriever{}
		g := &guardedRetriever{policy: secretGuardBlock, inner: inner}
		var err error
		stderr := captureStderr(t, func() {
			_, err = g.Retrieve(context.Background(), []string{clean}, 3)
		})
		require.Error(t, err)
		var se *semindex.Error
		require.True(t, errors.As(err, &se))
		assert.Equal(t, semanticDegradeSecretGuard, se.Class)
		assert.Zero(t, inner.calls, "an untrusted scan must send nothing")
		assert.Contains(t, stderr, "using the built-in ruleset")
		assert.Contains(t, stderr, "could not be checked")
	})
	t.Run("warn proceeds on the built-in rules", func(t *testing.T) {
		failingGitleaks(t)
		inner := &recordingRetriever{}
		g := &guardedRetriever{policy: secretGuardWarn, inner: inner}
		var err error
		stderr := captureStderr(t, func() {
			_, err = g.Retrieve(context.Background(), []string{leaky, clean}, 3)
		})
		require.NoError(t, err)
		assert.Equal(t, []string{leaky, clean}, inner.queries, "warn sends the queries unchanged")
		assert.Contains(t, stderr, "using the built-in ruleset")
		assert.Contains(t, stderr, "aws-access-token", "the built-in rules still found the key")
		assert.NotContains(t, stderr, guardAWSKey)
	})
	t.Run("redact proceeds on the built-in rules", func(t *testing.T) {
		failingGitleaks(t)
		inner := &recordingRetriever{}
		g := &guardedRetriever{policy: secretGuardRedact, inner: inner}
		var err error
		captureStderr(t, func() {
			_, err = g.Retrieve(context.Background(), []string{leaky, clean}, 3)
		})
		require.NoError(t, err)
		require.Len(t, inner.queries, 2)
		assert.NotContains(t, inner.queries[0], guardAWSKey)
		assert.Contains(t, inner.queries[0], payload.SecretPlaceholder)
	})
}

// An enabled block that semindex.New rejects still fails open, and the failure
// is not stderr-only: the option carries a retriever that fails with a
// *semindex.Error of class semanticDegradeConfig, so the manifest records it.
func TestSemanticRetrieverOption_UnbuildableClientFailsOpen(t *testing.T) {
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))
	cfg := twoAgentConfig("http://unused")
	cfg.Settings.SemanticPrefetch = registry.SemanticPrefetch{
		Enabled: true, Endpoint: "http://127.0.0.1:1", EmbeddingEndpoint: "http://127.0.0.1:1", // no collection
	}
	require.True(t, cfg.Settings.ResolvedSemanticPrefetch().Enabled, "PRECONDITION: the block is enabled")

	assert.NotNil(t, semanticRetrieverOption(ctx, cfg, t.TempDir()), "the failing retriever rides an option")
	assert.Contains(t, buf.String(), "semantic retrieval client could not be built")
	assert.Contains(t, buf.String(), string(semanticDegradeConfig))
}

// A filepath.Abs failure takes the same fail-open path: the review completes,
// the manifest records the config class, and the warning names the build failure.
func TestSemanticPrefetch_AbsFailureDegradesConfigInvalid(t *testing.T) {
	orig := absRepoPath
	absRepoPath = func(string) (string, error) { return "", errors.New("getwd: boom") }
	t.Cleanup(func() { absRepoPath = orig })

	t.Setenv("ATCR_TEST_KEY", "secret")
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))
	repo, base, head := semanticFanoutRepo(t)
	cfg := twoAgentConfig(mockProvider(t).URL)
	enableSemantic(cfg, "http://127.0.0.1:1")

	res, err := RunReview(ctx, llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	_, m := readSemanticManifest(t, res.Dir)
	require.NotNil(t, m.Prefetch)
	assert.Equal(t, string(semanticDegradeConfig), m.Prefetch.DegradeReason)
	assert.Contains(t, buf.String(), "client could not be built")
}

// The stored construction error is class-only: a configured URL (here one
// semindex.New rejects with a parse error that quotes it) reaches neither the manifest nor
// the log.
func TestSemanticPrefetch_ConstructionErrorLeaksNoURL(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	var buf bytes.Buffer
	ctx := log.NewContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	repo, base, head := semanticFanoutRepo(t)
	cfg := twoAgentConfig(mockProvider(t).URL)
	enableSemantic(cfg, "http://qdrant.example.invalid:badport")

	res, err := RunReview(ctx, llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	raw, m := readSemanticManifest(t, res.Dir)
	require.NotNil(t, m.Prefetch)
	assert.Equal(t, string(semanticDegradeConfig), m.Prefetch.DegradeReason, "PRECONDITION: the config path ran")
	for _, out := range []string{string(raw), buf.String()} {
		assert.NotContains(t, out, "qdrant.example.invalid")
	}
}

func TestFailingRetriever_ReturnsConfigClassError(t *testing.T) {
	cause := errors.New("boom")
	snips, err := failingRetriever{err: cause}.Retrieve(context.Background(), []string{"q"}, 3)
	assert.Nil(t, snips)
	var se *semindex.Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, semanticDegradeConfig, se.Class)
	assert.Equal(t, "config", se.Stage)
	assert.Equal(t, cause, se.Err)
}

// A URL with credentials (and an insecure scheme) is rejected by semindex.New;
// the review still completes and the manifest names the config class.
func TestSemanticPrefetch_ConstructionErrorRecordsDegradeReason(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := semanticFanoutRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)
	enableSemantic(cfg, "http://user:pass@127.0.0.1:1")

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	assert.Equal(t, 2, res.Summary.Succeeded, "the review completes normally")

	_, m := readSemanticManifest(t, res.Dir)
	require.NotNil(t, m.Prefetch)
	assert.Equal(t, string(semanticDegradeConfig), m.Prefetch.DegradeReason)
}

// block skips retrieval only when the queries or the scan are dirty: clean
// queries under a working scanner pass through unchanged, or block would
// silently yield zero similarity snippets on every run.
func TestSemanticQueryGuard_BlockPassesCleanQueries(t *testing.T) {
	hideGitleaks(t)
	inner := &recordingRetriever{}
	g := &guardedRetriever{policy: secretGuardBlock, inner: inner}
	clean := []string{"func ReadStore(path string) (string, error)", "func ReadCache(key string) (string, error)"}
	var err error
	stderr := captureStderr(t, func() {
		_, err = g.Retrieve(context.Background(), clean, 3)
	})
	require.NoError(t, err)
	assert.Equal(t, 1, inner.calls, "clean queries must be retrieved")
	assert.Equal(t, clean, inner.queries, "clean queries are sent unchanged")
	assert.Empty(t, stderr)
}

// readSemanticManifest decodes a finished review's manifest.json, raw and typed.
func readSemanticManifest(t *testing.T, dir string) ([]byte, payload.Manifest) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	var m payload.Manifest
	require.NoError(t, json.Unmarshal(raw, &m))
	return raw, m
}

// Epic 35.16.12.1 T3 (AC2, AC4): an endpoint configured but unreachable still
// completes the review, ships zero similarity snippets, counts the failure once
// and records one degrade reason in the manifest.
func TestSemanticPrefetch_ClosedPortCompletesWithOneDegradeReason(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := semanticFanoutRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)
	enableSemantic(cfg, "http://127.0.0.1:1") // port 1 is never listened on
	counter := metrics.DefaultRegistry.Counter(semindex.UnreachableMetric)
	before := counter.Value()

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)
	assert.Equal(t, 2, res.Summary.Succeeded, "the review completes normally")

	assert.Equal(t, int64(1), counter.Value()-before, "one failed retrieval is counted once")
	raw, m := readSemanticManifest(t, res.Dir)
	require.NotNil(t, m.Prefetch)
	assert.Equal(t, string(semindex.ClassUnreachable), m.Prefetch.DegradeReason)
	assert.Equal(t, 1, strings.Count(string(raw), `"degrade_reason"`), "one degrade reason is recorded")
	rendered, err := os.ReadFile(filepath.Join(res.Dir, "payload", "blocks.txt"))
	require.NoError(t, err)
	assert.NotContains(t, string(rendered), "similarity to", "zero similarity snippets ship")
}

func TestSemanticPrefetch_OffRunManifestHasNoDegradeReason(t *testing.T) {
	t.Setenv("ATCR_TEST_KEY", "secret")
	repo, base, head := semanticFanoutRepo(t)
	srv := mockProvider(t)
	cfg := twoAgentConfig(srv.URL)

	res, err := RunReview(context.Background(), llmclient.New(), cfg, reviewReq(repo, repo, base, head))
	require.NoError(t, err)

	raw, m := readSemanticManifest(t, res.Dir)
	require.NotNil(t, m.Prefetch, "PRECONDITION: the run had a range to prefetch from")
	assert.NotContains(t, string(raw), "degrade_reason")
}
