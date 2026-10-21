package semindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The wire contract, pinned to samestrin/llm-semantic-axi at da1f9eb
// (internal/semantic/embedder.go, internal/semantic/storage_qdrant.go). These
// literals are deliberately NOT the package constants: a change to either side
// must fail here rather than move both in lockstep.
const (
	pinnedModel      = "Qwen/Qwen3-Embedding-4B"
	pinnedVectorSize = 2560
	pinnedEmbedPath  = "/v1/embeddings"
	pinnedQueryPath  = "/collections/%s/points/query"
	pinnedAuthHeader = "api-key"
	pinnedKeyEnv     = "QDRANT_API_KEY"
	repoRoot         = "/Users/dev/GitHub/atcr"
	testCollection   = "llm_semantic"
)

func TestContractConstants(t *testing.T) {
	assert.Equal(t, pinnedModel, EmbeddingModel)
	assert.Equal(t, pinnedVectorSize, VectorSize)
	assert.Equal(t, pinnedKeyEnv, APIKeyEnv)
	assert.Equal(t, "atcr_semindex_configured_unreachable_total", UnreachableMetric)
}

// vec returns a pinned-size vector whose first element is seed, so the qdrant
// fake can tell which query a search request belongs to.
func vec(seed float64) []float64 {
	v := make([]float64, pinnedVectorSize)
	v[0] = seed
	return v
}

// embeddingReply is the OpenAI-compatible body llm-semantic-axi's embedder
// reads. data is returned in REVERSE input order so the test proves the client
// maps by data[i].index, not by position.
func embeddingReply(n int) string {
	type item struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	}
	items := make([]item, 0, n)
	for i := n - 1; i >= 0; i-- {
		items = append(items, item{Object: "embedding", Index: i, Embedding: vec(float64(i + 1))})
	}
	b, _ := json.Marshal(map[string]any{
		"id": "emb-1", "object": "list", "created": 1, "model": pinnedModel,
		"data": items, "usage": map[string]int{"prompt_tokens": 3, "total_tokens": 3},
	})
	return string(b)
}

// point is one qdrant hit with the llm-semantic-axi payload fields.
func point(id int, score float64, file string, start, end int, name, content string) map[string]any {
	return map[string]any{
		"id": id, "version": 1, "score": score,
		"payload": map[string]any{
			"file_path": file, "start_line": start, "end_line": end, "content": content,
			"name": name, "signature": "func " + name + "()", "language": "go", "type": "function",
			"chunk_id": fmt.Sprintf("c%d", id), "content_hash": "h", "domain": "code",
		},
	}
}

func queryReply(points ...map[string]any) string {
	if points == nil {
		points = []map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{
		"result": map[string]any{"points": points}, "status": "ok", "time": 0.001,
	})
	return string(b)
}

// fake is an httptest server playing both the embedding endpoint and qdrant.
type fake struct {
	srv *httptest.Server

	mu       sync.Mutex
	embedReq []map[string]any
	queryReq []map[string]any
	headers  []http.Header
	paths    []string

	embed func(w http.ResponseWriter, r *http.Request, body map[string]any)
	query func(w http.ResponseWriter, r *http.Request, body map[string]any)
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{}
	f.embed = func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		in, _ := body["input"].([]any)
		_, _ = io.WriteString(w, embeddingReply(len(in)))
	}
	f.query = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		_, _ = io.WriteString(w, queryReply())
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		isEmbed := r.URL.Path == pinnedEmbedPath
		if isEmbed {
			f.embedReq = append(f.embedReq, body)
		} else {
			f.queryReq = append(f.queryReq, body)
		}
		embed, query := f.embed, f.query
		f.mu.Unlock()
		if isEmbed {
			embed(w, r, body)
			return
		}
		query(w, r, body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// seedOf reads back which query a qdrant search vector came from.
func seedOf(body map[string]any) int {
	q, _ := body["query"].([]any)
	if len(q) == 0 {
		return -1
	}
	s, _ := q[0].(float64)
	return int(s)
}

type harness struct {
	f   *fake
	reg *metrics.Registry
	c   *Client
}

func (h harness) unreachable() int64 { return h.reg.Counter(UnreachableMetric).Value() }

func newHarness(t *testing.T, mut func(*Config)) harness {
	t.Helper()
	f := newFake(t)
	reg := metrics.NewRegistry()
	cfg := Config{
		EmbeddingEndpoint: f.srv.URL,
		QdrantEndpoint:    f.srv.URL,
		Collection:        testCollection,
		RepoRoot:          repoRoot,
		CallTimeout:       2 * time.Second,
		TotalBudget:       5 * time.Second,
		Metrics:           reg,
		Getenv:            func(string) string { return "" },
	}
	if mut != nil {
		mut(&cfg)
	}
	c, err := New(cfg)
	require.NoError(t, err)
	return harness{f: f, reg: reg, c: c}
}

func TestRetrieve_RequestShapeIsPinnedToTheContract(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.Getenv = func(k string) string {
			if k == pinnedKeyEnv {
				return "env-secret"
			}
			return ""
		}
	})
	_, err := h.c.Retrieve(context.Background(), []string{"func A()", "func B()"}, 3)
	require.NoError(t, err)

	require.Len(t, h.f.embedReq, 1, "all queries go in ONE embedding call")
	assert.Equal(t, map[string]any{"input": []any{"func A()", "func B()"}, "model": pinnedModel}, h.f.embedReq[0])

	require.Len(t, h.f.queryReq, 2, "one qdrant search per query")
	for i, body := range h.f.queryReq {
		assert.Equal(t, i+1, seedOf(body), "search %d carries embedding %d", i, i)
		q := body["query"].([]any)
		assert.Len(t, q, pinnedVectorSize)
		assert.Equal(t, float64(3), body["limit"])
		assert.Equal(t, true, body["with_payload"])
		assert.NotContains(t, body, "using", "one unnamed dense vector: no `using`")
		assert.Len(t, body, 3, "no keys beyond query/limit/with_payload")
	}

	wantQuery := "POST " + fmt.Sprintf(pinnedQueryPath, testCollection)
	assert.Equal(t, []string{"POST " + pinnedEmbedPath, wantQuery, wantQuery}, h.f.paths)

	// The qdrant key comes from the environment, goes to qdrant only.
	assert.Empty(t, h.f.headers[0].Get(pinnedAuthHeader), "the key never goes to the embedding endpoint")
	assert.Equal(t, "env-secret", h.f.headers[1].Get(pinnedAuthHeader))
	assert.Equal(t, "env-secret", h.f.headers[2].Get(pinnedAuthHeader))
	for _, hd := range h.f.headers {
		assert.Equal(t, "application/json", hd.Get("Content-Type"))
		assert.Empty(t, hd.Get("Authorization"))
	}
	assert.Zero(t, h.unreachable())
}

func TestRetrieve_NoKeyInEnvironmentSendsNoAuthHeader(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	require.NoError(t, err)
	for _, hd := range h.f.headers {
		_, ok := hd[http.CanonicalHeaderKey(pinnedAuthHeader)]
		assert.False(t, ok)
	}
}

func TestRetrieve_CollectionNameIsPathEscaped(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Collection = "a b" })
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	require.NoError(t, err)
	assert.Equal(t, "POST /collections/a b/points/query", h.f.paths[1])
}

func TestRetrieve_DecodeOrderingAndPathMapping(t *testing.T) {
	h := newHarness(t, nil)
	h.f.query = func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		switch seedOf(body) {
		case 1:
			_, _ = io.WriteString(w, queryReply(
				point(1, 0.70, repoRoot+"/internal/mcp/handlers.go", 10, 20, "Handle", "func Handle() {}"),
				point(2, 0.90, repoRoot+"/cmd/atcr/main.go", 1, 5, "main", "func main() {}"),
				point(3, 0.99, "/Users/dev/GitHub/other/x.go", 1, 2, "X", "outside"),
			))
		case 2:
			_, _ = io.WriteString(w, queryReply(
				// The same span as hit 1, scored higher: dedupe keeps the max.
				point(4, 0.80, repoRoot+"/internal/mcp/handlers.go", 10, 20, "Handle", "func Handle() {}"),
				// Indexed on another machine: matched on the repo-name suffix.
				point(5, 0.75, "/home/ci/src/atcr/internal/log/log.go", 3, 4, "New", "func New() {}"),
				// Ties on score break by path, then start line.
				point(6, 0.60, repoRoot+"/b.go", 9, 9, "B2", "b2"),
				point(7, 0.60, repoRoot+"/b.go", 2, 2, "B1", "b1"),
				point(8, 0.60, repoRoot+"/a.go", 5, 5, "A", "a"),
			))
		}
	}
	got, err := h.c.Retrieve(context.Background(), []string{"q1", "q2"}, 10)
	require.NoError(t, err)

	want := []Snippet{
		{Path: "cmd/atcr/main.go", StartLine: 1, EndLine: 5, Name: "main", Content: "func main() {}", Score: 0.90},
		{Path: "internal/mcp/handlers.go", StartLine: 10, EndLine: 20, Name: "Handle", Content: "func Handle() {}", Score: 0.80},
		{Path: "internal/log/log.go", StartLine: 3, EndLine: 4, Name: "New", Content: "func New() {}", Score: 0.75},
		{Path: "a.go", StartLine: 5, EndLine: 5, Name: "A", Content: "a", Score: 0.60},
		{Path: "b.go", StartLine: 2, EndLine: 2, Name: "B1", Content: "b1", Score: 0.60},
		{Path: "b.go", StartLine: 9, EndLine: 9, Name: "B2", Content: "b2", Score: 0.60},
	}
	assert.Equal(t, want, got)
	assert.Zero(t, h.unreachable())
}

func TestRetrieve_TruncatesToTopKAfterMerge(t *testing.T) {
	h := newHarness(t, nil)
	h.f.query = func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		s := seedOf(body)
		_, _ = io.WriteString(w, queryReply(
			point(s*10, 0.1*float64(s), fmt.Sprintf("%s/f%d.go", repoRoot, s), 1, 1, "n", "c"),
			point(s*10+1, 0.05*float64(s), fmt.Sprintf("%s/g%d.go", repoRoot, s), 1, 1, "n", "c"),
		))
	}
	got, err := h.c.Retrieve(context.Background(), []string{"a", "b", "c"}, 2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "f3.go", got[0].Path)
	assert.Equal(t, "f2.go", got[1].Path)
}

func TestRetrieve_EmptyQueriesMakeNoCall(t *testing.T) {
	h := newHarness(t, nil)
	got, err := h.c.Retrieve(context.Background(), nil, 5)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, h.f.paths)
	assert.Zero(t, h.unreachable())
}

func TestRetrieve_EmptyResultIsNotAFailure(t *testing.T) {
	h := newHarness(t, nil)
	got, err := h.c.Retrieve(context.Background(), []string{"q"}, 5)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Zero(t, h.unreachable())
}

func TestMapPath(t *testing.T) {
	cases := []struct {
		name, root, in, want string
		ok                   bool
	}{
		{"prefix strip", "/r/atcr", "/r/atcr/internal/x.go", "internal/x.go", true},
		{"root with trailing slash", "/r/atcr/", "/r/atcr/x.go", "x.go", true},
		{"suffix on first repo-name segment", "/w/atcr", "/Users/u/atcr/cmd/atcr/main.go", "cmd/atcr/main.go", true},
		{"relative path kept", "/r/atcr", "internal/x.go", "internal/x.go", true},
		{"relative dot path cleaned", "/r/atcr", "./internal/x.go", "internal/x.go", true},
		{"outside the repo", "/r/atcr", "/r/other/x.go", "", false},
		{"sibling dir sharing a prefix", "/r/atcr", "/r/atcr-old/x.go", "", false},
		{"the root itself", "/r/atcr", "/r/atcr", "", false},
		{"escape via dotdot", "/r/atcr", "/r/atcr/../other/x.go", "", false},
		{"relative escape", "/r/atcr", "../x.go", "", false},
		{"empty", "/r/atcr", "", "", false},
		{"windows separators", "/r/atcr", `C:\Users\u\atcr\internal\x.go`, "internal/x.go", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mapPath(tc.root, tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRetrieve_DropsMalformedHits(t *testing.T) {
	h := newHarness(t, nil)
	h.f.query = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		_, _ = io.WriteString(w, queryReply(
			point(1, 0.9, repoRoot+"/ok.go", 1, 2, "ok", "c"),
			point(2, 0.9, repoRoot+"/zero.go", 0, 2, "z", "c"),
			point(3, 0.9, repoRoot+"/inverted.go", 5, 2, "i", "c"),
			point(4, 0.9, "", 1, 2, "e", "c"),
		))
	}
	got, err := h.c.Retrieve(context.Background(), []string{"q"}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "ok.go", got[0].Path)
}

// TestRetrieve_ErrorMappingAndCounter is the table the task names: every
// failure class maps to its Class, and each increments the
// configured-but-unreachable counter exactly once.
func TestRetrieve_ErrorMappingAndCounter(t *testing.T) {
	status := func(code int) func(http.ResponseWriter, *http.Request, map[string]any) {
		return func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"status":{"error":"x"}}`)
		}
	}
	body := func(s string) func(http.ResponseWriter, *http.Request, map[string]any) {
		return func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { _, _ = io.WriteString(w, s) }
	}
	cases := []struct {
		name  string
		embed func(http.ResponseWriter, *http.Request, map[string]any)
		query func(http.ResponseWriter, *http.Request, map[string]any)
		topK  int
		want  Class
	}{
		{name: "embedding non-200", embed: status(500), want: ClassStatus},
		{name: "embedding malformed json", embed: body(`{"data":`), want: ClassDecode},
		{name: "embedding too few vectors", embed: body(embeddingReply(1)), want: ClassContract},
		{name: "embedding wrong vector size", embed: body(`{"data":[{"index":0,"embedding":[0.1,0.2]},{"index":1,"embedding":[0.1]}]}`), want: ClassContract},
		{name: "embedding index out of range", embed: body(`{"data":[{"index":5,"embedding":[]},{"index":1,"embedding":[]}]}`), want: ClassContract},
		{name: "embedding duplicate index", embed: body(strings.Replace(embeddingReply(2), `"index":0`, `"index":1`, 1)), want: ClassContract},
		{name: "collection missing", query: status(404), want: ClassNotFound},
		{name: "qdrant non-200", query: status(503), want: ClassStatus},
		{name: "qdrant malformed json", query: body(`not json`), want: ClassDecode},
		{name: "qdrant no result object", query: body(`{"status":"ok"}`), want: ClassContract},
		{name: "invalid topK", topK: -1, want: ClassInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			if tc.embed != nil {
				h.f.embed = tc.embed
			}
			if tc.query != nil {
				h.f.query = tc.query
			}
			topK := tc.topK
			if topK == 0 {
				topK = 3
			}
			got, err := h.c.Retrieve(context.Background(), []string{"q1", "q2"}, topK)
			require.Error(t, err)
			assert.Nil(t, got)
			var se *Error
			require.True(t, errors.As(err, &se), "error is a *semindex.Error: %v", err)
			assert.Equal(t, tc.want, se.Class, "err: %v", err)
			assert.Equal(t, int64(1), h.unreachable(), "exactly one increment per failed retrieval")
		})
	}
}

func TestRetrieve_UnreachableHost(t *testing.T) {
	h := newHarness(t, nil)
	url := h.f.srv.URL
	h.f.srv.Close()
	_ = url
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ClassUnreachable, se.Class)
	assert.Equal(t, int64(1), h.unreachable())
}

func TestRetrieve_CounterIncrementsOnEveryFailure(t *testing.T) {
	h := newHarness(t, nil)
	h.f.embed = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { w.WriteHeader(500) }
	for i := 0; i < 3; i++ {
		_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
		require.Error(t, err)
	}
	assert.Equal(t, int64(3), h.unreachable())
}

func TestRetrieve_CounterDefaultsToTheProcessRegistry(t *testing.T) {
	f := newFake(t)
	f.embed = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { w.WriteHeader(500) }
	c, err := New(Config{EmbeddingEndpoint: f.srv.URL, QdrantEndpoint: f.srv.URL, Collection: "c", RepoRoot: repoRoot})
	require.NoError(t, err)
	before := metrics.Counter(UnreachableMetric).Value()
	_, err = c.Retrieve(context.Background(), []string{"q"}, 1)
	require.Error(t, err)
	assert.Equal(t, before+1, metrics.Counter(UnreachableMetric).Value())
}

func TestRetrieve_PerCallTimeout(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h := newHarness(t, func(c *Config) {
		c.CallTimeout = 50 * time.Millisecond
		c.TotalBudget = 10 * time.Second
	})
	h.f.embed = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	start := time.Now()
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	assert.Less(t, time.Since(start), 2*time.Second)
	var se *Error
	require.True(t, errors.As(err, &se), "%v", err)
	assert.Equal(t, ClassTimeout, se.Class)
	assert.Contains(t, se.Error(), "per-call timeout")
	assert.Equal(t, int64(1), h.unreachable())
}

func TestRetrieve_TotalBudget(t *testing.T) {
	// Every call finishes inside the per-call timeout, but the sum does not
	// fit the total budget.
	h := newHarness(t, func(c *Config) {
		c.CallTimeout = 1 * time.Second
		c.TotalBudget = 150 * time.Millisecond
	})
	var calls atomic.Int32
	h.f.query = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		calls.Add(1)
		select {
		case <-time.After(60 * time.Millisecond):
			_, _ = io.WriteString(w, queryReply())
		case <-r.Context().Done():
		}
	}
	start := time.Now()
	_, err := h.c.Retrieve(context.Background(), []string{"a", "b", "c", "d", "e", "f"}, 1)
	assert.Less(t, time.Since(start), 2*time.Second)
	var se *Error
	require.True(t, errors.As(err, &se), "%v", err)
	assert.Equal(t, ClassTimeout, se.Class)
	assert.Contains(t, se.Error(), "total budget")
	assert.Less(t, calls.Load(), int32(6), "the budget stops the run before every query is searched")
	assert.Equal(t, int64(1), h.unreachable())
}

func TestRetrieve_CallerCancellation(t *testing.T) {
	h := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.c.Retrieve(ctx, []string{"q"}, 1)
	var se *Error
	require.True(t, errors.As(err, &se), "%v", err)
	assert.Equal(t, ClassCanceled, se.Class)
	assert.Equal(t, int64(1), h.unreachable())
}

func TestVetURL(t *testing.T) {
	cases := []struct {
		url      string
		insecure bool
		ok       bool
	}{
		{"https://qdrant.example.com", false, true},
		{"HTTPS://qdrant.example.com", false, true},
		{"http://127.0.0.1:6333", false, true},
		{"http://127.9.9.9:6333", false, true},
		{"http://localhost:6333", false, true},
		{"http://LOCALHOST:6333", false, true},
		{"http://[::1]:6333", false, true},
		{"http://nucleus.lan:6333", false, false},
		{"http://10.0.0.5:6333", false, false},
		{"http://localhost.evil.com", false, false},
		{"http://nucleus.lan:6333", true, true},
		{"ftp://127.0.0.1", true, false},
		{"file:///etc/passwd", true, false},
		{"https://", false, false},
		{"://bad", false, false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s insecure=%v", tc.url, tc.insecure), func(t *testing.T) {
			err := vetURL(tc.url, tc.insecure)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestRetrieve_RefusesPlainHTTPToNonLoopback(t *testing.T) {
	var hits atomic.Int32
	reg := metrics.NewRegistry()
	c, err := New(Config{
		// Never dialed: the refusal happens before any request leaves.
		EmbeddingEndpoint: "http://embed.invalid:8080",
		QdrantEndpoint:    "http://qdrant.invalid:6333",
		Collection:        "c",
		RepoRoot:          repoRoot,
		Metrics:           reg,
	})
	require.NoError(t, err)
	_, err = c.Retrieve(context.Background(), []string{"q"}, 1)
	var se *Error
	require.True(t, errors.As(err, &se), "%v", err)
	assert.Equal(t, ClassInsecureScheme, se.Class)
	assert.Zero(t, hits.Load())
	assert.Equal(t, int64(1), reg.Counter(UnreachableMetric).Value())
}

func TestRetrieve_RevetsSchemeOnEveryRedirectHop(t *testing.T) {
	h := newHarness(t, nil)
	h.f.embed = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		http.Redirect(w, r, "http://downgrade.invalid/v1/embeddings", http.StatusTemporaryRedirect)
	}
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	var se *Error
	require.True(t, errors.As(err, &se), "%v", err)
	assert.Equal(t, ClassInsecureScheme, se.Class)
	assert.Contains(t, se.Error(), "redirect")
	assert.Equal(t, int64(1), h.unreachable())
}

func TestRetrieve_SameHostRedirectIsFollowed(t *testing.T) {
	h := newHarness(t, nil)
	inner := h.f.embed
	h.f.embed = func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Query().Get("moved") == "" {
			http.Redirect(w, r, pinnedEmbedPath+"?moved=1", http.StatusTemporaryRedirect)
			return
		}
		inner(w, r, body)
	}
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	require.NoError(t, err)
}

func TestRetrieve_KeyIsStrippedOnCrossHostRedirect(t *testing.T) {
	var gotKey atomic.Value
	gotKey.Store("unset")
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.Header.Get(pinnedAuthHeader))
		_, _ = io.WriteString(w, queryReply())
	}))
	t.Cleanup(other.Close)
	// 127.0.0.1 vs localhost: both loopback (scheme passes) but distinct hosts.
	target := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)

	h := newHarness(t, func(c *Config) {
		c.Getenv = func(string) string { return "env-secret" }
	})
	h.f.query = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		http.Redirect(w, r, target+r.URL.Path, http.StatusTemporaryRedirect)
	}
	_, err := h.c.Retrieve(context.Background(), []string{"q"}, 1)
	require.NoError(t, err)
	assert.Equal(t, "", gotKey.Load(), "the api-key never follows a redirect to another host")
}

func TestNew_RejectsBadConfig(t *testing.T) {
	ok := Config{EmbeddingEndpoint: "https://e", QdrantEndpoint: "https://q", Collection: "c", RepoRoot: "/r"}
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"no embedding endpoint", func(c *Config) { c.EmbeddingEndpoint = "" }},
		{"no qdrant endpoint", func(c *Config) { c.QdrantEndpoint = " " }},
		{"no collection", func(c *Config) { c.Collection = "" }},
		{"no repo root", func(c *Config) { c.RepoRoot = "" }},
		{"relative repo root", func(c *Config) { c.RepoRoot = "rel" }},
		{"unparseable endpoint", func(c *Config) { c.QdrantEndpoint = "://x" }},
		// A credential in a config-file URL is refused outright: the key comes
		// from the environment only.
		{"userinfo in qdrant endpoint", func(c *Config) { c.QdrantEndpoint = "https://u:p@q" }},
		{"userinfo in embedding endpoint", func(c *Config) { c.EmbeddingEndpoint = "https://tok@e" }},
		{"query string in endpoint", func(c *Config) { c.QdrantEndpoint = "https://q?api-key=x" }},
		{"negative timeout", func(c *Config) { c.CallTimeout = -1 }},
		{"negative budget", func(c *Config) { c.TotalBudget = -1 }},
	}
	_, err := New(ok)
	require.NoError(t, err)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ok
			tc.mut(&cfg)
			_, err := New(cfg)
			assert.Error(t, err)
		})
	}
}

func TestNew_Defaults(t *testing.T) {
	c, err := New(Config{EmbeddingEndpoint: "https://e/", QdrantEndpoint: "https://q", Collection: "c", RepoRoot: "/r"})
	require.NoError(t, err)
	assert.Equal(t, DefaultCallTimeout, c.cfg.CallTimeout)
	assert.Equal(t, DefaultTotalBudget, c.cfg.TotalBudget)
	assert.Equal(t, "https://e/v1/embeddings", c.embedURL)
	assert.Same(t, metrics.DefaultRegistry, c.cfg.Metrics)
}

func TestError_Unwrap(t *testing.T) {
	inner := errors.New("boom")
	e := &Error{Class: ClassStatus, Stage: "qdrant", Err: inner}
	assert.ErrorIs(t, e, inner)
	assert.Contains(t, e.Error(), "qdrant")
	assert.Contains(t, e.Error(), string(ClassStatus))
}

func TestClient_SatisfiesRetriever(t *testing.T) {
	var _ Retriever = (*Client)(nil)
}
