package semindex

import (
	"context"
	"errors"

	"github.com/samestrin/atcr/internal/log"
)

// embedBatchSize bounds the inputs of one embedding call: 64 x 2560 floats is
// ~3.5 MB of JSON, well inside maxRespLen.
const embedBatchSize = 64

// Embedder is the embedding half of a Client, for a caller that compares texts
// with each other and has no qdrant collection to search (`atcr debt validate
// --coherence`, epic 96). Build it with NewEmbedder.
type Embedder struct{ c *Client }

// NewEmbedder validates the embedding fields of cfg and returns an Embedder.
// QdrantEndpoint, Collection and RepoRoot are ignored, and no qdrant API key
// is read, since nothing is ever sent to qdrant. It does no I/O.
func NewEmbedder(cfg Config) (*Embedder, error) {
	if err := checkEndpoint("embedding endpoint", cfg.EmbeddingEndpoint); err != nil {
		return nil, err
	}
	cfg, err := withDefaults(cfg)
	if err != nil {
		return nil, err
	}
	return &Embedder{c: newClient(cfg)}, nil
}

// Embed returns one EmbeddingModel vector per text, in input order. Texts go
// out in batches of embedBatchSize, each call under CallTimeout and the whole
// pass under TotalBudget. It is all or nothing: any failure returns no vectors
// and an *Error, counted once on UnreachableMetric. No texts means no call.
//
// The failure is logged by class only. The error itself can carry the
// endpoint URL, so a caller that must not show it reports the Class.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	vecs, err := e.embedAll(ctx, texts)
	if err != nil {
		e.c.cfg.Metrics.Counter(UnreachableMetric).Inc()
		class := ClassUnreachable
		var se *Error
		if errors.As(err, &se) {
			class = se.Class
		}
		log.FromContext(ctx).Warn("embedding failed; continuing without coherence scores", "class", string(class))
		return nil, err
	}
	return vecs, nil
}

func (e *Embedder) embedAll(ctx context.Context, texts []string) ([][]float64, error) {
	budget, cancel := context.WithTimeout(ctx, e.c.cfg.TotalBudget)
	defer cancel()
	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := min(start+embedBatchSize, len(texts))
		vecs, err := e.c.embed(ctx, budget, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}
