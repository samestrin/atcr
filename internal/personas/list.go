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

// ScoreDetail is the explainability companion for one persona: how many cases
// its rate rests on, how many were excluded, and the reason labels behind both.
//
// IT IS A LOCAL STRUCT, NOT internal/scorecard's PersonaScoreDetail, and the
// duplication is deliberate. internal/boundaries_test.go allowlists this
// package's imports as {registry, payload, gitexec}; importing internal/scorecard
// to borrow one DTO would put the ROSTER package downstream of the review-outcome
// LEDGER, which is backwards and forecloses the reverse edge structurally. The
// sibling field is the precedent: Rate is a *float64 carrying a
// scorecard-computed number without this package knowing scorecard exists.
//
// The caller converts. cli/personas.go imports both and does it in one loop.
// Reasons' KEYS are still scorecard's closed vocabulary (scorecard.ScoreReasons);
// this type copies no label constant, so the vocabulary has exactly one home.
type ScoreDetail struct {
	Counted  int
	Excluded int
	Reasons  map[string]int
}

// ScoredPersona is one row of `personas list --scores`: a persona joined with
// its corroboration rate. Rate is nil when the persona has no scorecard data,
// which renders as "n/a" (distinct from a real 0.0 rate).
//
// Detail is nil on exactly the personas whose Rate is nil — scorecard keys its
// priors map and its explainability map identically and omits a lens from both
// or from neither, so the two are never half-present. Whether a membership floor
// contributes to that omission is the CALLER's choice of minRuns; `personas list
// --scores` passes 0, so on that path absence means "no usable history", never
// "under-sampled". Detail is ADDITIVE to Rate and is never consulted by
// sortScoredPersonas, which is why a thin sample is marked by the renderer
// rather than by the ordering.
type ScoredPersona struct {
	PersonaMeta
	Rate   *float64
	Detail *ScoreDetail
}

// ListWithScores returns the personas from List joined with corroboration rates
// from scores (keyed by lowercase persona name, as built by the caller from
// scorecard.Aggregate) and with the explainability detail from details (keyed
// the same way, as built by scorecard.ExplainTrustPriors). The result is sorted
// by rate descending, then n/a rows alphabetically after all numeric rows. A
// directory walk error is returned alongside the rows gathered so far,
// mirroring List.
//
// details is a SECOND parameter rather than a widening of scores (sprint 36.0
// D4): the rate path and the explainability path stay independently testable,
// and scorecard.TrustPriors keeps its map[string]float64 shape for
// reconcile/consensus.go. A nil details map is legal and yields nil Detail on
// every row.
func ListWithScores(personasDir string, scores map[string]float64, details map[string]ScoreDetail) ([]ScoredPersona, error) {
	metas, err := List(personasDir)
	return joinScores(metas, scores, details), err
}

// ListTiersWithScores returns the personas from ListTiers joined with
// corroboration rates from scores and explainability detail from details. It
// mirrors ListWithScores but sources the persona set from the three resolver
// tiers (project > community > built-in) so the --scores table agrees with the
// plain list on the Source column.
func ListTiersWithScores(projectDir, communityDir string, scores map[string]float64, details map[string]ScoreDetail) ([]ScoredPersona, error) {
	metas, err := ListTiers(projectDir, communityDir)
	return joinScores(metas, scores, details), err
}

// joinScores attaches corroboration rates and explainability detail to metas and
// sorts the result. Both maps are read with the same strings.ToLower(m.Name) key
// the rate lookup has always used, so a persona cannot be present in one and
// missed in the other for a casing reason.
func joinScores(metas []PersonaMeta, scores map[string]float64, details map[string]ScoreDetail) []ScoredPersona {
	scored := make([]ScoredPersona, 0, len(metas))
	for _, m := range metas {
		sp := ScoredPersona{PersonaMeta: m}
		key := strings.ToLower(m.Name)
		if rate, ok := scores[key]; ok && !math.IsNaN(rate) {
			r := rate
			sp.Rate = &r
		}
		// Comma-ok, never a bare lookup: a persona absent from the detail map has
		// no usable history (or, when the CALLER passed a non-zero minRuns, sits
		// below it — scorecard reports the two the same way, and `personas list
		// --scores` passes 0 so only the first case arises there). The zero-valued
		// struct a bare lookup returns would render as "0 counted" — "measured,
		// found nothing", the opposite of the truth. nil Detail is the "no data"
		// marker, matching nil Rate.
		if d, ok := details[key]; ok {
			detail := d
			sp.Detail = &detail
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

// listCommunity returns the community personas under personasDir.
//
// ONE RULE FOR WHAT A PERSONA FILE IS, shared with listProject: a <name>.yaml /
// <name>.yml, or a bare <name>.md. The .md admission is what makes the two
// on-disk tiers agree — listProject has always taken .md, and this walker used
// to skip it as if it were a .DS_Store, which made an md-only lens invisible to
// every view built on ListTiers.
//
// A <name>.md co-located with a <name>.yaml is the YAML persona's prompt body,
// not a second persona: it folds into that row rather than being emitted twice.
// _base.md is the shared fallback template at any depth and is never a persona,
// matching listProject. Symlinks are skipped (they may point outside the dir),
// and a name colliding with a built-in warns and is skipped whichever extension
// it arrived under.
func listCommunity(personasDir string) ([]PersonaMeta, error) {
	if _, err := os.Stat(personasDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read personas directory %s: %w", personasDir, err)
	}
	var out []PersonaMeta
	var warnings []error
	// yamlNames is every name the YAML pass claimed (lowercased, the same key
	// ListTiers dedupes on); mdCandidates holds the .md files seen, resolved
	// against it after the walk.
	yamlNames := map[string]bool{}
	var mdCandidates []string
	walkErr := filepath.WalkDir(personasDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil // skip directories and symlinks (symlinks may point outside the personas dir)
		}
		ext := strings.ToLower(filepath.Ext(path))
		// A bare <name>.md IS a persona here, exactly as it is in listProject.
		// This tier used to admit only .yaml/.yml and skip .md alongside the
		// .DS_Store files, so one file shape was a persona in the project dir and
		// noise in the community dir — which made every md-only lens invisible to
		// `personas list`, the surface whose whole question is which lenses to keep.
		//
		// A .md co-located with a <name>.yaml is that persona's PROMPT BODY, not a
		// second persona, so it must not produce a second row. The walk therefore
		// defers .md files to a second pass (below) where the full set of YAML
		// names is known: WalkDir is lexical, so `archer.md` is visited before
		// `archer.yaml` and a decision taken inline would be made blind.
		if ext == ".md" {
			if rel, relErr := filepath.Rel(personasDir, path); relErr == nil && filepath.Base(path) != "_base.md" {
				mdCandidates = append(mdCandidates, rel)
			}
			return nil
		}
		if ext != ".yaml" && ext != ".yml" {
			return nil // silently skip non-persona files (.DS_Store, .gitkeep, ...)
		}
		rel, err := filepath.Rel(personasDir, path)
		if err != nil {
			return nil
		}
		name := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		yamlNames[strings.ToLower(name)] = true
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
	// Second pass: the md-only personas. Appended after the YAML rows in lexical
	// walk order, so the result stays deterministic. A candidate whose name a
	// YAML file already claimed is that persona's prompt body and is dropped — the
	// YAML row keeps its version pin and language tokens, which an md file carries
	// no way to express. Version "-" marks the rest as unpinned, the same marker a
	// YAML file with no version field gets.
	//
	// The built-in collision check applies here too. Without it the new admission
	// would open a silent shadowing path that the YAML branch has always been
	// closed to.
	for _, rel := range mdCandidates {
		name := filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		if yamlNames[strings.ToLower(name)] {
			continue
		}
		if isBuiltin(name) {
			warnings = append(warnings, fmt.Errorf("skipping community file %q: name collides with built-in persona %q", rel, name))
			continue
		}
		out = append(out, PersonaMeta{Name: name, Version: "-", Source: "community"})
	}
	return out, errors.Join(warnings...)
}

// IsCommunityInstalled reports whether name is a community-repo INSTALL under
// personasDir — that is, backed by a <name>.yaml.
//
// listCommunity admits two file shapes, and only one of them carries a resolved
// lock: a YAML persona has a version pin and a manifest, while a bare <name>.md
// is a local prompt file an operator dropped in. Consumers that filter on
// `Source == "community"` and then reach for the YAML — `personas drift`
// (LoadLock per row) and `personas remove --all` (Remove per row) — must ask this
// first, or they report a missing-file error for every md-only lens.
//
// A traversal or otherwise invalid name is not installed rather than probed, so
// the answer never depends on a path outside personasDir.
func IsCommunityInstalled(personasDir, name string) bool {
	dest, err := personaPath(personasDir, name)
	if err != nil {
		return false
	}
	fi, err := os.Stat(dest)
	return err == nil && fi.Mode().IsRegular()
}
