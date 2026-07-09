package personas

import (
	"fmt"
	"os"
	"strings"
)

// InstalledLock is one installed community persona's resolved-slug lock (Model)
// and its logical binding string — the two inputs a drift check compares against
// the catalog. Binding is "" for a bindingless 19.6 persona (its pinned Model is
// still its lock).
type InstalledLock struct {
	Name    string
	Model   string
	Binding string
}

// Drift condition identifiers. These are also the stable `condition` values in
// the --json output, so Epic 19.8's mechanical agent can switch on them without
// parsing free text.
const (
	ConditionNewerMember = "newer-member"
	ConditionDeprecation = "deprecation"
	ConditionMissing     = "missing"
)

// DriftFinding is one (persona, condition) result of a drift check. It is the
// single shared structure both the human-readable and --json renderers derive
// from (AC 05-02): condition-inapplicable fields are omitted from JSON via
// omitempty rather than emitted as null.
type DriftFinding struct {
	Persona        string `json:"persona"`
	Condition      string `json:"condition"`
	CurrentSlug    string `json:"current_slug"`
	SuggestedSlug  string `json:"suggested_slug,omitempty"`
	Family         string `json:"family,omitempty"`
	Channel        string `json:"channel,omitempty"`
	ExpirationDate string `json:"expiration_date,omitempty"`
}

// LoadLock reads an installed community persona's resolved-slug lock (model) and
// its logical binding from disk, reusing the same path resolution and permissive
// metadata decode as the upgrade path — so `models check` sees exactly the lock a
// review would consume. A corrupt or unreadable persona surfaces the AC 05-01
// per-persona failure message.
func LoadLock(personasDir, name string) (InstalledLock, error) {
	dest, err := personaPath(personasDir, name)
	if err != nil {
		return InstalledLock{}, fmt.Errorf("failed to read resolved lock for persona %q: %w", name, err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return InstalledLock{}, fmt.Errorf("failed to read resolved lock for persona %q: %w", name, err)
	}
	meta, err := lockMetaOf(data)
	if err != nil {
		return InstalledLock{}, fmt.Errorf("failed to read resolved lock for persona %q: %w", name, err)
	}
	return InstalledLock{Name: name, Model: strings.TrimSpace(meta.Model), Binding: meta.Binding}, nil
}

// CheckDrift compares each installed lock against the catalog and returns the
// drift findings in a deterministic order: locks in the given order, and within a
// single persona newer-member before deprecation. A slug absent from the catalog
// is terminal for that persona (one `missing` finding; no version baseline to
// compare, and its expiration cannot be read). A lock with an empty Model is
// skipped (a model-agnostic persona carries nothing to check). The missing and
// deprecation lookups use a catalog index built once (O(1) per persona); the
// newer-member check scans the model list per persona (O(personas × models)),
// which is negligible at the realistic scale of tens of personas.
func CheckDrift(locks []InstalledLock, models []CatalogModel) []DriftFinding {
	bySlug := make(map[string]CatalogModel, len(models))
	for _, m := range models {
		if _, seen := bySlug[m.ID]; !seen {
			bySlug[m.ID] = m
		}
	}
	findings := make([]DriftFinding, 0)
	for _, lock := range locks {
		slug := strings.TrimSpace(lock.Model)
		if slug == "" {
			continue
		}
		entry, present := bySlug[slug]
		if !present {
			findings = append(findings, DriftFinding{
				Persona:     lock.Name,
				Condition:   ConditionMissing,
				CurrentSlug: slug,
			})
			continue
		}
		if f, ok := newerMemberFinding(lock, slug, models); ok {
			findings = append(findings, f)
		}
		if isDeprecated(entry) {
			findings = append(findings, DriftFinding{
				Persona:        lock.Name,
				Condition:      ConditionDeprecation,
				CurrentSlug:    slug,
				ExpirationDate: strings.TrimSpace(*entry.ExpirationDate),
			})
		}
	}
	return findings
}

// newerMemberFinding reports whether a newer family member is available for the
// persona's locked slug. When the persona declares a family/channel binding it
// reuses the resolver directly (the same family the upgrade path would advance
// within); a pin never floats, so it yields no finding. For a bindingless persona
// it derives the family prefix from the locked slug and finds the newest @stable
// member sharing it (AC 05-01 Scenario 2 fallback). Either way the suggestion must
// both differ from the lock AND be a version-advance (isNewer), so an alias slug
// with no comparable version never reports drift.
func newerMemberFinding(lock InstalledLock, slug string, models []CatalogModel) (DriftFinding, bool) {
	binding, present, err := parseBinding(lock.Binding)
	if err != nil {
		return DriftFinding{}, false // an invalid binding is not a drift condition
	}
	var suggested, family, channel string
	if present {
		if strings.TrimSpace(binding.Pin) != "" {
			return DriftFinding{}, false // explicit pin never floats
		}
		s, rerr := ResolveModel(binding, models)
		if rerr != nil {
			return DriftFinding{}, false // unresolvable binding is not a drift condition
		}
		suggested = s
		family = binding.Family
		ch, _ := normalizeChannel(binding.Channel)
		channel = strings.TrimPrefix(ch, "@")
	} else {
		prefix := deriveFamilyPrefix(slug)
		s, ok := newestStableInFamilyPrefix(prefix, models)
		if !ok {
			return DriftFinding{}, false
		}
		suggested = s
		family = prefix
		channel = "stable"
	}
	if suggested == "" || suggested == slug {
		return DriftFinding{}, false
	}
	if !isNewer(versionFromSlug(slug), versionFromSlug(suggested)) {
		return DriftFinding{}, false
	}
	return DriftFinding{
		Persona:       lock.Name,
		Condition:     ConditionNewerMember,
		CurrentSlug:   slug,
		SuggestedSlug: suggested,
		Family:        family,
		Channel:       channel,
	}, true
}

// deriveFamilyPrefix reduces a concrete slug to its vendor+family prefix by
// stripping any :variant suffix and a single trailing version-like hyphen segment,
// so "anthropic/claude-opus-4.8" → "anthropic/claude-opus" and "z-ai/glm-5.2" →
// "z-ai/glm". A slug with no trailing version segment is returned unchanged (its
// family scan then finds only itself — a conservative "no drift").
func deriveFamilyPrefix(slug string) string {
	s := slug
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	vendor := ""
	rest := s
	if i := strings.IndexByte(s, '/'); i >= 0 {
		vendor = s[:i+1]
		rest = s[i+1:]
	}
	segs := strings.Split(rest, "-")
	if len(segs) > 1 && versionSegRe.MatchString(segs[len(segs)-1]) {
		segs = segs[:len(segs)-1]
	}
	return vendor + strings.Join(segs, "-")
}

// newestStableInFamilyPrefix returns the ID of the newest @stable-eligible catalog
// member of the SAME family tier as familyPrefix, selected by the same
// created-timestamp total order the resolver uses. A candidate is same-tier only
// when its own derived family prefix equals familyPrefix — so "anthropic/claude-opus"
// matches "anthropic/claude-opus-5.0" but NOT a sibling tier like
// "openai/gpt-5.4-mini" (whose derived prefix retains the "-mini" tier token) and
// never a "~"-prefixed alias (different vendor token). A malformed/control-char
// candidate slug is skipped so a bad catalog entry is never suggested as a lock.
// ok=false when the prefix is empty or no eligible member exists.
func newestStableInFamilyPrefix(familyPrefix string, models []CatalogModel) (string, bool) {
	if strings.TrimSpace(familyPrefix) == "" {
		return "", false
	}
	var best *CatalogModel
	for i := range models {
		m := &models[i]
		if deriveFamilyPrefix(m.ID) != familyPrefix { // same tier only (no cross-tier bleed)
			continue
		}
		if m.Created <= 0 {
			continue
		}
		if !channelEligible(*m, "@stable") {
			continue
		}
		if validateResolvedSlug(m.ID) != nil { // never suggest a malformed/control-char slug
			continue
		}
		if best == nil || newerCandidate(*m, *best) {
			best = m
		}
	}
	if best == nil {
		return "", false
	}
	return best.ID, true
}
