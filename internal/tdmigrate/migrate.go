package tdmigrate

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// DefaultReadmePath and DefaultItemsDir are the conventional locations under a
// repository's .planning tree.
const (
	DefaultReadmePath = ".planning/technical-debt/README.md"
	DefaultItemsDir   = ".planning/technical-debt/items"
)

// Migrate reads the README table at readmePath and writes one Markdown+frontmatter
// file per item into itemsDir, returning the number of files written. It does not
// modify the README (additive coexistence model).
func Migrate(readmePath, itemsDir string) (int, error) {
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		return 0, fmt.Errorf("read readme: %w", err)
	}
	_, items, err := ParseReadme(string(raw))
	if err != nil {
		return 0, fmt.Errorf("parse readme: %w", err)
	}
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		return 0, fmt.Errorf("create items dir: %w", err)
	}

	written := 0
	for _, it := range items {
		content, rerr := RenderItemFile(it)
		if rerr != nil {
			return written, fmt.Errorf("render %s: %w", it.ID, rerr)
		}
		dest := filepath.Join(itemsDir, it.Filename())
		if werr := os.WriteFile(dest, []byte(content), 0o644); werr != nil {
			return written, fmt.Errorf("write %s: %w", dest, werr)
		}
		written++
	}
	return written, nil
}

// Generate reads the item files in itemsDir (TD-*.md, sorted by name) and writes
// the regenerated README data-section tables to w. This is the reverse of
// Migrate and serves as the human-readable summary generator.
func Generate(itemsDir string, w io.Writer) error {
	items, err := LoadItems(itemsDir)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, RenderTable(items))
	return err
}

// LoadItems reads and parses every TD-*.md file in itemsDir, ordered by each
// item's Order field (falling back to filename order for stability).
func LoadItems(itemsDir string) ([]Item, error) {
	matches, err := filepath.Glob(filepath.Join(itemsDir, "TD-*.md"))
	if err != nil {
		return nil, fmt.Errorf("glob items: %w", err)
	}
	sort.Strings(matches)

	items := make([]Item, 0, len(matches))
	for _, path := range matches {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, fmt.Errorf("read %s: %w", path, rerr)
		}
		it, perr := ParseItemFile(string(raw))
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, perr)
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	return items, nil
}

const usage = `td-migrate: one-off technical-debt format migration aid (Epic 12.1)

Usage:
  td-migrate migrate  [-readme PATH] [-items DIR]   README table -> per-item files
  td-migrate generate [-items DIR]                  per-item files -> README table (stdout)
`

// Main is the testable CLI entry point. It returns a process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		readme := fs.String("readme", DefaultReadmePath, "path to the technical-debt README")
		items := fs.String("items", DefaultItemsDir, "output directory for per-item files")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		n, err := Migrate(*readme, *items)
		if err != nil {
			fmt.Fprintf(stderr, "migrate: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Wrote %d item file(s) to %s\n", n, *items)
		return 0

	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		items := fs.String("items", DefaultItemsDir, "directory of per-item files")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if err := Generate(*items, stdout); err != nil {
			fmt.Fprintf(stderr, "generate: %v\n", err)
			return 1
		}
		return 0

	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0

	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n\n%s", sub, usage)
		return 2
	}
}
