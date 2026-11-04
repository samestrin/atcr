package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samestrin/atcr/internal/log"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/semindex"
)

// semanticDegradeSecretGuard is the failure class a blocked similarity query
// reports (Epic 35.16.12.1). It rides a *semindex.Error so the payload's
// fail-open path, and the degrade reason it records, treat a query the guard
// refused to send like any other retriever failure.
const semanticDegradeSecretGuard semindex.Class = "secret-guard-blocked"

// semanticDegradeConfig is the failure class of an enabled similarity block
// whose client semindex.New refused to build (credentials in a URL, an insecure
// scheme, a missing collection). It is a fanout class, not a semindex one, so
// internal/semindex stays untouched.
const semanticDegradeConfig semindex.Class = "config-invalid"

// secretQueryLabel names the outbound similarity queries in a secret_guard
// warning. They belong to no payload file.
const secretQueryLabel = "similarity query"

// semanticRetrieverOption builds the optional similarity retriever from the
// resolved context_prefetch.semantic block (Epic 35.16.12.1). It lives here
// because payload must not import registry.
//
// Anything short of an explicit `enabled: true` returns nil BEFORE anything
// else happens: no client is constructed, no host is resolved and nothing is
// logged, so a config-free run is the run built before the tier existed. A
// client the config cannot build fails open with a warning and a retriever that
// fails every query with class semanticDegradeConfig, so the manifest records
// the reason when a query is attempted (a diff with no queryable symbol never
// calls the retriever, so it records none). Only the class is kept: the
// construction error can echo a configured URL, which must not reach the
// manifest or the logs.
func semanticRetrieverOption(ctx context.Context, cfg *ReviewConfig, repo string) payload.RangeOption {
	sp := cfg.Settings.ResolvedSemanticPrefetch()
	if !sp.Enabled {
		return nil
	}
	root, err := absRepoPath(repo)
	if err == nil {
		var c *semindex.Client
		c, err = semindex.New(semindex.Config{
			EmbeddingEndpoint: sp.EmbeddingEndpoint,
			QdrantEndpoint:    sp.Endpoint,
			Collection:        sp.Collection,
			RepoRoot:          root,
			AllowInsecureHTTP: sp.AllowInsecureHTTP,
			Metrics:           metrics.DefaultRegistry,
		})
		if err == nil {
			return payload.WithSemanticRetriever(&guardedRetriever{policy: secretGuardPolicy(cfg), inner: c}, sp.TopK)
		}
	}
	log.FromContext(ctx).Warn("semantic retrieval client could not be built; continuing without similarity snippets", "class", string(semanticDegradeConfig))
	return payload.WithSemanticRetriever(failingRetriever{err: errSemanticClientNotBuilt}, sp.TopK)
}

// absRepoPath is filepath.Abs, a variable so a test can force its failure.
var absRepoPath = filepath.Abs

// errSemanticClientNotBuilt is the class-only error failingRetriever stores.
var errSemanticClientNotBuilt = errors.New("the similarity client could not be built from the configuration")

// failingRetriever stands in for a client that could not be built. Every
// Retrieve fails with a *semindex.Error of class semanticDegradeConfig.
type failingRetriever struct{ err error }

func (f failingRetriever) Retrieve(context.Context, []string, int) ([]semindex.Snippet, error) {
	return nil, &semindex.Error{Class: semanticDegradeConfig, Stage: "config", Err: f.err}
}

// guardedRetriever scans every similarity query under the secret_guard policy
// before it leaves for the embedding endpoint. The queries are declaration text
// from the diff, so they can carry the same credential the rendered payload's
// guard (guardSecrets) would catch, but they never pass through it.
//
// off and warn send the queries unchanged, redact sends payload.RedactSecrets
// output, and block skips retrieval altogether, returning a *semindex.Error of
// class semanticDegradeSecretGuard so the review fails open to zero similarity
// snippets instead of aborting. A scan that cannot be trusted blocks too, as it
// does for the payload.
type guardedRetriever struct {
	policy string
	inner  semindex.Retriever
}

func (g *guardedRetriever) Retrieve(ctx context.Context, queries []string, topK int) ([]semindex.Snippet, error) {
	texts := make([]OutboundText, len(queries))
	for i, q := range queries {
		texts[i] = OutboundText{Label: secretQueryLabel, Text: q}
	}
	queries, err := GuardOutbound(ctx, g.policy, "the similarity index", texts)
	if err != nil {
		return nil, &semindex.Error{Class: semanticDegradeSecretGuard, Stage: "query", Err: errors.New("secret_guard=block: similarity queries not sent")}
	}
	return g.inner.Retrieve(ctx, queries, topK)
}

// ErrOutboundBlocked is GuardOutbound's refusal under secret_guard=block.
var ErrOutboundBlocked = errors.New("secret_guard=block: text not sent")

// OutboundText is one text bound for an outside endpoint. Label names it in a
// secret_guard warning in place of the text itself.
type OutboundText struct {
	Label string
	Text  string
}

// GuardOutbound scans each text under the secret_guard policy before it leaves
// for dest, an outside endpoint named for the warnings ("the similarity
// index"). It returns the texts to send, in order: off and warn send them
// unchanged, redact sends payload.RedactSecrets output, and block returns
// ErrOutboundBlocked and nothing, as does block on a scan that cannot be
// trusted. "" is the registry default, warn. Warnings name labels and secret
// kinds, never the text.
func GuardOutbound(ctx context.Context, policy, dest string, texts []OutboundText) ([]string, error) {
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = t.Text
	}
	if policy == secretGuardOff {
		return out, nil
	}
	var (
		found   []secretFinding
		scanErr error
		hits    = make([][]payload.SecretHit, len(texts))
	)
	for i, t := range texts {
		h, err := payload.ScanPayloadContext(ctx, t.Text, nil)
		if err != nil {
			scanErr = err
		}
		hits[i] = h
		for _, hit := range h {
			found = append(found, secretFinding{kind: hit.Kind, file: t.Label})
		}
	}
	if scanErr != nil {
		fmt.Fprintf(os.Stderr, "atcr: warning: text for %s: secret scan: %v; using the built-in ruleset\n", dest, scanErr)
	}
	switch policy {
	case secretGuardBlock:
		if len(found) == 0 && scanErr == nil {
			break
		}
		why := "the secret scan failed, so the text could not be checked"
		if len(found) > 0 {
			why = describeFindings(found)
		}
		fmt.Fprintf(os.Stderr, "atcr: warning: nothing sent to %s: %s (secret_guard: block)\n", dest, why)
		return nil, ErrOutboundBlocked
	case secretGuardRedact:
		if len(found) > 0 {
			for i, t := range texts {
				out[i] = payload.RedactSecrets(t.Text, hits[i])
			}
			fmt.Fprintf(os.Stderr, "atcr: warning: redacted %s before sending to %s (secret_guard: redact)\n", describeFindings(found), dest)
		}
	default:
		if len(found) > 0 {
			fmt.Fprintf(os.Stderr, "atcr: warning: %s sent unchanged to %s (secret_guard: warn); set secret_guard: redact or block to stop it\n", describeFindings(found), dest)
		}
	}
	return out, nil
}
