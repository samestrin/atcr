package personas

import (
	"errors"
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

// ListTiers returns persona metadata across the three resolver tiers in
// precedence order — project (.atcr/personas) > community (communityDir) >
// built-in (embedded), matching internal/registry.ResolvePersona's
// PersonaDirs{Project, Registry} ordering. A name present in a higher-precedence
// tier shadows the lower ones, so each persona appears once, labeled by its
// winning source. A walk error in either on-disk dir is returned alongside the
// rows gathered so far (mirroring List), so the caller can warn yet still render.
func ListTiers(projectDir, communityDir string) ([]PersonaMeta, error) {
	baseMetas, baseErr := List(communityDir) // built-ins + community
	project, projErr := listProject(projectDir)

	byName := make(map[string]PersonaMeta, len(baseMetas)+len(project))
	order := make([]string, 0, len(baseMetas)+len(project))
	add := func(m PersonaMeta) {
		key := strings.ToLower(m.Name)
		if _, seen := byName[key]; !seen {
			order = append(order, key)
		}
		byName[key] = m // later tier (project) overrides earlier at the same name
	}
	for _, m := range baseMetas {
		add(m)
	}
	for _, m := range project {
		add(m)
	}
	out := make([]PersonaMeta, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out, errors.Join(baseErr, projErr)
}

// listProject returns the project-override personas: <name>.md prompt files under
// projectDir (the .atcr/personas dir), labeled Source "project". The shared
// _base.md template and symlinks are skipped (symlinks may point outside the
// dir); nested names are reported with their slash path. A missing directory
// yields no rows and no error.
func listProject(projectDir string) ([]PersonaMeta, error) {
	if _, err := os.Stat(projectDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read project personas directory %s: %w", projectDir, err)
	}
	var out []PersonaMeta
	walkErr := filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		rel, relErr := filepath.Rel(projectDir, path)
		if relErr != nil {
			return nil
		}
		if filepath.Base(path) == "_base.md" {
			return nil // shared base template (at any depth), not a persona
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		out = append(out, PersonaMeta{Name: name, Version: "project", Source: "project"})
		return nil
	})
	return out, walkErr
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
	return joinScores(metas, scores), err
}

// ListTiersWithScores returns the personas from ListTiers joined with
// corroboration rates from scores. It mirrors ListWithScores but sources the
// persona set from the three resolver tiers (project > community > built-in)
// so the --scores table agrees with the plain list on the Source column.
func ListTiersWithScores(projectDir, communityDir string, scores map[string]float64) ([]ScoredPersona, error) {
	metas, err := ListTiers(projectDir, communityDir)
	return joinScores(metas, scores), err
}

// joinScores attaches corroboration rates to metas and sorts the result.
func joinScores(metas []PersonaMeta, scores map[string]float64) []ScoredPersona {
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
	return scored
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
	var warnings []error
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
		if isBuiltin(name) {
			warnings = append(warnings, fmt.Errorf("skipping community file %q: name collides with built-in persona %q", rel, name))
			return nil
		}
		meta := PersonaMeta{Name: name, Version: "-", Source: "community"}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			warnings = append(warnings, fmt.Errorf("could not read persona file %q: %w", rel, readErr))
		} else {
			var fm personaFileMeta
			if unmarshalErr := yaml.Unmarshal(data, &fm); unmarshalErr != nil {
				warnings = append(warnings, fmt.Errorf("could not parse persona file %q: %w", rel, unmarshalErr))
			} else {
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
	if walkErr != nil {
		return out, walkErr
	}
	return out, errors.Join(warnings...)
}
