package reconcile

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docs/benchmark.md's export section tells an operator that anchoring a `repo-state-v1`
// suite inherits the per-case `change.diff` size cap, names the cap's value, and names
// the escape. Nothing pinned any of it.
//
// The paragraph exists because the failure it describes is diagnosable ONLY from the
// doc: `benchmark export --suite-path` consults just the identity pair and the case
// ids, so an operator hitting a cap error there has no reason to suspect a diff they
// never asked the command to read. A paragraph that carries the whole explanation is
// exactly the kind that must not drift.
//
// The guard is BIDIRECTIONAL, like its siblings benchmark_repostate_doc_test.go and
// benchmark_publishable_doc_test.go: asserting only that the doc says a thing catches a
// doc edit but not a code edit, and the code direction is the one that misleads — the
// doc keeps promising a cap value, a loader path, or a narrow anchor read that the code
// no longer has. Each claim is therefore asserted twice, once against the doc and once
// against the code that makes it true.
//
// It lives in internal/reconcile/ per the repo's convention for doc-vs-code drift tests
// (no Go package lives under docs/, and the published reconcile/ module must not assume
// this repo's file layout). See justification_record_boundary_test.go for the precedent.
//
// Sources are read as TEXT rather than imported, matching the sibling guards: the claim
// under test is about which code arm exists and where, which a symbol reference cannot
// express, and reading text keeps this file free of a dependency on internal/benchmark.
func TestBenchmarkDoc_ExportAnchoringInheritsTheDiffCap(t *testing.T) {
	doc := readRepoFile(t, "../../docs/benchmark.md")
	bench := readRepoFile(t, "../../internal/benchmark/benchmark.go")
	repostate := readRepoFile(t, "../../internal/benchmark/repostate.go")
	coverage := readRepoFile(t, "../../cli/benchmark_coverage.go")

	// The paragraph itself. Its lead-in is the searchable handle an operator lands on.
	assert.Contains(t, doc, "Anchoring loads the whole manifest, size cap included",
		"docs/benchmark.md must keep the paragraph explaining that `benchmark export --suite-path` "+
			"inherits the per-case change.diff cap; without it a cap error from a command that reads "+
			"only case ids is undiagnosable")

	// Claim 1: the cap's VALUE. The doc states a number; MaxDiffBytes is that number.
	// Retuning the cap without re-reading this paragraph is the drift this pins.
	capDecl := regexp.MustCompile(`MaxDiffBytes = int64\((\d+) \* 1024 \* 1024\)`).FindStringSubmatch(bench)
	require.Len(t, capDecl, 2,
		"internal/benchmark/benchmark.go must declare MaxDiffBytes as int64(N * 1024 * 1024); "+
			"if the form changed, update this drift test to follow it")
	capMiB, err := strconv.Atoi(capDecl[1])
	require.NoError(t, err)
	assert.Contains(t, doc, strconv.Itoa(capMiB)+" MiB per-file cap",
		"docs/benchmark.md names the anchoring cap in MiB; MaxDiffBytes is now %d MiB, so the doc's "+
			"figure has gone stale", capMiB)

	// Claim 2: the cap is applied on the LoadRepoState path, which is what makes the
	// doc's "same loader `benchmark run` and `benchmark verify` use" true for anchoring.
	// A cap moved back out to the CLI runner would leave the doc describing a guarantee
	// the export path no longer inherits.
	assert.Contains(t, repostate, "fi.Size() > MaxDiffBytes",
		"internal/benchmark/repostate.go must apply MaxDiffBytes where the case's diff bytes become "+
			"resident; moving the cap to a caller silently un-does what the doc promises anchoring inherits")

	// Claim 3: anchoring really does route through that loader.
	loadAnchor := funcBody(t, coverage, "func loadSuiteAnchor(")
	assert.Contains(t, loadAnchor, "benchmark.LoadRepoState(suitePath)",
		"cli/benchmark_coverage.go's loadSuiteAnchor must load a repo-state suite through "+
			"benchmark.LoadRepoState; the doc's cap explanation only holds while it does")

	// Claim 4: the anchor read is as narrow as the doc says. This is the half that makes
	// the cap surprising and therefore worth documenting — if anchoring ever started
	// consulting the diffs themselves, the paragraph's "even though" clause goes false.
	anchorStruct := funcBody(t, coverage, "type suiteAnchor struct {")
	for _, field := range []string{"Suite", "SuiteVersion", "CaseIDs"} {
		assert.Contains(t, anchorStruct, field,
			"suiteAnchor must keep field %s — the doc says anchoring consults the identity pair and the case ids", field)
	}
	assert.NotContains(t, anchorStruct, "Diff",
		"suiteAnchor must not carry diff data: docs/benchmark.md tells the operator anchoring consults "+
			"only the identity pair and the case ids, which is why inheriting the diff cap needs explaining")

	// Claim 5: the escape and its cost. Dropping the flag is the documented way past a
	// capped suite, and the doc must keep saying it weakens the gate — an escape
	// described without its cost reads as a free workaround.
	assert.Contains(t, doc, "Dropping the flag to\nget past it is the documented escape",
		"docs/benchmark.md must keep naming the drop-the-flag escape for a capped suite")
	assert.Contains(t, doc, "downgrades the gate to the weaker",
		"docs/benchmark.md must keep stating that dropping --suite-path weakens the coverage gate")
}

// funcBody returns src from the line declaring decl through the closing brace at column
// zero, i.e. one top-level declaration. It lets a text-based drift guard scope an
// assertion to one function or type instead of the whole file, where a match elsewhere
// would pass the check for the wrong reason.
func funcBody(t *testing.T, src, decl string) string {
	t.Helper()
	start := strings.Index(src, decl)
	require.GreaterOrEqual(t, start, 0,
		"expected declaration %q; if it was renamed or moved, update this drift test to follow it", decl)
	rest := src[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}
