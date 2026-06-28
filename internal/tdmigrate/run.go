package tdmigrate

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const (
	defaultREADME = ".planning/technical-debt/README.md"
	defaultItems  = ".planning/technical-debt/items"
)

// Run is the CLI entry point, kept in the package (not in cmd/) so the whole
// dispatch surface is unit-testable. cmd/td-migrate is a one-line shim over it.
//
// Subcommands:
//
//	migrate  [--readme PATH] [--items DIR]   parse the README table -> write shards
//	generate [--items DIR]                   read shards -> print regenerated ToC to stdout
//	validate [--items DIR]                   strict-load + schema-check every shard
//
// generate NEVER overwrites the README (additive/proven-only this epic); it
// always writes the regenerated table to stdout for inspection.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage)
		return 2
	}
}

const usage = `td-migrate — migrate technical-debt storage to shard-by-source YAML (additive)

Usage:
  td-migrate migrate  [--readme PATH] [--items DIR]   parse README table -> write shards
  td-migrate generate [--items DIR]                   shards -> regenerated ToC table (stdout)
  td-migrate validate [--items DIR]                   strict-load + schema-check shards`

func newFlags(name string, args []string, stderr io.Writer) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	readme := fs.String("readme", defaultREADME, "path to the technical-debt README.md")
	items := fs.String("items", defaultItems, "path to the shard directory")
	return fs, readme, items
}

// newItemsFlags builds an items-only flag set for the shard-consuming
// subcommands (generate, validate). --readme is migrate-only: registering it
// here too would silently accept and ignore it, so these subcommands omit it and
// reject it as an unknown flag (exit 2) instead.
func newItemsFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	items := fs.String("items", defaultItems, "path to the shard directory")
	return fs, items
}

func runMigrate(args []string, stdout, stderr io.Writer) int {
	fs, readme, items := newFlags("migrate", args, stderr)
	allowEmpty := fs.Bool("allow-empty", false, "permit writing when the README parses to zero sections (wipes the shard store)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	data, err := os.ReadFile(*readme)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: read README: %v\n", err)
		return 1
	}
	shards, err := ParseREADME(string(data))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: parse README: %v\n", err)
		return 1
	}
	if len(shards) == 0 && !*allowEmpty {
		_, _ = fmt.Fprintln(stderr, "migrate: parsed 0 sections; refusing to wipe the shard store (pass --allow-empty to override)")
		return 1
	}
	for _, s := range shards {
		if err := s.Validate(); err != nil {
			_, _ = fmt.Fprintf(stderr, "migrate: refusing to write invalid shard %s/%s: %v\n", s.Date, s.Label, err)
			return 1
		}
	}
	written, err := WriteShards(*items, shards)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: write shards: %v\n", err)
		return 1
	}
	itemCount := 0
	for _, s := range shards {
		itemCount += len(s.Items)
	}
	_, _ = fmt.Fprintf(stdout, "migrate: wrote %d shard(s), %d item(s) to %s\n", len(written), itemCount, *items)
	return 0
}

func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs, items := newItemsFlags("generate", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	shards, err := LoadShards(*items)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "generate: load shards: %v\n", err)
		return 1
	}
	table, err := GenerateTable(shards)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "generate: render table: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprint(stdout, table)
	return 0
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	fs, items := newItemsFlags("validate", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	count, err := ValidateDir(*items)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "validate: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "validate: %d shard(s) OK\n", count)
	return 0
}
