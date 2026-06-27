// Package tdmigrate converts the flat Markdown technical-debt table at
// .planning/technical-debt/README.md into a directory of per-item Markdown
// files with YAML frontmatter (and back), without touching any of the live
// tooling that still reads the table. It is a one-off migration aid for the
// additive coexistence model adopted in Epic 12.1.
package tdmigrate

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

// RenderItemFile serializes an Item to a Markdown document with YAML frontmatter.
func RenderItemFile(it Item) (string, error) {
	return "", nil
}

// ParseItemFile parses a Markdown+frontmatter document back into an Item.
func ParseItemFile(content string) (Item, error) {
	return Item{}, nil
}

// Filename returns the on-disk name for the item, e.g. "TD-0001-some-slug.md".
func (it Item) Filename() string {
	return ""
}

func statusToBox(status string) (string, error) {
	return "", nil
}

func boxToStatus(box string) (string, error) {
	return "", nil
}
