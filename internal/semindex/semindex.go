// Package semindex is atcr's one seam onto an external similarity index: an
// OpenAI-compatible embedding endpoint plus a qdrant collection built by
// samestrin/llm-semantic-axi (epic 35.16.12). Everything about embeddings,
// qdrant and HTTP stays behind Retrieve, so a change in the producer touches
// this file and nothing above it.
//
// The index is a foreign service, not an atcr artifact: atcr never builds it
// and never assumes it exists. A caller that has no semantic config must not
// construct a Client at all, so the default path makes no network call and
// logs nothing. A configured Client fails open — every failure returns a
// classified *Error and zero snippets, never a failed review — but never
// silently: each failed Retrieve increments UnreachableMetric once.
//
// The package is a stdlib-only leaf: net/http plus internal/log and
// internal/metrics, nothing above them (internal/boundaries_test.go).
package semindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
)

// The wire contract, read from llm-semantic-axi at da1f9eb
// (internal/semantic/embedder.go, internal/semantic/storage_qdrant.go). The
// tests pin these against their own literals, so a producer change fails a
// test here rather than an `atcr review`.
const (
	// EmbeddingModel is the model the index was embedded with. Query vectors
	// must come from the same model or the cosine scores mean nothing.
	EmbeddingModel = "Qwen/Qwen3-Embedding-4B"
	// VectorSize is EmbeddingModel's dimension; a reply of any other size is a
	// contract break, not a usable vector.
	VectorSize = 2560
	// APIKeyEnv is the ONLY source of the qdrant API key. Config has no
	// credential field, so a key can never come from the repo config file.
	APIKeyEnv = "QDRANT_API_KEY"

	embedPath  = "/v1/embeddings"
	apiKeyHdr  = "api-key"
	maxRespLen = 16 << 20 // 40 queries x 2560 floats is ~2 MB; 16 MB bounds a hostile reply
)

// UnreachableMetric counts Retrieve calls on a CONFIGURED client that failed,
// for any reason. Zero snippets from a retriever that is down looks exactly
// like zero snippets from one that found nothing similar, the same blind spot
// internal/reconcile's tier4UnavailableMetric closes for Tier 4: the counter
// is the durable signal that separates the two.
const UnreachableMetric = "atcr_semindex_configured_unreachable_total"

// Defaults for a zero Config timeout. The call timeout bounds one HTTP
// exchange; the budget bounds the whole Retrieve (one embedding call plus one
// search per query), so many fast calls still cannot stall a review.
const (
	DefaultCallTimeout = 5 * time.Second
	DefaultTotalBudget = 15 * time.Second
)

// Snippet is one similarity hit, mapped to a repo-relative path.
type Snippet struct {
	Path      string // repo-relative, slash-separated
	StartLine int    // 1-based, inclusive
	EndLine   int    // 1-based, inclusive
	Name      string // symbol name from the index, may be empty
	Content   string // the indexed chunk text; stale until checked against HEAD
	Score     float64
}

// Retriever is the seam the payload layer depends on.
type Retriever interface {
	Retrieve(ctx context.Context, queries []string, topK int) ([]Snippet, error)
}

// Config configures a Client. It deliberately carries no credential: see
// APIKeyEnv.
type Config struct {
	EmbeddingEndpoint string // base URL; "/v1/embeddings" is appended
	QdrantEndpoint    string // base URL of the qdrant REST API (port 6333)
	Collection        string
	// RepoRoot is the absolute path of the repository under review. Hits are
	// mapped relative to it, and hits outside it are dropped.
	RepoRoot string
	// AllowInsecureHTTP permits plain http to a non-loopback host (a LAN
	// qdrant). Loopback http is always allowed.
	AllowInsecureHTTP bool
	CallTimeout       time.Duration // 0 means DefaultCallTimeout
	TotalBudget       time.Duration // 0 means DefaultTotalBudget
	// Metrics receives UnreachableMetric; nil means metrics.DefaultRegistry.
	Metrics *metrics.Registry
	// Getenv reads APIKeyEnv; nil means os.Getenv. Tests inject it.
	Getenv func(string) string
}

// Class names a failure kind. Every class is a fail-open degrade; the class
// exists so the caller can report WHY, not to decide whether to fail.
type Class string

const (
	ClassInvalidArgument Class = "invalid-argument"
	ClassInsecureScheme  Class = "insecure-scheme"
	ClassUnreachable     Class = "unreachable"
	ClassTimeout         Class = "timeout"
	ClassCanceled        Class = "canceled"
	ClassNotFound        Class = "not-found" // qdrant 404: the collection is missing
	ClassStatus          Class = "status"    // any other non-200
	ClassDecode          Class = "decode"
	ClassContract        Class = "contract" // decoded, but not the pinned shape
)

// Error is every error Retrieve returns.
type Error struct {
	Class Class
	Stage string // "embedding" or "qdrant"
	Err   error
}

func (e *Error) Error() string {
	return fmt.Sprintf("semindex: %s %s: %v", e.Stage, e.Class, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

var errInsecure = errors.New("plain http to a non-loopback host is refused (set allow_insecure_http to permit it)")

// Client is the qdrant + embedding Retriever. Build it with New.
type Client struct {
	cfg      Config
	embedURL string
	queryURL string
	apiKey   string
	http     *http.Client
}

// New validates cfg and returns a Client. It does no I/O. The scheme rule is
// enforced per request (and per redirect hop) rather than here, so a refused
// scheme is a counted Retrieve failure like any other.
func New(cfg Config) (*Client, error) {
	for _, ep := range []struct{ name, v string }{
		{"embedding endpoint", cfg.EmbeddingEndpoint},
		{"qdrant endpoint", cfg.QdrantEndpoint},
	} {
		if strings.TrimSpace(ep.v) == "" {
			return nil, fmt.Errorf("semindex: %s is required", ep.name)
		}
		u, err := url.Parse(ep.v)
		if err != nil {
			return nil, fmt.Errorf("semindex: %s: %w", ep.name, err)
		}
		// A credential in a config-file URL is refused, not stripped: the user
		// should learn the key does not belong there.
		if u.User != nil || u.RawQuery != "" {
			return nil, fmt.Errorf("semindex: %s must not carry credentials or a query string; set %s instead", ep.name, APIKeyEnv)
		}
	}
	if strings.TrimSpace(cfg.Collection) == "" {
		return nil, errors.New("semindex: collection is required")
	}
	if !filepath.IsAbs(cfg.RepoRoot) && !path.IsAbs(toSlash(cfg.RepoRoot)) {
		return nil, fmt.Errorf("semindex: repo root %q must be an absolute path", cfg.RepoRoot)
	}
	if cfg.CallTimeout < 0 || cfg.TotalBudget < 0 {
		return nil, errors.New("semindex: timeouts must not be negative")
	}
	if cfg.CallTimeout == 0 {
		cfg.CallTimeout = DefaultCallTimeout
	}
	if cfg.TotalBudget == 0 {
		cfg.TotalBudget = DefaultTotalBudget
	}
	if cfg.Metrics == nil {
		cfg.Metrics = metrics.DefaultRegistry
	}
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	insecure := cfg.AllowInsecureHTTP
	return &Client{
		cfg:      cfg,
		embedURL: strings.TrimRight(cfg.EmbeddingEndpoint, "/") + embedPath,
		queryURL: strings.TrimRight(cfg.QdrantEndpoint, "/") + "/collections/" + url.PathEscape(cfg.Collection) + "/points/query",
		apiKey:   strings.TrimSpace(cfg.Getenv(APIKeyEnv)),
		http: &http.Client{
			// vetURL checks only the initial URL; re-vet every hop so a 307/308
			// (which replays the POST body) cannot downgrade to plaintext.
			// Mirrors internal/telemetry's CheckRedirect. net/http strips
			// Authorization on a cross-host hop but not a custom header, so the
			// api-key is dropped here by hand.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if err := vetURL(req.URL.String(), insecure); err != nil {
					return fmt.Errorf("redirect: %w", err)
				}
				if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
					req.Header.Del(apiKeyHdr)
				}
				return nil
			},
		},
	}, nil
}

// vetURL reports whether raw may be sent to: https anywhere, http only to a
// loopback host unless allowInsecure. The host is judged literally and never
// resolved, so this does no DNS.
func vetURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if allowInsecure || isLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%w: %q", errInsecure, raw)
	default:
		return fmt.Errorf("%w: unsupported scheme %q", errInsecure, u.Scheme)
	}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Retrieve embeds every query in one call, searches the collection once per
// query with limit topK, maps each hit to a repo-relative path (dropping hits
// outside the repo or with no usable span), merges duplicates keeping the best
// score, and returns at most topK snippets ordered by score descending, then
// path, then start line. No queries means no call and no error. An empty
// result is not a failure; every error is an *Error. A topK<=0 is the caller's
// bug, so it is returned uncounted; every other error is counted once.
func (c *Client) Retrieve(ctx context.Context, queries []string, topK int) ([]Snippet, error) {
	if topK <= 0 {
		return nil, &Error{Class: ClassInvalidArgument, Stage: "request", Err: fmt.Errorf("topK must be positive, got %d", topK)}
	}
	if len(queries) == 0 {
		return nil, nil
	}
	snips, err := c.retrieve(ctx, queries, topK)
	if err != nil {
		c.cfg.Metrics.Counter(UnreachableMetric).Inc()
		log.FromContext(ctx).Warn("semantic retrieval failed; continuing without similarity snippets",
			"error", err.Error(), "collection", c.cfg.Collection)
		return nil, err
	}
	return snips, nil
}

func (c *Client) retrieve(ctx context.Context, queries []string, topK int) ([]Snippet, error) {
	budget, cancel := context.WithTimeout(ctx, c.cfg.TotalBudget)
	defer cancel()

	vecs, err := c.embed(ctx, budget, queries)
	if err != nil {
		return nil, err
	}
	type span struct {
		path       string
		start, end int
	}
	best := map[span]Snippet{}
	for _, v := range vecs {
		hits, err := c.search(ctx, budget, v, topK)
		if err != nil {
			return nil, err
		}
		for _, s := range hits {
			k := span{s.Path, s.StartLine, s.EndLine}
			if old, ok := best[k]; !ok || s.Score > old.Score {
				best[k] = s
			}
		}
	}
	out := make([]Snippet, 0, len(best))
	for _, s := range best {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.StartLine < b.StartLine
	})
	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

type embedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) embed(caller, budget context.Context, queries []string) ([][]float64, error) {
	const stage = "embedding"
	var resp embedResponse
	if err := c.post(caller, budget, stage, c.embedURL, false, embedRequest{Input: queries, Model: EmbeddingModel}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) != len(queries) {
		return nil, &Error{Class: ClassContract, Stage: stage, Err: fmt.Errorf("got %d vectors for %d inputs", len(resp.Data), len(queries))}
	}
	out := make([][]float64, len(queries))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(queries) || out[d.Index] != nil {
			return nil, &Error{Class: ClassContract, Stage: stage, Err: fmt.Errorf("bad or duplicate data index %d", d.Index)}
		}
		if len(d.Embedding) != VectorSize {
			return nil, &Error{Class: ClassContract, Stage: stage, Err: fmt.Errorf("vector %d has %d dimensions, want %d", d.Index, len(d.Embedding), VectorSize)}
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

type queryRequest struct {
	Query       []float64 `json:"query"`
	Limit       int       `json:"limit"`
	WithPayload bool      `json:"with_payload"`
}

type queryResponse struct {
	Result *struct {
		Points []struct {
			Score   float64 `json:"score"`
			Payload struct {
				FilePath  string `json:"file_path"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
				Content   string `json:"content"`
				Name      string `json:"name"`
			} `json:"payload"`
		} `json:"points"`
	} `json:"result"`
}

func (c *Client) search(caller, budget context.Context, v []float64, topK int) ([]Snippet, error) {
	const stage = "qdrant"
	var resp queryResponse
	if err := c.post(caller, budget, stage, c.queryURL, true, queryRequest{Query: v, Limit: topK, WithPayload: true}, &resp); err != nil {
		return nil, err
	}
	if resp.Result == nil {
		return nil, &Error{Class: ClassContract, Stage: stage, Err: errors.New("reply has no result object")}
	}
	out := make([]Snippet, 0, len(resp.Result.Points))
	for _, p := range resp.Result.Points {
		rel, ok := mapPath(c.cfg.RepoRoot, p.Payload.FilePath)
		if !ok || p.Payload.StartLine < 1 || p.Payload.EndLine < p.Payload.StartLine {
			continue
		}
		out = append(out, Snippet{
			Path: rel, StartLine: p.Payload.StartLine, EndLine: p.Payload.EndLine,
			Name: p.Payload.Name, Content: p.Payload.Content, Score: p.Score,
		})
	}
	return out, nil
}

// post sends one JSON exchange under the per-call timeout and the shared
// budget, and classifies any failure.
func (c *Client) post(caller, budget context.Context, stage, target string, withKey bool, body, into any) error {
	fail := func(cl Class, err error) error { return &Error{Class: cl, Stage: stage, Err: err} }
	if err := vetURL(target, c.cfg.AllowInsecureHTTP); err != nil {
		return fail(ClassInsecureScheme, err)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fail(ClassInvalidArgument, err)
	}
	call, cancel := context.WithTimeout(budget, c.cfg.CallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(call, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return fail(ClassInvalidArgument, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if withKey && c.apiKey != "" {
		req.Header.Set(apiKeyHdr, c.apiKey)
	}
	timing := func(err error) error {
		switch {
		case caller.Err() != nil:
			return fail(ClassCanceled, err)
		case budget.Err() != nil:
			return fail(ClassTimeout, fmt.Errorf("total budget %s exhausted: %w", c.cfg.TotalBudget, err))
		case call.Err() != nil:
			return fail(ClassTimeout, fmt.Errorf("per-call timeout %s: %w", c.cfg.CallTimeout, err))
		}
		return nil
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, errInsecure) {
			return fail(ClassInsecureScheme, err)
		}
		if t := timing(err); t != nil {
			return t
		}
		return fail(ClassUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRespLen))
	if err != nil {
		if t := timing(err); t != nil {
			return t
		}
		return fail(ClassUnreachable, err)
	}
	if resp.StatusCode != http.StatusOK {
		cl := ClassStatus
		if stage == "qdrant" && resp.StatusCode == http.StatusNotFound {
			cl = ClassNotFound
		}
		return fail(cl, fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fail(ClassDecode, err)
	}
	return nil
}

func toSlash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// mapPath maps an indexed file_path to a repo-relative slash path. The index
// stores the path the indexer walked, usually absolute on the machine that
// built it. In order: a path under root has root stripped; any other absolute
// path is matched on the suffix after the FIRST segment equal to root's base
// name (so /Users/x/atcr/cmd/atcr/main.go -> cmd/atcr/main.go); a relative
// path is taken as already repo-relative. Anything that is empty, the root
// itself, or escapes the repo after cleaning is dropped (ok=false).
func mapPath(root, filePath string) (string, bool) {
	if filePath == "" {
		return "", false
	}
	root = strings.TrimRight(path.Clean(toSlash(root)), "/")
	p := toSlash(filePath)
	var rel string
	switch {
	case strings.HasPrefix(p, root+"/"):
		rel = p[len(root)+1:]
	case path.IsAbs(p) || (len(p) > 2 && p[1] == ':' && p[2] == '/'): // posix or drive-letter
		seg := "/" + path.Base(root) + "/"
		i := strings.Index(p, seg)
		if i < 0 {
			return "", false
		}
		rel = p[i+len(seg):]
	default:
		rel = p
	}
	// Any ".." segment is refused outright rather than cleaned away: the
	// index has no business naming a path that way.
	if strings.Contains("/"+rel+"/", "/../") {
		return "", false
	}
	rel = path.Clean(rel)
	if rel == "." || rel == "" || strings.HasPrefix(rel, "/") {
		return "", false
	}
	return rel, true
}
