package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
	toon "github.com/toon-format/toon-go"

	"github.com/samestrin/atcr/internal/localdebt"
)

// group-td's strategies, defaults and fixed labels (llm-tools
// internal/support/commands/group_td.go, v1.5.0).
const (
	groupByPath     = "path"
	groupByCategory = "category"
	groupByFile     = "file"

	debtGroupRootTheme     = "misc"
	debtGroupCriticalTheme = "critical"
)

// The frontend-detection rule group-td computes per group, copied with its
// lists unchanged so a group's frontend flag means what it meant there.
var (
	debtGroupFrontendExts     = []string{".tsx", ".jsx", ".svelte", ".vue", ".html", ".css", ".scss"}
	debtGroupFrontendKeywords = []string{"accessibility", "ux", "layout", "ui", "responsive", "visual", "css", "touch", "frontend"}
	debtGroupReflexPyKeywords = []string{"ux", "accessibility", "layout", "ui"}
)

// debtGroup is one of group-td's groups with store records for items. It has no
// number: group-td numbers groups only under --assign-numbers, which also writes
// a GROUP label into each item, and the store keeps no group field.
type debtGroup struct {
	Theme        string             `json:"theme"`
	PathPattern  string             `json:"path_pattern"`
	Items        []localdebt.Record `json:"items"`
	Count        int                `json:"count"`
	TotalMinutes int                `json:"total_minutes"`
	Frontend     bool               `json:"frontend"`
}

// debtGroupSummary is group-td's summary, key for key.
type debtGroupSummary struct {
	TotalItems     int `json:"total_items"`
	GroupedCount   int `json:"grouped_count"`
	UngroupedCount int `json:"ungrouped_count"`
	GroupCount     int `json:"group_count"`
}

type debtGroupDoc struct {
	Groups    []debtGroup        `json:"groups"`
	Ungrouped []localdebt.Record `json:"ungrouped"`
	Summary   debtGroupSummary   `json:"summary"`
}

// newDebtGroupCmd is `llm-support group-td` over the store: it clusters the open
// items by directory prefix, category or file, with CRITICAL items in their own
// group. The grouping is computed on every run and never stored, so the command
// writes nothing.
func newDebtGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Group the open technical-debt items by path, category or file; read-only, emits {groups, ungrouped, summary}",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  runDebtGroup,
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().String("group-by", groupByPath, "grouping strategy: path (directory prefix) | category | file (exact path)")
	cmd.Flags().Int("path-depth", 2, "number of directory segments in a path theme (--group-by path)")
	cmd.Flags().Int("min-group-size", 3, "fewest items that form a group; smaller themes are ungrouped (CRITICAL items always form one)")
	cmd.Flags().Bool("json", false, "emit {groups, ungrouped, summary} as JSON (the default output)")
	cmd.Flags().Bool("axi", false, "emit {groups, ungrouped, summary} as a token-dense TOON payload for agent consumption")
	// Registered and rejected for the reason debt filter gives.
	cmd.Flags().Bool("legacy-pipe", false, "not supported by this command; rejected")
	_ = cmd.Flags().MarkHidden("legacy-pipe")
	cmd.Annotations = map[string]string{noLegacyPipeAnnotation: "true"}
	return cmd
}

func runDebtGroup(cmd *cobra.Command, _ []string) error {
	if legacy, _ := cmd.Flags().GetBool("legacy-pipe"); legacy {
		return usageError(fmt.Errorf("--legacy-pipe is not supported by debt group; --axi emits standard TOON"))
	}
	groupBy := strings.ToLower(strings.TrimSpace(mustFlag(cmd, "group-by")))
	switch groupBy {
	case groupByPath, groupByCategory, groupByFile:
	default:
		return usageError(fmt.Errorf("invalid --group-by %q: expected path|category|file", mustFlag(cmd, "group-by")))
	}
	depth, _ := cmd.Flags().GetInt("path-depth")
	if depth < 1 {
		return usageError(fmt.Errorf("invalid --path-depth %d: expected at least 1", depth))
	}
	minSize, _ := cmd.Flags().GetInt("min-group-size")
	if minSize < 1 {
		return usageError(fmt.Errorf("invalid --min-group-size %d: expected at least 1", minSize))
	}

	// group-td groups the rows it is handed, in their order; the store has no
	// order, so the open items are read in file:line order.
	recs, err := selectDebtForList(cmd, debtFilter{Status: statusOpen}, sortKeyFile, 0)
	if err != nil {
		return err
	}
	doc := groupDebt(recs, groupBy, depth, minSize)

	out := cmd.OutOrStdout()
	if axiFromContext(cmd.Context()) {
		return renderDebtGroupAXI(out, doc)
	}
	return renderDebtDocJSON(out, doc)
}

// groupDebt is group-td's groupItems without --assign-numbers and with the
// critical override always on: CRITICAL items form one group whatever their
// count, every other item joins its theme's group when the theme has at least
// minSize items and is ungrouped otherwise. Items keep their input order within a
// group; groups are ordered critical first, then by theme name.
func groupDebt(recs []localdebt.Record, groupBy string, depth, minSize int) debtGroupDoc {
	var critical, regular []localdebt.Record
	for _, r := range recs {
		if strings.ToUpper(r.Severity) == "CRITICAL" {
			critical = append(critical, r)
		} else {
			regular = append(regular, r)
		}
	}
	themes := make([]string, len(regular))
	counts := map[string]int{}
	for i, r := range regular {
		themes[i] = debtGroupTheme(r, groupBy, depth)
		counts[themes[i]]++
	}

	byTheme := map[string]*debtGroup{}
	ungrouped := []localdebt.Record{}
	for i, r := range regular {
		theme := themes[i]
		if counts[theme] < minSize {
			ungrouped = append(ungrouped, r)
			continue
		}
		g := byTheme[theme]
		if g == nil {
			g = &debtGroup{Theme: theme, PathPattern: debtGroupPattern(theme, groupBy)}
			byTheme[theme] = g
		}
		g.Items = append(g.Items, r)
	}
	names := make([]string, 0, len(byTheme))
	for theme := range byTheme {
		names = append(names, theme)
	}
	// A theme can itself be "critical" (a critical/ directory, a category).
	// group-td sorts it first, as here, but when CRITICAL items exist its map
	// lets their group overwrite the theme, and the run dies on its own
	// data-loss check. Here the theme follows the CRITICAL group under its name.
	sort.Slice(names, func(i, j int) bool {
		if ci, cj := names[i] == debtGroupCriticalTheme, names[j] == debtGroupCriticalTheme; ci != cj {
			return ci
		}
		return names[i] < names[j]
	})

	groups := []debtGroup{}
	if len(critical) > 0 {
		groups = append(groups, debtGroup{Theme: debtGroupCriticalTheme, PathPattern: "CRITICAL severity items", Items: critical})
	}
	for _, theme := range names {
		groups = append(groups, *byTheme[theme])
	}
	doc := debtGroupDoc{Groups: groups, Ungrouped: ungrouped}
	for i := range doc.Groups {
		g := &doc.Groups[i]
		g.Count = len(g.Items)
		for _, r := range g.Items {
			g.TotalMinutes += r.EstMinutes
			g.Frontend = g.Frontend || debtGroupFrontend(r)
		}
		doc.Summary.GroupedCount += g.Count
	}
	doc.Summary.TotalItems = len(recs)
	doc.Summary.UngroupedCount = len(ungrouped)
	doc.Summary.GroupCount = len(groups)
	return doc
}

// debtGroupTheme is group-td's extractTheme over a store record.
func debtGroupTheme(r localdebt.Record, groupBy string, depth int) string {
	categoryTheme := func() string {
		if r.Category == "" {
			return debtGroupRootTheme
		}
		return strings.ToLower(strings.ReplaceAll(r.Category, " ", "-"))
	}
	switch groupBy {
	case groupByCategory:
		return categoryTheme()
	case groupByFile:
		if r.File == "" {
			return debtGroupRootTheme
		}
		return strings.ReplaceAll(r.File, "/", "-")
	default:
		if r.File == "" {
			return categoryTheme()
		}
		return debtGroupPathTheme(r.File, depth)
	}
}

// debtGroupPathTheme is group-td's extractPathTheme: the first depth directory
// segments of file, joined with "-" and lower-cased; a file at the root is misc.
func debtGroupPathTheme(file string, depth int) string {
	dir := filepath.Dir(strings.ReplaceAll(file, "\\", "/"))
	if dir == "." || dir == "" {
		return debtGroupRootTheme
	}
	var segs []string
	for _, s := range strings.Split(filepath.ToSlash(dir), "/") {
		if s != "" && s != "." {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return debtGroupRootTheme
	}
	if len(segs) > depth {
		segs = segs[:depth]
	}
	return strings.ToLower(strings.Join(segs, "-"))
}

// debtGroupPattern is group-td's buildPathPattern.
func debtGroupPattern(theme, groupBy string) string {
	switch groupBy {
	case groupByCategory:
		return "CATEGORY=" + theme
	case groupByFile:
		return strings.ReplaceAll(theme, "-", "/")
	default:
		return strings.ReplaceAll(theme, "-", "/") + "/*"
	}
}

// debtGroupFrontend is group-td's detectFrontendItem: a frontend file
// extension, a .py file with a UI category, or a UI category keyword.
func debtGroupFrontend(r localdebt.Record) bool {
	ext := strings.ToLower(filepath.Ext(r.File))
	category := strings.ToLower(r.Category)
	hasKeyword := func(kws []string) bool {
		for _, kw := range kws {
			if strings.Contains(category, kw) {
				return true
			}
		}
		return false
	}
	for _, fe := range debtGroupFrontendExts {
		if ext == fe {
			return true
		}
	}
	if ext == ".py" && hasKeyword(debtGroupReflexPyKeywords) {
		return true
	}
	return category != "" && hasKeyword(debtGroupFrontendKeywords)
}

// renderDebtGroupAXI emits {groups, ungrouped, summary} as standard TOON: each
// group an object whose items are the tabular array debt filter uses, ungrouped
// the same array, summary an object.
func renderDebtGroupAXI(w io.Writer, doc debtGroupDoc) error {
	groups := make([]toon.Object, 0, len(doc.Groups))
	for _, g := range doc.Groups {
		groups = append(groups, toon.NewObject(
			toon.Field{Key: "theme", Value: goaxi.SanitizeString(g.Theme)},
			toon.Field{Key: "path_pattern", Value: goaxi.SanitizeString(g.PathPattern)},
			toon.Field{Key: "count", Value: g.Count},
			toon.Field{Key: "total_minutes", Value: g.TotalMinutes},
			toon.Field{Key: "frontend", Value: g.Frontend},
			toon.Field{Key: "items", Value: debtItemAXIRows(g.Items)},
		))
	}
	s := doc.Summary
	payload := toon.NewObject(
		toon.Field{Key: "groups", Value: groups},
		toon.Field{Key: "ungrouped", Value: debtItemAXIRows(doc.Ungrouped)},
		toon.Field{Key: "summary", Value: toon.NewObject(
			toon.Field{Key: "total_items", Value: s.TotalItems},
			toon.Field{Key: "grouped_count", Value: s.GroupedCount},
			toon.Field{Key: "ungrouped_count", Value: s.UngroupedCount},
			toon.Field{Key: "group_count", Value: s.GroupCount},
		)},
	)
	if _, err := goaxi.EncodeChecked(w, payload); err != nil {
		return fmt.Errorf("axi encoder: %w", err)
	}
	return nil
}
