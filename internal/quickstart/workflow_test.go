package quickstart

import (
	"strings"
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

func TestWorkflowYAML_NoExpressionInRunLine(t *testing.T) {
	m := &Manifest{Provider: Provider{Name: "synthetic", APIKeyEnv: "LLM_SYNTHETIC_API_KEY"}}
	out := WorkflowYAML(m)

	// GitHub hardening: a ${{ }} expression must never be inlined into a run:
	// shell line (it is substituted before the shell runs). The base ref is
	// passed through an env var and referenced as the shell var $BASE_REF instead.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "run:") {
			assert.NotContainsf(t, line, "${{", "no GitHub expression inlined into a run: line: %q", line)
		}
	}
	assert.Contains(t, out, "BASE_REF: ${{ github.base_ref }}", "base ref passed via env")
	assert.Contains(t, out, "origin/${BASE_REF}", "run script references the shell env var")
}
