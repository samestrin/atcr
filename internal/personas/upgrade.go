package personas

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/samestrin/atcr/internal/registry"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

// envCatalogURL overrides CatalogBaseURL when set (e.g. an httptest server), so
// the resolver's catalog fetch inside Upgrade stays zero-live-network in CI. It
// mirrors envPersonasURL (client.go): the resolution path is only ever exercised
// via Upgrade, so this is the single injection seam CI needs.
const envCatalogURL = "ATCR_CATALOG_URL"

// catalogBaseURL returns the ATCR_CATALOG_URL override when set, else "" so the
// CatalogClient falls back to the hardcoded CatalogBaseURL constant.
func catalogBaseURL() string {
	return strings.TrimSpace(os.Getenv(envCatalogURL))
}

// UpgradeResult reports the outcome of an Upgrade for one persona. FromVersion/
// ToVersion track the 19.6 version-string path; FromSlug/ToSlug/Resolved/
// SlugChanged track the 19.7 resolved-lock path (populated only when the persona
// carries a family/channel binding).
type UpgradeResult struct {
	Name        string
	FromVersion string
	ToVersion   string
	Upgraded    bool // remote was newer (and written, unless dryRun)
	UpToDate    bool // remote not newer than local

	// Resolved is true when the persona declared a binding and the resolver ran.
	Resolved bool
	// FromSlug/ToSlug are the resolved-lock slugs before and after. On an
	// unchanged resolution both equal the current lock; "(none)" is the
	// placeholder for a persona with no prior lock (AC 04-01 Edge Case 2).
	FromSlug string
	ToSlug   string
	// SlugChanged is true when the resolved slug advanced the lock (written,
	// unless dryRun).
	SlugChanged bool
}

// Upgrade re-fetches name from baseURL, compares its version to the installed
// copy, and overwrites the local unit (YAML plus any co-located custom prompt)
// when the remote is newer. dryRun reports what would change without writing.
// The fetched content is validated before any write, so invalid remote content
// never overwrites a good local file.
func Upgrade(client HTTPClient, baseURL, personasDir, name string, dryRun bool) (UpgradeResult, error) {
	dest, err := personaPath(personasDir, name)
	if err != nil {
		return UpgradeResult{}, err
	}
	localData, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return UpgradeResult{}, fmt.Errorf("persona %q is not installed", name)
		}
		return UpgradeResult{}, fmt.Errorf("failed to read installed persona %q: %w", name, err)
	}

	// Epic 19.7: when the persona declares a family/channel binding, upgrade
	// re-resolves it against the live catalog and advances the resolved-slug lock
	// (Model). A persona with no binding — every 19.6 persona today — falls
	// through to the unchanged version-based path below: backward-compatible,
	// zero migration, and it never touches the catalog endpoint.
	lockMeta, err := lockMetaOf(localData)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("installed persona %q is unparseable; aborting upgrade: %w", name, err)
	}
	binding, present, err := parseBinding(lockMeta.Binding)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q has an invalid binding: %w", name, err)
	}
	if present {
		return upgradeResolvedLock(client, baseURL, name, dest, localData, lockMeta.Model, binding, dryRun)
	}

	remoteData, err := FetchPersonaYAML(client, baseURL, name)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := registry.ValidateCommunityPersonaYAML(name, remoteData); err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q failed validation: %w", name, err)
	}

	localVersion, err := versionOf(localData)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("installed persona %q is unparseable; aborting upgrade: %w", name, err)
	}
	remoteVersion, err := versionOf(remoteData)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("remote persona %q is unparseable; aborting upgrade: %w", name, err)
	}
	res := UpgradeResult{Name: name, FromVersion: localVersion, ToVersion: remoteVersion}
	if !isNewer(res.FromVersion, res.ToVersion) {
		res.UpToDate = true
		return res, nil
	}
	res.Upgraded = true
	if dryRun {
		return res, nil
	}
	if err := writePersonaUnit(client, baseURL, name, dest, remoteData); err != nil {
		return UpgradeResult{}, err
	}
	return res, nil
}

// versionOf extracts the version metadata field, or "-" when absent. A corrupt
// YAML payload surfaces as an error so callers do not silently treat it as a
// missing version and overwrite a customized local persona.
func versionOf(data []byte) (string, error) {
	var fm personaFileMeta
	if err := yaml.Unmarshal(data, &fm); err != nil {
		return "-", fmt.Errorf("failed to parse persona metadata: %w", err)
	}
	if strings.TrimSpace(fm.Version) == "" {
		return "-", nil
	}
	return fm.Version, nil
}

// personaLockMeta captures the two fields the 19.7 resolution path reads from an
// installed persona: its resolved-slug lock (Model) and its logical binding.
// Decoded permissively (unknown fields ignored) so it reads from a full persona
// document without a strict schema.
type personaLockMeta struct {
	Model   string `yaml:"model"`
	Binding string `yaml:"binding"`
}

func lockMetaOf(data []byte) (personaLockMeta, error) {
	var m personaLockMeta
	if err := yaml.Unmarshal(data, &m); err != nil {
		return personaLockMeta{}, fmt.Errorf("failed to parse persona metadata: %w", err)
	}
	return m, nil
}

// parseBinding turns a persona's on-disk `binding` string into a resolver Binding,
// fully fail-closed (Phase 4 clarification, extending the catalog.go:131 contract):
//   - empty/whitespace                → (zero, present=false): no binding, skip resolution;
//   - "pin:<slug>"                    → explicit Pin (verbatim, never floats);
//   - "<family>@<channel>"            → Family + Channel (split on the LAST '@');
//   - bare, EXACT match in aliasTable ∪ vendorPrefixTable → Family, default @stable;
//   - anything else                   → error (an alias-shaped typo like
//     "anthropic/claude-opu" is NOT silently accepted as a pin — the pin: sigil
//     is required, so the mistake fails here, not downstream at the API).
func parseBinding(s string) (Binding, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Binding{}, false, nil
	}
	if rest, ok := strings.CutPrefix(s, "pin:"); ok {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return Binding{}, false, fmt.Errorf("binding %q has an empty pin", s)
		}
		return Binding{Pin: rest}, true, nil
	}
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		family := strings.TrimSpace(s[:i])
		channel := strings.TrimSpace(s[i:]) // retains the leading '@'
		if family == "" {
			return Binding{}, false, fmt.Errorf("binding %q has an empty family", s)
		}
		return Binding{Family: family, Channel: channel}, true, nil
	}
	if _, ok := aliasTable[s]; ok {
		return Binding{Family: s}, true, nil
	}
	if _, ok := vendorPrefixTable[s]; ok {
		return Binding{Family: s}, true, nil
	}
	return Binding{}, false, fmt.Errorf("unrecognized binding %q: expected pin:<slug>, <family>@<channel>, or a known bare family", s)
}

// versionSegRe matches a hyphen-delimited slug segment that is a version token:
// an optional leading "v" then a dotted numeric run (e.g. "4.8", "5", "v4.1").
var versionSegRe = regexp.MustCompile(`^v?\d+(\.\d+)*$`)

// versionFromSlug extracts the version token from a resolved model slug so the
// existing isNewer/semver machinery can classify a lock transition (story note,
// upgrade.go reuse of isNewer). It strips the vendor prefix and any :variant
// suffix, then returns the RIGHTMOST hyphen segment that looks like a version
// (e.g. "anthropic/claude-opus-4.8" → "4.8", "deepseek/deepseek-v4-pro" → "v4").
// A slug with no numeric version segment (e.g. a "-latest" alias) yields "", which
// isNewer treats as non-comparable — conservatively NOT an advance.
func versionFromSlug(slug string) string {
	if i := strings.LastIndexByte(slug, '/'); i >= 0 {
		slug = slug[i+1:]
	}
	if i := strings.IndexByte(slug, ':'); i >= 0 {
		slug = slug[:i]
	}
	segs := strings.Split(slug, "-")
	for i := len(segs) - 1; i >= 0; i-- {
		if versionSegRe.MatchString(segs[i]) {
			return segs[i]
		}
	}
	return ""
}

// upgradeResolvedLock is the 19.7 resolution path: fetch the catalog once, resolve
// the binding to a concrete slug, and advance the lock (Model) only when the
// resolved slug both differs from the current lock AND is a version-advance. A
// failed fetch or unresolvable binding aborts cleanly, leaving the lock unchanged
// (no partial advance, no silent stale fallback — AC 04-01 Error Scenario 3).
func upgradeResolvedLock(client HTTPClient, baseURL, name, dest string, localData []byte, currentSlug string, b Binding, dryRun bool) (UpgradeResult, error) {
	cat := &CatalogClient{HTTPClient: client, BaseURL: catalogBaseURL()}
	models, err := cat.FetchModels()
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("failed to resolve model binding for persona %q: %w", name, err)
	}
	newSlug, err := ResolveModel(b, models)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("failed to resolve model binding for persona %q: %w", name, err)
	}

	res := UpgradeResult{Name: name, Resolved: true, FromSlug: slugOrPlaceholder(currentSlug), ToSlug: newSlug}
	// A persona with no prior lock (empty Model) always adopts the resolved slug to
	// establish the lock — the version-advance gate only applies once a lock exists
	// (AC 04-01 Edge Case 2). With a prior lock, advance only when the resolved slug
	// both differs and is a version-advance; otherwise retain it and report unchanged.
	noPriorLock := strings.TrimSpace(currentSlug) == ""
	if !noPriorLock && (newSlug == currentSlug || !isNewer(versionFromSlug(currentSlug), versionFromSlug(newSlug))) {
		res.UpToDate = true
		res.ToSlug = slugOrPlaceholder(currentSlug)
		return res, nil
	}
	res.SlugChanged = true
	res.Upgraded = true
	if dryRun {
		return res, nil
	}

	newYAML, err := setModelField(localData, newSlug)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q: %w", name, err)
	}
	if err := registry.ValidateCommunityPersonaYAML(name, newYAML); err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q failed validation: %w", name, err)
	}
	if err := writePersonaUnit(client, baseURL, name, dest, newYAML); err != nil {
		return UpgradeResult{}, err
	}
	return res, nil
}

// slugOrPlaceholder renders an empty lock as "(none)" so the before→after report
// never prints an empty value (AC 04-01 Edge Case 2).
func slugOrPlaceholder(slug string) string {
	if strings.TrimSpace(slug) == "" {
		return "(none)"
	}
	return slug
}

// setModelField returns data with only its top-level `model` value replaced by
// newModel, preserving every other field, comment, and scalar style via a
// yaml.Node round-trip — so a lock advance is a minimal, faithful edit (the
// resolved slug is the only change written to disk).
func setModelField(data []byte, newModel string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse persona for lock update: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("persona is not a YAML mapping")
	}
	m := root.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "model" {
			v := m.Content[i+1]
			v.Value = newModel
			v.Tag = "!!str"
			var buf bytes.Buffer
			enc := yaml.NewEncoder(&buf)
			enc.SetIndent(2)
			if err := enc.Encode(&root); err != nil {
				return nil, fmt.Errorf("failed to re-encode persona: %w", err)
			}
			if err := enc.Close(); err != nil {
				return nil, fmt.Errorf("failed to finalize persona encode: %w", err)
			}
			return buf.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("persona has no model field to advance")
}

// isNewer reports whether remote is a newer version than local. Valid semver
// is compared structurally. When exactly one side is valid semver the versions
// are not comparable, so the local copy is treated as up-to-date to avoid
// silently overwriting a newer or customized local persona. Otherwise any
// difference is treated as an upgrade.
func isNewer(local, remote string) bool {
	lv := "v" + strings.TrimPrefix(local, "v")
	rv := "v" + strings.TrimPrefix(remote, "v")
	lValid := semver.IsValid(lv)
	rValid := semver.IsValid(rv)
	switch {
	case lValid && rValid:
		return semver.Compare(rv, lv) > 0
	case lValid || rValid:
		return false
	default:
		return local != remote
	}
}
