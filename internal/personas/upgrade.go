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

	// MajorJump is true when the resolved transition crosses a major-version
	// boundary (semver.Major differs). It drives the unconditional "prompt tuned
	// for the prior major — verify" flag in the report, surfaced regardless of the
	// fixture outcome (AC 06-01 Edge Case 1).
	MajorJump bool
	// FixtureBlocked is true when a major jump's fixture re-check did not pass, so
	// the lock write was withheld; ToSlug still names the would-be slug.
	FixtureBlocked bool
	// FixtureReason is a human-readable explanation of a FixtureBlocked outcome.
	FixtureReason string
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
		return upgradeResolvedLock(client, name, personasDir, dest, localData, lockMeta.Model, binding, dryRun)
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
		// Reject a bare trailing '@' (empty channel) here so the typo fails closed
		// for ALL families — otherwise an alias family silently ignores it while a
		// scan family fails only later at normalizeChannel (asymmetric).
		if channel == "@" {
			return Binding{}, false, fmt.Errorf("binding %q has an empty channel", s)
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
//
// A lock advance writes ONLY the model-bumped YAML (writeLockYAML) — it never
// touches the co-located custom prompt or the personas .md endpoint, so advancing
// a model can never clobber a locally-authored prompt and never depends on a
// second network fetch (TD-003 resolution). This deliberately diverges from the
// 19.6 version path's writePersonaUnit, whose .md re-sync is correct for a
// full-unit version upgrade but wrong for a model-only lock advance.
func upgradeResolvedLock(client HTTPClient, name, personasDir, dest string, localData []byte, currentSlug string, b Binding, dryRun bool) (UpgradeResult, error) {
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

	// Major-bump re-validation gate (Story 06): when the resolved transition
	// crosses a major-version boundary, always surface the verify flag and gate
	// the lock write on the persona's committed fixture re-passing. A minor advance
	// (same major) falls straight through unchanged — the fixture never runs on the
	// common path (AC 06-01 Edge Case 3). A passing fixture proves only that the
	// template still renders, never that the prompt is well-tuned for the new
	// major, so the verify flag is unconditional on every major jump.
	if isMajorJump(versionFromSlug(currentSlug), versionFromSlug(newSlug)) {
		res.MajorJump = true
		outcome, ferr := newUpgradeFixtureRunner(personasDir).RunFixture(name)
		if ferr != nil {
			return UpgradeResult{}, fmt.Errorf("persona %q: major-version fixture re-check failed: %w", name, ferr)
		}
		if !fixturePassed(outcome) {
			// Withhold the write, but still report the would-be slug (res.ToSlug)
			// and the reason the lock did not advance.
			res.SlugChanged = false
			res.Upgraded = false
			res.FixtureBlocked = true
			res.FixtureReason = fixtureBlockReason(outcome)
			return res, nil
		}
	}

	// Compute and validate the new lock BEFORE the dry-run short-circuit, so a
	// dry-run surfaces exactly the error a real run would and never advertises a
	// would-be advance a real run cannot produce (report parity, AC 04-03). Only
	// the write itself is gated by dryRun — the shared-computation guarantee.
	newYAML, err := setModelField(localData, newSlug)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q: %w", name, err)
	}
	if err := registry.ValidateCommunityPersonaYAML(name, newYAML); err != nil {
		return UpgradeResult{}, fmt.Errorf("persona %q failed validation: %w", name, err)
	}
	if dryRun {
		return res, nil
	}
	if err := writeLockYAML(dest, name, newYAML); err != nil {
		return UpgradeResult{}, err
	}
	return res, nil
}

// writeLockYAML persists a resolved-lock advance by writing ONLY the model-bumped
// persona YAML — it leaves the co-located .md untouched (a lock advance changes
// the model, never the prompt) and never fetches the .md endpoint. It keeps the
// same symlink guards writePersonaUnit uses: refuseSymlinkedIntermediate for the
// name-contributed intermediate dirs plus writeFileAtomic's own leaf Lstat check.
func writeLockYAML(dest, name string, yamlData []byte) error {
	if err := refuseSymlinkedIntermediate(dest, name); err != nil {
		return err
	}
	return writeFileAtomic(dest, yamlData)
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
// yaml.Node round-trip — so the resolved slug replaces only the model value and
// every other persona field is left intact. The caller persists the result via
// writeLockYAML (YAML only), so the resolved slug really is the only change
// written to disk.
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

// newUpgradeFixtureRunner builds the FixtureRunner the major-bump gate uses to
// re-check a persona's committed fixture before advancing a lock across a major
// boundary. It is a package var so tests inject a deterministic stub without
// changing Upgrade's signature (mirrors the envCatalogURL override seam).
var newUpgradeFixtureRunner = func(personasDir string) FixtureRunner {
	return TemplateFixtureRunner{PersonasDir: func() (string, error) { return personasDir, nil }}
}

// fixturePassed reports whether a fixture outcome counts as a passing re-check for
// the major-bump gate: a present fixture with every case passing. An absent
// fixture is treated as non-passing — an untestable major jump must not advance
// the lock silently (AC 06-01 Edge Case 2).
func fixturePassed(o FixtureOutcome) bool {
	return o.HasFixture && o.Total > 0 && o.Passed == o.Total
}

// fixtureBlockReason renders the human-readable reason a major-jump lock write was
// withheld, distinguishing an absent fixture from one that ran but did not pass.
func fixtureBlockReason(o FixtureOutcome) string {
	if !o.HasFixture {
		return "no committed fixture to re-check"
	}
	return fmt.Sprintf("fixture re-check failed (%d/%d cases passed)", o.Passed, o.Total)
}

// normalizeSemver renders a bare or "v"-prefixed version token in the single
// "v"-prefixed form isNewer and the major-jump classifier both consume, so both
// checks validate and compare the exact same normalized string — no divergent
// parsing path (AC 06-01/06-02 Input Validation).
func normalizeSemver(v string) string {
	return "v" + strings.TrimPrefix(v, "v")
}

// isMajorJump reports whether remote crosses a major-version boundary relative to
// local. It reuses normalizeSemver — the same normalization isNewer applies — and
// fires only when BOTH sides are valid semver with differing majors; any
// non-comparable input (mixed or absent validity) is conservatively NOT a major
// jump, matching isNewer's mixed-validity "treat as up-to-date" posture so the
// gate degrades safely rather than misclassifying (AC 06-02 Edge Case 1).
func isMajorJump(local, remote string) bool {
	lv := normalizeSemver(local)
	rv := normalizeSemver(remote)
	if !semver.IsValid(lv) || !semver.IsValid(rv) {
		return false
	}
	return semver.Major(lv) != semver.Major(rv)
}

// isNewer reports whether remote is a newer version than local. Valid semver
// is compared structurally. When exactly one side is valid semver the versions
// are not comparable, so the local copy is treated as up-to-date to avoid
// silently overwriting a newer or customized local persona. Otherwise any
// difference is treated as an upgrade.
func isNewer(local, remote string) bool {
	lv := normalizeSemver(local)
	rv := normalizeSemver(remote)
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
