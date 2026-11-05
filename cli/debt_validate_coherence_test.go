package cli

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/semindex"
)

// coherenceAWSKey matches the built-in ruleset's aws-access-token rule.
const coherenceAWSKey = "AKIA" + "QYLPMN5HHHFPZAM2"

// fakeEmbedder is an OpenAI-compatible embedding endpoint whose vectors are
// bags of words, so a Fix that shares its Problem's words scores high and a
// Fix copied from another finding scores near zero. failCall, when non-zero,
// answers that call (1-based) with a 502.
type fakeEmbedder struct {
	srv      *httptest.Server
	mu       sync.Mutex
	calls    int
	inputs   []string
	failCall int
}

func newFakeEmbedder(t *testing.T) *fakeEmbedder {
	t.Helper()
	f := &fakeEmbedder{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		f.mu.Lock()
		f.calls++
		fail := f.calls == f.failCall
		f.inputs = append(f.inputs, req.Input...)
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		items := make([]item, len(req.Input))
		for i, in := range req.Input {
			items[i] = item{Index: i, Embedding: bagOfWords(in)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func bagOfWords(s string) []float64 {
	v := make([]float64, semindex.VectorSize)
	// Punctuation is not a word, so an all-punctuation text embeds to zeros.
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for _, w := range words {
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[h.Sum32()%uint32(semindex.VectorSize)]++
	}
	return v
}

// coherenceEnv points HOME at a registry whose context_prefetch.semantic block
// names embedURL ("" leaves the block out) under the given secret_guard, and
// PATH at an empty directory so the built-in secret rules decide.
func coherenceEnv(t *testing.T, embedURL, guard string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATCR_REGISTRY_URL", "")
	t.Setenv("PATH", t.TempDir())
	reg := "providers:\n  mock:\n    api_key_env: ATCR_COHERENCE_TEST_KEY\n    base_url: http://127.0.0.1:1/v1\n" +
		"agents:\n  bruce:\n    provider: mock\n    model: m\n" +
		"secret_guard: " + guard + "\n"
	if embedURL != "" {
		reg += "context_prefetch:\n  semantic:\n    enabled: true\n    endpoint: http://127.0.0.1:1\n" +
			"    embedding_endpoint: " + embedURL + "\n    collection: c\n"
	}
	dir := filepath.Join(home, ".config", "atcr")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(reg), 0o644))
}

func coherenceRec(file, problem, fix string) localdebt.Record {
	r := localdebt.Record{
		SchemaVersion: localdebt.SchemaVersion, RunID: "2026-10-01T10:00:00Z-x", Timestamp: "2026-10-01T10:00:00Z",
		Severity: "HIGH", File: file, Line: 1, Problem: problem, Fix: fix, Category: "c", Confidence: "high",
	}
	r.StampID()
	return r
}

// coherenceStore has five rows; the last one's Fix is copied from the second.
func coherenceStore(t *testing.T) (dir, root string) {
	t.Helper()
	root = t.TempDir()
	return writeLocalDebt(t,
		coherenceRec("a.go", "nil pointer dereference in parser when input is empty", "guard the parser against a nil pointer when input is empty"),
		coherenceRec("b.go", "sql query built by string concatenation allows injection", "use a parameterized sql query instead of string concatenation"),
		coherenceRec("c.go", "goroutine leak when the context is canceled in worker", "stop the worker goroutine when the context is canceled"),
		coherenceRec("d.go", "cache never evicts entries so memory grows", "evict cache entries so memory stays bounded"),
		coherenceRec("e.go", "log line prints the api token in plain text", "use a parameterized sql query instead of string concatenation"),
	), root
}

func runCoherence(t *testing.T, dir, root string, extra ...string) (int, debtValidateDoc, string, string) {
	t.Helper()
	args := append([]string{"debt", "validate", "--dir", dir, "--root", root, "--coherence"}, extra...)
	code, stdout, stderr := execCmdSplit(t, args...)
	var doc debtValidateDoc
	if code == 0 && !strings.Contains(strings.Join(extra, " "), "--axi") {
		require.NoError(t, json.Unmarshal([]byte(stdout), &doc), stdout)
	}
	return code, doc, stdout, stderr
}

func TestDebtValidateCoherence_FlagsAFixCopiedFromAnotherFinding(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	dir, root := coherenceStore(t)

	code, doc, stdout, _ := runCoherence(t, dir, root)
	require.Equal(t, 0, code, "flagged rows are advisory")
	c := doc.Coherence
	assert.Equal(t, coherenceChecked, c.Status)
	assert.Equal(t, 20, c.Percentile)
	assert.Equal(t, 5, c.Scored)
	require.Len(t, c.Flagged, 1, "20% of five rows is one")
	assert.Equal(t, "e.go", c.Flagged[0].File)
	assert.Less(t, c.Flagged[0].Score, 0.3)
	assert.Len(t, emb.inputs, 10, "one Problem and one Fix per row")
	assert.NotContains(t, stdout, "parameterized", "the output carries no row text")
	assert.Len(t, doc.Items, 5, "grounding still runs")

	_, doc, _, _ = runCoherence(t, dir, root, "--coherence-percentile", "40")
	assert.Len(t, doc.Coherence.Flagged, 2)
	assert.Equal(t, "e.go", doc.Coherence.Flagged[0].File, "least coherent first")
}

func TestDebtValidateCoherence_AXI(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	dir, root := coherenceStore(t)
	code, _, stdout, _ := runCoherence(t, dir, root, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "status: checked")
	assert.Contains(t, stdout, "percentile: 20")
	assert.Contains(t, stdout, "flagged[1]{id,file,line,score}:")
}

func TestDebtValidateCoherence_NotRequestedByDefault(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	dir, root := coherenceStore(t)
	out, err := runDebt(t, "validate", "--dir", dir, "--root", root)
	require.NoError(t, err)
	assert.Contains(t, out, `"reason": "not-requested"`)
	assert.Zero(t, emb.calls, "no flag, no egress")
}

func TestDebtValidateCoherence_NoEndpointIsSkipped(t *testing.T) {
	dir, root := coherenceStore(t)
	t.Run("no semantic block", func(t *testing.T) {
		coherenceEnv(t, "", "warn")
		code, doc, _, _ := runCoherence(t, dir, root)
		require.Equal(t, 0, code)
		assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "not-configured"}, doc.Coherence)
	})
	t.Run("no registry at all", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("ATCR_REGISTRY_URL", "")
		code, doc, stdout, _ := runCoherence(t, dir, root)
		require.Equal(t, 0, code)
		assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "not-configured"}, doc.Coherence)
		assert.NotContains(t, stdout, "percentile", "a skipped result is {status, reason} only")
	})
	t.Run("unreadable config", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "atcr"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "atcr", "registry.yaml"), []byte("bogus: ["), 0o644))
		code, doc, _, _ := runCoherence(t, dir, root)
		require.Equal(t, 0, code)
		assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "config-unavailable"}, doc.Coherence)
	})
}

func TestDebtValidateCoherence_SecretGuard(t *testing.T) {
	root := t.TempDir()
	dir := writeLocalDebt(t,
		coherenceRec("a.go", "key "+coherenceAWSKey+" is committed", "rotate the key"),
		coherenceRec("b.go", "slow loop", "cache the loop"),
	)
	t.Run("block sends nothing", func(t *testing.T) {
		emb := newFakeEmbedder(t)
		coherenceEnv(t, emb.srv.URL, "block")
		code, doc, stdout, stderr := runCoherence(t, dir, root)
		require.Equal(t, 0, code)
		assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "secret-guard-blocked"}, doc.Coherence)
		assert.Zero(t, emb.calls, "a blocked row is never sent")
		assert.NotContains(t, stdout+stderr, coherenceAWSKey)
	})
	t.Run("redact sends redacted text", func(t *testing.T) {
		emb := newFakeEmbedder(t)
		coherenceEnv(t, emb.srv.URL, "redact")
		code, doc, _, stderr := runCoherence(t, dir, root)
		require.Equal(t, 0, code)
		assert.Equal(t, coherenceChecked, doc.Coherence.Status)
		require.Len(t, emb.inputs, 4)
		assert.NotContains(t, strings.Join(emb.inputs, "\n"), coherenceAWSKey)
		assert.NotContains(t, stderr, coherenceAWSKey)
	})
}

func TestDebtValidateCoherence_EndpointFailingMidRunIsSkipped(t *testing.T) {
	emb := newFakeEmbedder(t)
	emb.failCall = 2 // the first batch succeeds, the second fails
	coherenceEnv(t, emb.srv.URL, "warn")
	root := t.TempDir()
	recs := make([]localdebt.Record, 0, 40)
	for i := range 40 {
		recs = append(recs, coherenceRec(fmt.Sprintf("f%d.go", i), fmt.Sprintf("problem %d", i), fmt.Sprintf("fix %d", i)))
	}
	dir := writeLocalDebt(t, recs...)
	before := metrics.DefaultRegistry.Counter(semindex.UnreachableMetric).Value()

	code, doc, stdout, stderr := runCoherence(t, dir, root)
	require.Equal(t, 0, code, "an endpoint failure never fails the command")
	assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "status"}, doc.Coherence, "never a partial pass")
	assert.Equal(t, 2, emb.calls, "PRECONDITION: the failure came mid-run")
	assert.Equal(t, before+1, metrics.DefaultRegistry.Counter(semindex.UnreachableMetric).Value())
	host := strings.TrimPrefix(emb.srv.URL, "http://")
	assert.NotContains(t, stdout+stderr, host, "no URL in the output")
	assert.NotContains(t, stdout+stderr, "problem 1", "no row text in the output")
}

func TestDebtValidateCoherence_InsecureEndpointIsSkipped(t *testing.T) {
	coherenceEnv(t, "http://embed.example.invalid", "warn")
	dir, root := coherenceStore(t)
	code, doc, stdout, stderr := runCoherence(t, dir, root)
	require.Equal(t, 0, code)
	assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "insecure-scheme"}, doc.Coherence)
	assert.NotContains(t, stdout+stderr, "embed.example.invalid")
}

func TestDebtValidateCoherence_RowsWithoutAFixAreNotScored(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	root := t.TempDir()
	dir := writeLocalDebt(t, coherenceRec("a.go", "p", " "), coherenceRec("b.go", "slow loop", "cache the loop"))
	code, doc, _, _ := runCoherence(t, dir, root)
	require.Equal(t, 0, code)
	assert.Equal(t, 1, doc.Coherence.Scored)
	assert.Len(t, emb.inputs, 2)
}

func TestDebtValidateCoherence_PercentileRange(t *testing.T) {
	dir, root := coherenceStore(t)
	for _, bad := range []string{"0", "101", "-5"} {
		code, _, _ := execCmdSplit(t, "debt", "validate", "--dir", dir, "--root", root, "--coherence", "--coherence-percentile", bad)
		assert.Equal(t, 2, code, bad)
	}
}

func TestDebtValidateCoherence_DisabledBlockSendsNothing(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	reg := filepath.Join(os.Getenv("HOME"), ".config", "atcr", "registry.yaml")
	data, err := os.ReadFile(reg)
	require.NoError(t, err)
	off := strings.Replace(string(data), "enabled: true", "enabled: false", 1)
	require.NotEqual(t, string(data), off, "PRECONDITION: the block was enabled")
	require.NoError(t, os.WriteFile(reg, []byte(off), 0o644))
	dir, root := coherenceStore(t)

	code, doc, _, _ := runCoherence(t, dir, root)
	require.Equal(t, 0, code)
	assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "not-configured"}, doc.Coherence)
	assert.Zero(t, emb.calls, "a disabled block never sends row text")
}

func TestDebtValidateCoherence_NoRowsToScore(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	root := t.TempDir()
	dir := writeLocalDebt(t, coherenceRec("a.go", "p", " "), coherenceRec("b.go", " ", "f"))

	code, doc, stdout, _ := runCoherence(t, dir, root)
	require.Equal(t, 0, code)
	assert.Equal(t, coherenceChecked, doc.Coherence.Status)
	assert.Equal(t, "no-rows-to-score", doc.Coherence.Reason)
	assert.Zero(t, doc.Coherence.Scored)
	assert.Contains(t, stdout, `"flagged": []`, "a checked result lists flagged, never null")
	assert.Zero(t, emb.calls)

	code, _, stdout, _ = runCoherence(t, dir, root, "--axi")
	require.Equal(t, 0, code)
	assert.Contains(t, stdout, "flagged[0]")
}

func TestDebtValidateCoherence_EmbedderConfigInvalid(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	old := newCoherenceEmbedder
	t.Cleanup(func() { newCoherenceEmbedder = old })
	newCoherenceEmbedder = func(semindex.Config) (*semindex.Embedder, error) {
		return nil, fmt.Errorf("semindex: embedding endpoint %s is bad", emb.srv.URL)
	}
	dir, root := coherenceStore(t)
	code, doc, stdout, stderr := runCoherence(t, dir, root)
	require.Equal(t, 0, code)
	assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "config-invalid"}, doc.Coherence)
	assert.NotContains(t, stdout+stderr, strings.TrimPrefix(emb.srv.URL, "http://"))
	assert.Zero(t, emb.calls)
}

func TestDebtValidateCoherence_TiesOrderByIDAndZeroVectorsScoreZero(t *testing.T) {
	emb := newFakeEmbedder(t)
	coherenceEnv(t, emb.srv.URL, "warn")
	root := t.TempDir()
	// Rows reach scoring sorted by file, so pick file names where the first
	// file has the HIGHER id: only the comparator can then put the lower first.
	var a, b localdebt.Record
	for i := 0; ; i++ {
		require.Less(t, i, 1000, "no file pair with descending ids")
		a = coherenceRec(fmt.Sprintf("a%d.go", i), "slow loop", "cache the loop")
		b = coherenceRec(fmt.Sprintf("b%d.go", i), "slow loop", "cache the loop")
		if a.ID > b.ID {
			break
		}
	}
	z := coherenceRec("z.go", "!!!", "???")
	dir := writeLocalDebt(t, a, b, z)

	code, doc, _, _ := runCoherence(t, dir, root, "--coherence-percentile", "100")
	require.Equal(t, 0, code)
	f := doc.Coherence.Flagged
	require.Len(t, f, 3)
	assert.Equal(t, "z.go", f[0].File)
	assert.Zero(t, f[0].Score, "a zero vector scores 0, not NaN")
	assert.Equal(t, f[1].Score, f[2].Score, "PRECONDITION: a tie")
	assert.Equal(t, []string{b.ID, a.ID}, []string{f[1].ID, f[2].ID}, "ties order by ID, not by input order")
}

func TestDebtValidateCoherence_ConfigReadFailures(t *testing.T) {
	dir, root := coherenceStore(t)
	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("HOME", "")
		code, doc, _, _ := runCoherence(t, dir, root)
		require.Equal(t, 0, code)
		assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "config-unavailable"}, doc.Coherence)
	})
	t.Run("unreadable project config", func(t *testing.T) {
		coherenceEnv(t, "", "warn")
		proot := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(proot, ".atcr", "config.yaml"), 0o755))
		code, doc, _, _ := runCoherence(t, dir, proot)
		require.Equal(t, 0, code)
		assert.Equal(t, debtValidateCoherence{Status: "skipped", Reason: "config-unavailable"}, doc.Coherence)
	})
}
