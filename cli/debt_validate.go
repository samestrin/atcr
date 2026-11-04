package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/localdebt"
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

// debtValidateCoherence is the placeholder the coherence pass replaces.
type debtValidateCoherence struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type debtValidateDoc struct {
	Items     []debtValidateItem    `json:"items"`
	Coherence debtValidateCoherence `json:"coherence"`
}

// newDebtValidateCmd grounds each row against the working tree: the file exists,
// the line is inside it, and a symbol the Problem text names is present. It is
// read-only and advisory, so it exits 0 whatever it finds.
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
		Coherence: debtValidateCoherence{Status: "skipped", Reason: "not-requested"},
	}
	for _, r := range recs {
		doc.Items = append(doc.Items, validateDebtRecord(root, r))
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
		toon.Field{Key: "coherence", Value: toon.NewObject(
			toon.Field{Key: "status", Value: doc.Coherence.Status},
			toon.Field{Key: "reason", Value: doc.Coherence.Reason},
		)},
	)
	if _, err := goaxi.EncodeChecked(w, payload); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}
