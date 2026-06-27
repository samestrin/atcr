// Package tdmigrate converts the flat Markdown technical-debt table at
// .planning/technical-debt/README.md into a directory of per-item Markdown
// files with YAML frontmatter (and back), without touching any of the live
// tooling that still reads the table. It is a one-off migration aid for the
// additive coexistence model adopted in Epic 12.1.
package tdmigrate

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Item is a single technical-debt entry. Structured metadata round-trips
// through YAML frontmatter; the free-form Problem and Fix text round-trips
// through the Markdown body, where it may span multiple lines.
type Item struct {
	ID            string // e.g. "TD-0001"
	Order         int    // global 1-based sequence in document order
	Section       string // verbatim section header inner text, e.g. "[2026-06-26] From Sprint: epic-11.2"
	Date          string // "YYYY-MM-DD" parsed from Section
	Group         string // original group id, e.g. "1" or "U"
	Status        string // open | resolved | deferred
	Severity      string // CRITICAL | HIGH | MEDIUM | LOW
	File          string // path:line citation
	Problem       string // free-form, may be multi-line
	Fix           string // free-form, may be multi-line
	Category      string
	EstMinutes    string // kept as string to preserve "0" and exact text
	Source        string
	Reviewers     string // "" when the section has no reviewer columns
	Confidence    string // "" when the section has no reviewer columns
	HasReviewCols bool   // section carried Reviewers|Confidence columns
}

// itemMeta is the YAML frontmatter projection of an Item. The free-form
// Problem/Fix text lives in the Markdown body, not here, so multi-line content
// never has to survive YAML scalar quoting.
type itemMeta struct {
	ID            string `yaml:"id"`
	Order         int    `yaml:"order"`
	Section       string `yaml:"section"`
	Date          string `yaml:"date"`
	Group         string `yaml:"group"`
	Status        string `yaml:"status"`
	Severity      string `yaml:"severity"`
	File          string `yaml:"file"`
	Category      string `yaml:"category"`
	EstMinutes    string `yaml:"est_minutes"`
	Source        string `yaml:"source"`
	Reviewers     string `yaml:"reviewers"`
	Confidence    string `yaml:"confidence"`
	HasReviewCols bool   `yaml:"has_review_cols"`
}

const (
	problemHeading = "## Problem"
	fixHeading     = "## Fix"
	frontDelim     = "---"
)

// RenderItemFile serializes an Item to a Markdown document with YAML frontmatter.
func RenderItemFile(it Item) (string, error) {
	meta := itemMeta{
		ID:            it.ID,
		Order:         it.Order,
		Section:       it.Section,
		Date:          it.Date,
		Group:         it.Group,
		Status:        it.Status,
		Severity:      it.Severity,
		File:          it.File,
		Category:      it.Category,
		EstMinutes:    it.EstMinutes,
		Source:        it.Source,
		Reviewers:     it.Reviewers,
		Confidence:    it.Confidence,
		HasReviewCols: it.HasReviewCols,
	}
	front, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal frontmatter: %w", err)
	}

	var b strings.Builder
	b.WriteString(frontDelim + "\n")
	b.Write(front) // yaml.Marshal output already ends with a newline
	b.WriteString(frontDelim + "\n\n")
	b.WriteString(problemHeading + "\n\n")
	b.WriteString(strings.TrimRight(it.Problem, "\n") + "\n\n")
	b.WriteString(fixHeading + "\n\n")
	b.WriteString(strings.TrimRight(it.Fix, "\n") + "\n")
	return b.String(), nil
}

// ParseItemFile parses a Markdown+frontmatter document back into an Item.
func ParseItemFile(content string) (Item, error) {
	if !strings.HasPrefix(content, frontDelim+"\n") {
		return Item{}, fmt.Errorf("missing opening frontmatter delimiter")
	}
	rest := content[len(frontDelim)+1:]
	end := strings.Index(rest, "\n"+frontDelim+"\n")
	if end < 0 {
		return Item{}, fmt.Errorf("missing closing frontmatter delimiter")
	}
	front := rest[:end+1] // include trailing newline of the last yaml line
	body := rest[end+len("\n"+frontDelim+"\n"):]

	var meta itemMeta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return Item{}, fmt.Errorf("unmarshal frontmatter: %w", err)
	}

	problem, fix, err := splitBody(body)
	if err != nil {
		return Item{}, err
	}

	return Item{
		ID:            meta.ID,
		Order:         meta.Order,
		Section:       meta.Section,
		Date:          meta.Date,
		Group:         meta.Group,
		Status:        meta.Status,
		Severity:      meta.Severity,
		File:          meta.File,
		Problem:       problem,
		Fix:           fix,
		Category:      meta.Category,
		EstMinutes:    meta.EstMinutes,
		Source:        meta.Source,
		Reviewers:     meta.Reviewers,
		Confidence:    meta.Confidence,
		HasReviewCols: meta.HasReviewCols,
	}, nil
}

// splitBody extracts the Problem and Fix sections from an item Markdown body.
func splitBody(body string) (problem, fix string, err error) {
	pIdx := strings.Index(body, problemHeading)
	if pIdx < 0 {
		return "", "", fmt.Errorf("missing %q section", problemHeading)
	}
	fIdx := strings.Index(body, fixHeading)
	if fIdx < 0 || fIdx < pIdx {
		return "", "", fmt.Errorf("missing %q section", fixHeading)
	}
	problem = strings.TrimSpace(body[pIdx+len(problemHeading) : fIdx])
	fix = strings.TrimSpace(body[fIdx+len(fixHeading):])
	return problem, fix, nil
}

// Filename returns the on-disk name for the item, e.g. "TD-0001-some-slug.md".
func (it Item) Filename() string {
	slug := slugify(it.Problem)
	if slug == "" {
		return it.ID + ".md"
	}
	return it.ID + "-" + slug + ".md"
}

const slugMaxLen = 50

// slugify produces a lowercase, hyphen-separated, filesystem-safe slug capped
// at slugMaxLen characters on a word boundary.
func slugify(s string) string {
	var b strings.Builder
	lastHyphen := true // suppress leading hyphen
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
		if b.Len() >= slugMaxLen {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

var statusBox = map[string]string{
	"open":     "[ ]",
	"resolved": "[x]",
	"deferred": "[/]",
}

func statusToBox(status string) (string, error) {
	box, ok := statusBox[status]
	if !ok {
		return "", fmt.Errorf("unknown status %q", status)
	}
	return box, nil
}

func boxToStatus(box string) (string, error) {
	for status, b := range statusBox {
		if b == box {
			return status, nil
		}
	}
	return "", fmt.Errorf("unknown checkbox %q", box)
}
