package personas

import (
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
	builtins "github.com/samestrin/atcr/personas"
	"gopkg.in/yaml.v3"
)

// PersonaMeta is one row of `atcr personas list`.
type PersonaMeta struct {
	Name     string
	Version  string
	Source   string // "built-in" | "community"
	Language []string
}

// personaFileMeta captures the persona-file metadata read from an installed
// community YAML (a superset of registry.AgentConfig).
type personaFileMeta struct {
	Version  string   `yaml:"version"`
	Language []string `yaml:"language"`
}

// List returns metadata for the nine built-in personas plus every community
// persona installed under personasDir. A missing directory yields just the
// built-ins (no error). A directory that exists but cannot be walked yields the
// built-ins gathered so far plus a non-nil error, so the caller can warn yet
// still render the built-ins.
func List(personasDir string) ([]PersonaMeta, error) {
	names := builtins.Names()
	metas := make([]PersonaMeta, 0, len(names))
	for _, n := range names {
		metas = append(metas, PersonaMeta{Name: n, Version: "built-in", Source: "built-in"})
	}
	community, err := listCommunity(personasDir)
	metas = append(metas, community...)
	return metas, err
}

// ScoredPersona is one row of `personas list --scores`: a persona joined with
// its corroboration rate. Rate is nil when the persona has no scorecard data,
// which renders as "n/a" (distinct from a real 0.0 rate).
type ScoredPersona struct {
	PersonaMeta
	Rate *float64
}

// ListWithScores returns the personas from List joined with corroboration rates
// from scores (keyed by lowercase persona name, as built by the caller from
// scorecard.Aggregate). The result is sorted by rate descending, then n/a rows
// alphabetically after all numeric rows. A directory walk error is returned
// alongside the rows gathered so far, mirroring List.
func ListWithScores(personasDir string, scores map[string]float64) ([]ScoredPersona, error) {
	metas, err := List(personasDir)
	scored := make([]ScoredPersona, 0, len(metas))
	for _, m := range metas {
		sp := ScoredPersona{PersonaMeta: m}
		if rate, ok := scores[strings.ToLower(m.Name)]; ok && !math.IsNaN(rate) {
			r := rate
			sp.Rate = &r
		}
		scored = append(scored, sp)
	}
	sortScoredPersonas(scored)
	return scored, err
}

// sortScoredPersonas orders rows by corroboration rate descending, breaking ties
// alphabetically by name; rows with no data (nil rate, "n/a") sort after all
// numeric rows, alphabetically among themselves. Deterministic for any input.
// Precondition: rates are finite (scorecard ratios in [0,1]); a NaN rate would
// violate the comparator's strict-weak ordering. The scorecard producer guards
// division-by-zero, so a NaN never reaches here.
func sortScoredPersonas(ps []ScoredPersona) {
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		switch {
		case a.Rate != nil && b.Rate != nil:
			if *a.Rate != *b.Rate {
				return *a.Rate > *b.Rate
			}
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case a.Rate != nil: // numeric sorts before n/a
			return true
		case b.Rate != nil:
			return false
		default: // both n/a → alphabetical
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
	})
}

// FormatRate renders a corroboration rate as "XX.X%" (clamped to [0,100]) or
// "n/a" when nil (no scorecard data).
func FormatRate(rate *float64) string {
	if rate == nil {
		return "n/a"
	}
	pct := *rate * 100
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("%.1f%%", pct)
}

func listCommunity(personasDir string) ([]PersonaMeta, error) {
	if _, err := os.Stat(personasDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read personas directory %s: %w", personasDir, err)
	}
	var out []PersonaMeta
	walkErr := filepath.WalkDir(personasDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil // skip directories and symlinks (symlinks may point outside the personas dir)
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil // silently skip non-YAML files (.DS_Store, .gitkeep, ...)
		}
		rel, err := filepath.Rel(personasDir, path)
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		meta := PersonaMeta{Name: name, Version: "-", Source: "community"}
		if data, readErr := os.ReadFile(path); readErr == nil {
			var fm personaFileMeta
			if yaml.Unmarshal(data, &fm) == nil {
				if strings.TrimSpace(fm.Version) != "" {
					meta.Version = fm.Version
				}
				for _, l := range fm.Language {
					if c := registry.NormalizeLanguageToken(l); c != "" {
						meta.Language = append(meta.Language, c)
					}
				}
			}
		}
		out = append(out, meta)
		return nil
	})
	return out, walkErr
}
