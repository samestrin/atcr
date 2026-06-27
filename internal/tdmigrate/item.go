// Package tdmigrate migrates technical-debt storage from the single Markdown
// table at .planning/technical-debt/README.md to YAML files sharded by source
// under .planning/technical-debt/items/, and regenerates the README table-of-
// contents from those shards (round-trip). It is additive: no existing tooling
// reads the shards yet, and the README table stays authoritative. See
// .planning/epics/active/12.1_technical_debt_format_migration.md.
package tdmigrate

import (
	"fmt"
	"regexp"
	"strings"
)

// Status values map 1:1 to the README checkbox tokens.
const (
	StatusOpen     = "open"     // [ ]
	StatusDeferred = "deferred" // [/]
	StatusResolved = "resolved" // [x]

	// SourceTypeSprint and SourceTypeReview are the two `From <...>:` variants.
	SourceTypeSprint = "Sprint"
	SourceTypeReview = "Review"
)

var validSeverities = map[string]bool{
	"CRITICAL": true,
	"HIGH":     true,
	"MEDIUM":   true,
	"LOW":      true,
}

var validStatuses = map[string]bool{
	StatusOpen:     true,
	StatusDeferred: true,
	StatusResolved: true,
}

// fileLinePattern requires the File field to end with a :<line> suffix. The path
// portion is intentionally permissive (any non-empty string) because File is
// primarily a human-readable pointer, not a filesystem locator.
var fileLinePattern = regexp.MustCompile(`^.+:\d+$`)

// Item is one technical-debt entry. Long-form fields (Problem, Fix, Notes) are
// emitted as YAML block scalars by yaml.v3 Marshal, which quotes/escapes by
// construction so values that look like other YAML types stay strings.
type Item struct {
	Group      string   `yaml:"group"`                // positional group label from the table ("U" or a number)
	Status     string   `yaml:"status"`               // open | deferred | resolved
	Severity   string   `yaml:"severity"`             // CRITICAL | HIGH | MEDIUM | LOW
	File       string   `yaml:"file"`                 // file:line (or range, or free text)
	Problem    string   `yaml:"problem"`              // long-form, multi-line allowed
	Fix        string   `yaml:"fix"`                  // long-form, multi-line allowed
	Category   string   `yaml:"category"`             // free-form label (correctness, security, ...)
	EstMinutes int      `yaml:"est_minutes"`          // best-guess effort, >= 0
	Source     string   `yaml:"source"`               // capture origin (code-review, execute-epic-*, ...)
	Reviewers  []string `yaml:"reviewers,omitempty"`  // reconciled sections only
	Confidence string   `yaml:"confidence,omitempty"` // reconciled sections only
	Notes      string   `yaml:"notes,omitempty"`      // optional long-form, for hand-editing
}

// Shard is one source section: every item captured from a single
// `### [date] From <Sprint|Review>: <label>` provenance unit.
type Shard struct {
	Date       string `yaml:"date"`        // YYYY-MM-DD
	SourceType string `yaml:"source_type"` // Sprint | Review
	Label      string `yaml:"label"`       // e.g. epic-11.2
	Items      []Item `yaml:"items"`
}

// NormalizeSeverity upper-cases and trims a severity token.
func NormalizeSeverity(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// CheckboxToStatus maps a README checkbox token to a status enum.
func CheckboxToStatus(box string) (string, error) {
	switch strings.TrimSpace(box) {
	case "[ ]":
		return StatusOpen, nil
	case "[/]":
		return StatusDeferred, nil
	case "[x]", "[X]":
		return StatusResolved, nil
	default:
		return "", fmt.Errorf("unknown checkbox token %q", box)
	}
}

// StatusToCheckbox maps a status enum back to its README checkbox token.
func StatusToCheckbox(status string) (string, error) {
	switch status {
	case StatusOpen:
		return "[ ]", nil
	case StatusDeferred:
		return "[/]", nil
	case StatusResolved:
		return "[x]", nil
	default:
		return "", fmt.Errorf("unknown status %q", status)
	}
}

// Validate schema-checks a single item: required fields present, enums valid.
func (it Item) Validate() error {
	if !validSeverities[it.Severity] {
		return fmt.Errorf("invalid severity %q (want CRITICAL|HIGH|MEDIUM|LOW)", it.Severity)
	}
	if !validStatuses[it.Status] {
		return fmt.Errorf("invalid status %q (want open|deferred|resolved)", it.Status)
	}
	for _, req := range []struct {
		field, val string
	}{
		{"group", it.Group},
		{"file", it.File},
		{"problem", it.Problem},
		{"fix", it.Fix},
		{"category", it.Category},
		{"source", it.Source},
	} {
		if strings.TrimSpace(req.val) == "" {
			return fmt.Errorf("%s is required", req.field)
		}
	}
	if it.EstMinutes < 0 {
		return fmt.Errorf("est_minutes must be >= 0, got %d", it.EstMinutes)
	}
	return nil
}

// ValidateFileFormat is an optional, stricter check that the File field matches
// the conventional path:line format. The default Validate only requires File to
// be non-empty because the table historically stores ranges, multiple files,
// and free-text pointers; callers that need the stricter contract can call this.
func (it Item) ValidateFileFormat() error {
	if !fileLinePattern.MatchString(it.File) {
		return fmt.Errorf("file %q must match path:line format", it.File)
	}
	return nil
}

// Validate schema-checks a shard and every item it contains.
func (s Shard) Validate() error {
	if strings.TrimSpace(s.Date) == "" {
		return fmt.Errorf("date is required")
	}
	if s.SourceType != SourceTypeSprint && s.SourceType != SourceTypeReview {
		return fmt.Errorf("invalid source_type %q (want Sprint|Review)", s.SourceType)
	}
	if strings.TrimSpace(s.Label) == "" {
		return fmt.Errorf("label is required")
	}
	if len(s.Items) == 0 {
		return fmt.Errorf("shard has no items")
	}
	for i, it := range s.Items {
		if err := it.Validate(); err != nil {
			return fmt.Errorf("item %d: %w", i, err)
		}
	}
	return nil
}
