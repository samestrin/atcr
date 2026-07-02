package quickstart

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkflowYAML_ReviewOnlySynthetic(t *testing.T) {
	m := &Manifest{Provider: Provider{Name: "synthetic", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"}}
	out := WorkflowYAML(m)

	// Wired to the synthetic key secret and runs a review.
	assert.Contains(t, out, "LLM_SYNTHETIC_API_KEY: ${{ secrets.LLM_SYNTHETIC_API_KEY }}")
	assert.Contains(t, out, "atcr review")
	// atcr needs full history to resolve the merge-base.
	assert.Contains(t, out, "fetch-depth: 0")
	// Intentionally review-only: no reconcile/github PR-gate RUN steps (the
	// explanatory header may still name them).
	assert.NotContains(t, out, "run: atcr github")
	assert.NotContains(t, out, "run: atcr reconcile")
	assert.NotContains(t, out, "atcr reconcile &&")
}
