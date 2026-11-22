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
//
// registryYAML is the user registry quickstart wrote (RegistryYAML). The workflow
// writes it to the runner's ~/.config/atcr/registry.yaml before the review: a
// fresh runner has no user registry, and a committed .atcr/registry.yaml is only
// an overlay on one, so without this step `atcr review` exits 2 "registry not
// found". It holds only the key env var NAME, never a key, so it is safe to commit.
func WorkflowYAML(m *Manifest, registryYAML string) string {
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
	// Pinning note (deliberate, not an oversight): this scaffold uses @latest for
	// the atcr install and floating major tags for the setup actions. atcr has no
	// published release yet, so pinning `go install ...@vN` — or switching to the
	// composite `uses: samestrin/atcr@v1` — would fail to resolve and break
	// consumer CI worse than @latest. (The composite path is also foreclosed here
	// independent of the tag: action.yml requires an openrouter-api-key input with
	// no synthetic-key alternative, and its public contract is out of scope to
	// change.) This template is an intentionally minimal, non-merge-gating
	// review-only starter — unlike the composite action's full pipeline referenced
	// above — so a floating version is an acceptable default. Revisit once atcr
	// cuts a tagged release; that is its own release-engineering follow-up, not a
	// workflow-template change.
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("        with:\n")
	b.WriteString("          fetch-depth: 0   # atcr needs full history to resolve the merge-base\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.25'\n")
	b.WriteString("      - name: Install atcr\n")
	b.WriteString("        run: go install github.com/samestrin/atcr/cmd/atcr@latest\n")
	b.WriteString("      - name: Write the atcr user registry\n")
	b.WriteString("        # A fresh runner has no ~/.config/atcr/registry.yaml, and a committed\n")
	b.WriteString("        # .atcr/registry.yaml only overlays one. This is the registry quickstart\n")
	b.WriteString("        # wrote; it names the key env var, never the key.\n")
	b.WriteString("        run: |\n")
	b.WriteString("          mkdir -p \"$HOME/.config/atcr\"\n")
	b.WriteString("          cat > \"$HOME/.config/atcr/registry.yaml\" <<'ATCR_REGISTRY'\n")
	for _, line := range strings.Split(strings.TrimRight(registryYAML, "\n"), "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("          " + line + "\n")
	}
	b.WriteString("          ATCR_REGISTRY\n")
	b.WriteString("      - name: Review\n")
	b.WriteString("        env:\n")
	fmt.Fprintf(&b, "          %s: ${{ secrets.%s }}\n", env, env)
	// Pass the base ref through an env var rather than inlining ${{ }} into the
	// run: shell line — GitHub hardening guidance warns that expressions are
	// substituted before the shell runs, so a branch name with shell
	// metacharacters could break or inject. The script reads the shell var.
	b.WriteString("          BASE_REF: ${{ github.base_ref }}\n")
	b.WriteString("        run: atcr review --base \"origin/${BASE_REF}\"\n")
	return b.String()
}
