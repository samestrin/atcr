package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/registry"
	"github.com/samestrin/atcr/internal/semindex"
)

// Grounding statuses. line-past-end reports as file_missing: the location the
// row points at does not exist, and the reason field says which half failed.
const (
	validateValid          = "valid"
	validateFileMissing    = "file_missing"
	validateSymbolNotFound = "symbol_not_found"
	validateNoFile         = "no_file"
)

type debtValidateItem struct {
	ID     string `json:"id"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Symbol string `json:"symbol,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Coherence statuses. checked carries a reason only when nothing was scored;
// skipped always carries one: not-requested, not-configured,
// config-unavailable, config-invalid, secret-guard-blocked, or the
// semindex failure class of the embedding call.
const (
	coherenceChecked = "checked"
	coherenceSkipped = "skipped"
)

// debtValidateCoherence is the --coherence result. A skipped result is
// {status, reason} only (MarshalJSON); a checked one always lists flagged.
type debtValidateCoherence struct {
	Status     string             `json:"status"`
	Reason     string             `json:"reason,omitempty"`
	Percentile int                `json:"percentile,omitempty"`
	Scored     int                `json:"scored"`
	Flagged    []debtCoherenceRow `json:"flagged"`
}

// debtCoherenceRow is one flagged row. It carries no row text: the Problem and
// Fix stay in the store.
type debtCoherenceRow struct {
	ID    string  `json:"id"`
	File  string  `json:"file"`
	Line  int     `json:"line"`
	Score float64 `json:"score"`
}

func (c debtValidateCoherence) MarshalJSON() ([]byte, error) {
	if c.Status != coherenceChecked {
		return json.Marshal(struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}{c.Status, c.Reason})
	}
	type full debtValidateCoherence
	f := full(c)
	if f.Flagged == nil {
		f.Flagged = []debtCoherenceRow{}
	}
	return json.Marshal(f)
}

type debtValidateDoc struct {
	Items     []debtValidateItem    `json:"items"`
	Coherence debtValidateCoherence `json:"coherence"`
}

// newDebtValidateCmd grounds each row against the working tree: the file exists,
// the line is inside it, and a symbol the Problem text names is present. With
// --coherence it also scores each row's Fix against its Problem through the
// context_prefetch.semantic embedding endpoint and flags the least coherent.
// It is read-only and advisory, so it exits 0 whatever it finds.
func newDebtValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check that each technical-debt row's file, line and named symbol exist; emits {items, coherence}",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtValidate,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("status", statusOpen, "status of the rows to validate (exact: "+debtListStatusList()+")")
	cmd.Flags().String("root", "", "directory the rows' file paths are relative to (default: the repository root)")
	cmd.Flags().Bool("coherence", false, "also score each row's Fix against its Problem through context_prefetch.semantic.embedding_endpoint and flag the least coherent")
	cmd.Flags().Int("coherence-percentile", defaultCoherencePercentile, "percent of scored rows, least coherent first, that --coherence flags (1-100)")
	cmd.Flags().Bool("json", false, "emit {items, coherence} as JSON (the default output)")
	cmd.Flags().Bool("axi", false, "emit {items, coherence} as a token-dense TOON payload for agent consumption")
	cmd.Flags().Bool("legacy-pipe", false, "not supported by this command; rejected")
	_ = cmd.Flags().MarkHidden("legacy-pipe")
	cmd.Annotations = map[string]string{noLegacyPipeAnnotation: "true"}
	return cmd
}

func runDebtValidate(cmd *cobra.Command, _ []string) error {
	if legacy, _ := cmd.Flags().GetBool("legacy-pipe"); legacy {
		return usageError(fmt.Errorf("--legacy-pipe is not supported by debt validate; --axi emits standard TOON"))
	}
	if st := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "status"))); !debtListStatuses[st] {
		return usageError(fmt.Errorf("invalid --status %q: expected %s", mustFlag(cmd, "status"), debtListStatusList()))
	}
	pct, _ := cmd.Flags().GetInt("coherence-percentile")
	if pct < 1 || pct > 100 {
		return usageError(fmt.Errorf("invalid --coherence-percentile %d: expected 1-100", pct))
	}
	root := mustFlag(cmd, "root")
	if root == "" {
		var err error
		if root, err = debtRepoRoot(); err != nil {
			return fmt.Errorf("resolve the repository root: %w", err)
		}
	}
	recs, err := selectDebtForList(cmd, debtFilter{Status: mustFlag(cmd, "status")}, sortKeyFile, 0)
	if err != nil {
		return err
	}
	doc := debtValidateDoc{
		Items:     make([]debtValidateItem, 0, len(recs)),
		Coherence: debtValidateCoherence{Status: coherenceSkipped, Reason: "not-requested"},
	}
	for _, r := range recs {
		doc.Items = append(doc.Items, validateDebtRecord(root, r))
	}
	if on, _ := cmd.Flags().GetBool("coherence"); on {
		doc.Coherence = debtCoherence(cmd.Context(), root, recs, pct)
	}
	out := cmd.OutOrStdout()
	if axiFromContext(cmd.Context()) {
		return renderDebtValidateAXI(out, doc)
	}
	return renderDebtDocJSON(out, doc)
}

var (
	validateAnchorRe   = regexp.MustCompile(`^\s*\(([A-Za-z_][A-Za-z0-9_]*)\)\s`)
	validateBacktickRe = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)`")
)

// debtProblemSymbol returns the symbol a Problem names: the leading "(name) "
// anchor, else the first backticked identifier. "" means it names none.
func debtProblemSymbol(problem string) string {
	if m := validateAnchorRe.FindStringSubmatch(problem); m != nil {
		return m[1]
	}
	if m := validateBacktickRe.FindStringSubmatch(problem); m != nil {
		return m[1]
	}
	return ""
}

func validateDebtRecord(root string, r localdebt.Record) debtValidateItem {
	it := debtValidateItem{ID: r.ID, File: r.File, Line: r.Line, Symbol: debtProblemSymbol(r.Problem)}
	if strings.TrimSpace(r.File) == "" {
		it.Status = validateNoFile
		return it
	}
	rel := filepath.FromSlash(r.File)
	if filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		it.Status, it.Reason = validateFileMissing, "path is outside the repository"
		return it
	}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		it.Status, it.Reason = validateFileMissing, "file not found"
		return it
	}
	if r.Line > 0 {
		lines := bytes.Count(data, []byte("\n"))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		if r.Line > lines {
			it.Status, it.Reason = validateFileMissing, fmt.Sprintf("line %d is past the end of the file (%d lines)", r.Line, lines)
			return it
		}
	}
	if it.Symbol != "" && !containsIdent(data, it.Symbol) {
		it.Status, it.Reason = validateSymbolNotFound, "symbol not found in file"
		return it
	}
	it.Status = validateValid
	return it
}

// containsIdent reports whether sym occurs in data as a whole identifier.
func containsIdent(data []byte, sym string) bool {
	isID := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	s, from := []byte(sym), 0
	for {
		i := bytes.Index(data[from:], s)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(s)
		if (i == 0 || !isID(data[i-1])) && (end == len(data) || !isID(data[end])) {
			return true
		}
		from = i + 1
	}
}

func renderDebtValidateAXI(w interface{ Write([]byte) (int, error) }, doc debtValidateDoc) error {
	rows := make([]toon.Object, 0, len(doc.Items))
	for _, it := range doc.Items {
		rows = append(rows, toon.NewObject(
			toon.Field{Key: "id", Value: goaxi.SanitizeString(it.ID)},
			toon.Field{Key: "file", Value: goaxi.SanitizeString(it.File)},
			toon.Field{Key: "line", Value: it.Line},
			toon.Field{Key: "symbol", Value: goaxi.SanitizeString(it.Symbol)},
			toon.Field{Key: "status", Value: it.Status},
			toon.Field{Key: "reason", Value: goaxi.SanitizeString(it.Reason)},
		))
	}
	payload := toon.NewObject(
		toon.Field{Key: "items", Value: rows},
		toon.Field{Key: "coherence", Value: coherenceAXI(doc.Coherence)},
	)
	if _, err := goaxi.EncodeChecked(w, payload); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}

func coherenceAXI(c debtValidateCoherence) toon.Object {
	if c.Status != coherenceChecked {
		return toon.NewObject(
			toon.Field{Key: "status", Value: c.Status},
			toon.Field{Key: "reason", Value: c.Reason},
		)
	}
	rows := make([]toon.Object, 0, len(c.Flagged))
	for _, f := range c.Flagged {
		rows = append(rows, toon.NewObject(
			toon.Field{Key: "id", Value: goaxi.SanitizeString(f.ID)},
			toon.Field{Key: "file", Value: goaxi.SanitizeString(f.File)},
			toon.Field{Key: "line", Value: f.Line},
			toon.Field{Key: "score", Value: f.Score},
		))
	}
	return toon.NewObject(
		toon.Field{Key: "status", Value: c.Status},
		toon.Field{Key: "reason", Value: c.Reason},
		toon.Field{Key: "percentile", Value: c.Percentile},
		toon.Field{Key: "scored", Value: c.Scored},
		toon.Field{Key: "flagged", Value: rows},
	)
}

// defaultCoherencePercentile is llm-support td-validate's least-coherent 20%.
const defaultCoherencePercentile = 20

// coherenceDest names the endpoint in secret_guard warnings, never its URL.
const coherenceDest = "the coherence embedding endpoint"

// debtCoherence scores each row that has both a Problem and a Fix by the cosine
// of their embeddings and flags the least coherent pct percent (rounded up).
// It never fails the command: every way it cannot finish is a skipped result
// whose reason is a class, never a URL or row text. Every text passes the
// secret_guard scan before anything is sent, and one failed embedding call
// skips the whole pass rather than scoring part of it.
func debtCoherence(ctx context.Context, root string, recs []localdebt.Record, pct int) debtValidateCoherence {
	skip := func(reason string) debtValidateCoherence {
		return debtValidateCoherence{Status: coherenceSkipped, Reason: reason}
	}
	settings, configured, err := loadCoherenceSettings(root)
	if err != nil {
		return skip("config-unavailable")
	}
	sp := settings.ResolvedSemanticPrefetch()
	if !configured || !sp.Enabled || sp.EmbeddingEndpoint == "" {
		return skip("not-configured")
	}
	emb, err := newCoherenceEmbedder(semindex.Config{
		EmbeddingEndpoint: sp.EmbeddingEndpoint,
		AllowInsecureHTTP: sp.AllowInsecureHTTP,
		Metrics:           metrics.DefaultRegistry,
	})
	if err != nil {
		return skip("config-invalid")
	}
	var (
		scored []localdebt.Record
		texts  []fanout.OutboundText
	)
	for _, r := range recs {
		if strings.TrimSpace(r.Problem) == "" || strings.TrimSpace(r.Fix) == "" {
			continue
		}
		scored = append(scored, r)
		texts = append(texts,
			fanout.OutboundText{Label: "row " + r.ID + " problem", Text: r.Problem},
			fanout.OutboundText{Label: "row " + r.ID + " fix", Text: r.Fix})
	}
	out := debtValidateCoherence{Status: coherenceChecked, Percentile: pct, Scored: len(scored)}
	if len(scored) == 0 {
		out.Reason = "no-rows-to-score"
		return out
	}
	send, err := fanout.GuardOutbound(ctx, settings.SecretGuard, coherenceDest, texts)
	if err != nil {
		return skip("secret-guard-blocked")
	}
	vecs, err := emb.Embed(ctx, send)
	if err != nil {
		var se *semindex.Error
		if errors.As(err, &se) {
			return skip(string(se.Class))
		}
		return skip(string(semindex.ClassUnreachable))
	}
	rows := make([]debtCoherenceRow, len(scored))
	for i, r := range scored {
		rows[i] = debtCoherenceRow{ID: r.ID, File: r.File, Line: r.Line, Score: math.Round(cosine(vecs[2*i], vecs[2*i+1])*1e4) / 1e4}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score < rows[j].Score
		}
		return rows[i].ID < rows[j].ID
	})
	n := (len(rows)*pct + 99) / 100
	out.Flagged = rows[:n]
	return out
}

// newCoherenceEmbedder is semindex.NewEmbedder, a variable so a test can reach
// the config-invalid skip: the registry loader already refuses every endpoint
// NewEmbedder would.
var newCoherenceEmbedder = semindex.NewEmbedder

// loadCoherenceSettings resolves the user registry and project config tiers
// the way a review does. configured is false when neither file exists, which
// is "no endpoint configured", not a broken config.
func loadCoherenceSettings(root string) (settings registry.Settings, configured bool, err error) {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return settings, false, err
	}
	var reg *registry.Registry
	if _, statErr := os.Stat(regPath); !errors.Is(statErr, fs.ErrNotExist) || os.Getenv("ATCR_REGISTRY_URL") != "" {
		if reg, err = registry.LoadMergedRegistry(regPath, root); err != nil {
			return settings, false, err
		}
	}
	proj, err := registry.LoadProjectConfig(registry.DefaultProjectConfigPath(root))
	if errors.Is(err, fs.ErrNotExist) {
		proj, err = nil, nil
	}
	if err != nil {
		return settings, false, err
	}
	if reg == nil && proj == nil {
		return settings, false, nil
	}
	settings, err = registry.ResolveSettings(registry.CLIOverrides{}, proj, reg)
	return settings, err == nil, err
}

// cosine is 0 when either vector is all zeros.
func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
