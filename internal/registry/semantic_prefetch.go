package registry

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	// DefaultSemanticTopK is the number of similarity hits requested per query
	// when context_prefetch.semantic.top_k is unset (Epic 35.16.12).
	DefaultSemanticTopK = 5
	// MaxSemanticTopK bounds top_k. Similarity snippets are the LOWEST budget tier
	// of the Context Definitions section, so a large top_k buys network time and
	// bytes the cap will shed anyway.
	MaxSemanticTopK = 100
)

// semanticCollectionRE is the accepted collection-name shape. The name is
// interpolated into a URL path (/collections/{collection}/points/query), so a
// separator, a query character or a leading dot ("..") must never reach it.
var semanticCollectionRE = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]{0,254}$`)

// ContextPrefetchConfig is the optional `context_prefetch` block, carried at
// the registry (user) and project tiers. Its only member today is Semantic.
type ContextPrefetchConfig struct {
	Semantic *SemanticPrefetchConfig `yaml:"semantic,omitempty"`
}

// SemanticPrefetchConfig is one tier's `context_prefetch.semantic` block (Epic
// 35.16.12): opt-in similarity retrieval against an external index. Every field
// is optional at a single tier, because the other tier may supply the rest; the
// project tier wins field by field. Enabled and AllowInsecureHTTP are pointers so
// an explicit false at the project tier overrides a user-tier true.
//
// Retrieval makes two calls, so it needs two base URLs: Endpoint is the qdrant
// REST API the vectors are queried against, and EmbeddingEndpoint is the
// OpenAI-compatible service that turns the query text into those vectors.
//
// There is deliberately no credential field: the index's API key comes from the
// environment, never from a config file a repository can carry.
type SemanticPrefetchConfig struct {
	Enabled           *bool  `yaml:"enabled,omitempty"`
	Endpoint          string `yaml:"endpoint,omitempty"`
	EmbeddingEndpoint string `yaml:"embedding_endpoint,omitempty"`
	Collection        string `yaml:"collection,omitempty"`
	TopK              *int   `yaml:"top_k,omitempty"`
	AllowInsecureHTTP *bool  `yaml:"allow_insecure_http,omitempty"`
}

// SemanticPrefetch is the resolved `context_prefetch.semantic` setting. Its zero
// value is DISABLED — the safe direction for a feature that makes network calls —
// so a hand-built Settings{} never switches it on. Read it through
// Settings.ResolvedSemanticPrefetch.
type SemanticPrefetch struct {
	Enabled           bool
	Endpoint          string
	EmbeddingEndpoint string
	Collection        string
	TopK              int
	AllowInsecureHTTP bool
}

// Validate checks the values a single tier set. Completeness (enabled needs an
// endpoint, an embedding_endpoint and a collection) is a property of the
// RESOLVED block and is checked by ResolveSettings, since either tier may supply
// any of them. A nil receiver
// or a nil Semantic block is valid: absent means disabled.
func (c *ContextPrefetchConfig) Validate() error {
	if c == nil || c.Semantic == nil {
		return nil
	}
	sc := c.Semantic
	topK := 0
	if sc.TopK != nil {
		topK = *sc.TopK
	}
	return validateSemanticFields(sc.Endpoint, sc.EmbeddingEndpoint, sc.Collection, topK, sc.TopK != nil)
}

// validateSemanticFields checks the non-blank fields. topKSet distinguishes an
// explicit top_k from an unset one (0 is invalid only when it was written).
func validateSemanticFields(endpoint, embeddingEndpoint, collection string, topK int, topKSet bool) error {
	var errs []error
	if topKSet && (topK < 1 || topK > MaxSemanticTopK) {
		errs = append(errs, fmt.Errorf("context_prefetch.semantic.top_k must be within 1..%d, got %d", MaxSemanticTopK, topK))
	}
	if e := strings.TrimSpace(endpoint); e != "" {
		if err := validateSemanticEndpoint("endpoint", e); err != nil {
			errs = append(errs, err)
		}
	}
	if e := strings.TrimSpace(embeddingEndpoint); e != "" {
		if err := validateSemanticEndpoint("embedding_endpoint", e); err != nil {
			errs = append(errs, err)
		}
	}
	if c := strings.TrimSpace(collection); c != "" && !semanticCollectionRE.MatchString(c) {
		errs = append(errs, fmt.Errorf("invalid context_prefetch.semantic.collection %q: must be 1-255 of A-Z a-z 0-9 . _ - and not start with '.'", c))
	}
	return errors.Join(errs...)
}

// validateSemanticEndpoint accepts an absolute http(s) base URL with a host. The
// http-to-non-loopback refusal is the retrieval client's (internal/semindex), at
// call time and on every redirect hop; this is the load-time shape check. A URL
// carrying userinfo is refused because it would put a credential in a config file.
// key is the YAML key being checked, so the error names the URL that is wrong.
func validateSemanticEndpoint(key, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid context_prefetch.semantic.%s: %w", key, err)
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("invalid context_prefetch.semantic.%s %q: scheme must be http or https", key, raw)
	case u.User != nil:
		return fmt.Errorf("invalid context_prefetch.semantic.%s: it must not carry a credential (user:password@); the index API key comes from the environment", key)
	case u.Opaque != "" || u.Hostname() == "":
		return fmt.Errorf("invalid context_prefetch.semantic.%s %q: must be an absolute URL with a host", key, raw)
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return fmt.Errorf("invalid context_prefetch.semantic.%s %q: must not carry a query or fragment", key, raw)
	}
	return nil
}

// overlaySemanticPrefetch applies one tier's explicitly-set fields onto dst.
// Blank strings count as unset and fall through, like every string setting here.
func overlaySemanticPrefetch(dst *SemanticPrefetch, cfg *ContextPrefetchConfig, topKSet *bool) {
	if cfg == nil || cfg.Semantic == nil {
		return
	}
	sc := cfg.Semantic
	if sc.Enabled != nil {
		dst.Enabled = *sc.Enabled
	}
	if v := strings.TrimSpace(sc.Endpoint); v != "" {
		dst.Endpoint = v
	}
	if v := strings.TrimSpace(sc.EmbeddingEndpoint); v != "" {
		dst.EmbeddingEndpoint = v
	}
	if v := strings.TrimSpace(sc.Collection); v != "" {
		dst.Collection = v
	}
	if sc.TopK != nil {
		dst.TopK = *sc.TopK
		*topKSet = true
	}
	if sc.AllowInsecureHTTP != nil {
		dst.AllowInsecureHTTP = *sc.AllowInsecureHTTP
	}
}

// validateResolvedSemanticPrefetch re-checks the merged block: a hand-built
// ProjectConfig or Registry bypasses the loaders, and only the merged value can
// say whether an enabled block knows where to send its queries.
func validateResolvedSemanticPrefetch(sp SemanticPrefetch, topKSet bool) error {
	if err := validateSemanticFields(sp.Endpoint, sp.EmbeddingEndpoint, sp.Collection, sp.TopK, topKSet); err != nil {
		return err
	}
	if !sp.Enabled {
		return nil
	}
	var errs []error
	for _, req := range []struct{ key, value string }{
		{"endpoint", sp.Endpoint},
		{"embedding_endpoint", sp.EmbeddingEndpoint},
		{"collection", sp.Collection},
	} {
		if req.value == "" {
			errs = append(errs, fmt.Errorf("context_prefetch.semantic.enabled is true but no tier sets context_prefetch.semantic.%s", req.key))
		}
	}
	return errors.Join(errs...)
}

// ResolvedSemanticPrefetch returns the effective similarity-retrieval setting.
// Anything short of an explicit `enabled: true` returns the zero value, so a
// disabled block hands no endpoint to a caller that might be tempted to use it.
// An unset top_k reads as DefaultSemanticTopK.
func (s Settings) ResolvedSemanticPrefetch() SemanticPrefetch {
	sp := s.SemanticPrefetch
	if !sp.Enabled {
		return SemanticPrefetch{}
	}
	if sp.TopK <= 0 {
		sp.TopK = DefaultSemanticTopK
	}
	if sp.TopK > MaxSemanticTopK {
		sp.TopK = MaxSemanticTopK // a hand-built Settings{} never reaches the resolver's check
	}
	return sp
}
