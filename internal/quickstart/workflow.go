package quickstart

import (
	"fmt"
	"strings"
)

// WorkflowYAML renders the .github/workflows/atcr.yml scaffold: a minimal
// review-only CI workflow that installs atcr and runs `atcr review` wired to the
// synthetic provider's API-key secret. It is intentionally review-only — it does
// NOT run `atcr reconcile`/`atcr github`, so it posts no PR check and is not a
// merge gate; the published composite action (samestrin/atcr@v1) is the path for
// the full pipeline. The secret name tracks the manifest's api_key_env so the
// scaffold stays consistent with the generated registry.
func WorkflowYAML(m *Manifest) string {
	env := m.Provider.APIKeyEnv
	var b strings.Builder
	b.WriteString("# .github/workflows/atcr.yml — scaffolded by `atcr quickstart`.\n")
	b.WriteString("# Minimal review-only workflow: installs atcr and runs `atcr review` against\n")
	b.WriteString("# the PR, wired to the synthetic provider via the secret below.\n")
	b.WriteString("#\n")
	b.WriteString("# NOTE: intentionally minimal. It does NOT run `atcr reconcile` or `atcr github`,\n")
	b.WriteString("# so it posts no PR check and is not a merge gate. For the full PR-check pipeline\n")
	b.WriteString("# use the published composite action (samestrin/atcr@v1); see docs/github-action.md.\n")
	b.WriteString("name: atcr review\n")
	b.WriteString("on:\n")
	b.WriteString("  pull_request:\n")
	b.WriteString("permissions:\n")
	b.WriteString("  contents: read\n")
	b.WriteString("jobs:\n")
	b.WriteString("  atcr:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("        with:\n")
	b.WriteString("          fetch-depth: 0   # atcr needs full history to resolve the merge-base\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.25'\n")
	b.WriteString("      - name: Install atcr\n")
	b.WriteString("        run: go install github.com/samestrin/atcr/cmd/atcr@latest\n")
	b.WriteString("      - name: Review\n")
	b.WriteString("        env:\n")
	fmt.Fprintf(&b, "          %s: ${{ secrets.%s }}\n", env, env)
	b.WriteString("        run: atcr review --base \"origin/${{ github.base_ref }}\"\n")
	return b.String()
}
