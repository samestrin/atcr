package personas

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	builtins "github.com/samestrin/atcr/personas"
)

func ratePtr(f float64) *float64 { return &f }

// --- AC 01-05: ListTiers three-tier source labeling -------------------------

// TestListTiers_ThreeSourcesInPrecedence covers AC 01-05 Scenario 2: `personas
// list` distinguishes project > community > built-in, with a project override
// shadowing the built-in of the same name and the community pin version shown.
func TestListTiers_ThreeSourcesInPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	// A hand-authored project override for a built-in name.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# project bruce\n"), 0o644))
	// A community persona (namespaced, disjoint from built-in names) with a pin.
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))

	metas, err := ListTiers(projectDir, communityDir)
	require.NoError(t, err)

	byName := map[string]PersonaMeta{}
	for _, m := range metas {
		byName[m.Name] = m
	}
	require.Contains(t, byName, "bruce")
	assert.Equal(t, "project", byName["bruce"].Source, "project override shadows the built-in")
	require.Contains(t, byName, "security/owasp")
	assert.Equal(t, "community", byName["security/owasp"].Source)
	assert.Equal(t, "1.0.0", byName["security/owasp"].Version, "community pin version shown")
	require.Contains(t, byName, "greta")
	assert.Equal(t, "built-in", byName["greta"].Source, "un-overridden persona stays built-in")
}

func TestListTiers_CaseInsensitiveBuiltinDedup(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	// Namespaced community persona that does not collide.
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "Owasp.yaml"), []byte(validPersonaYAML), 0o644))
	// Mixed-case name that matches a built-in when compared case-insensitively.
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "Bruce.yaml"), []byte(validPersonaYAML), 0o644))

	metas, err := ListTiers(projectDir, communityDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with built-in")

	byName := map[string]PersonaMeta{}
	for _, m := range metas {
		byName[strings.ToLower(m.Name)] = m
	}
	require.Contains(t, byName, "security/owasp")
	require.Contains(t, byName, "bruce")
	assert.Equal(t, "built-in", byName["bruce"].Source, "mixed-case community file must be treated as built-in collision")
	assert.NotContains(t, byName, "Bruce", "Bruce must not appear separately from bruce")
}

func TestListTiersWithScores_IncludesProjectOverride(t *testing.T) {
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	// Project override for a built-in name.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# project bruce\n"), 0o644))
	// Community pin.
	require.NoError(t, os.MkdirAll(filepath.Join(communityDir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(communityDir, "security", "owasp.yaml"), []byte(validPersonaYAML), 0o644))

	scores := map[string]float64{"bruce": 0.9, "security/owasp": 0.6}
	scored, err := ListTiersWithScores(projectDir, communityDir, scores, nil)
	require.NoError(t, err)

	bruce := scoredByName(scored, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, "project", bruce.Source, "project override must appear in scored list")
	require.NotNil(t, bruce.Rate)
	assert.InDelta(t, 0.9, *bruce.Rate, 1e-9)

	owasp := scoredByName(scored, "security/owasp")
	require.NotNil(t, owasp)
	assert.Equal(t, "community", owasp.Source)
	require.NotNil(t, owasp.Rate)
	assert.InDelta(t, 0.6, *owasp.Rate, 1e-9)
}

// --- FormatRate -------------------------------------------------------------

func TestFormatRate(t *testing.T) {
	assert.Equal(t, "n/a", FormatRate(nil))
	assert.Equal(t, "0.0%", FormatRate(ratePtr(0.0)))
	assert.Equal(t, "50.0%", FormatRate(ratePtr(0.5)))
	assert.Equal(t, "72.5%", FormatRate(ratePtr(0.725)))
	assert.Equal(t, "100.0%", FormatRate(ratePtr(1.0)))
	// Out-of-range rates clamp to [0,100]% rather than render a nonsense value.
	assert.Equal(t, "0.0%", FormatRate(ratePtr(-0.5)))
	assert.Equal(t, "100.0%", FormatRate(ratePtr(1.5)))
}

// --- ListWithScores join ----------------------------------------------------

func scoredByName(scored []ScoredPersona, name string) *ScoredPersona {
	for i := range scored {
		if scored[i].Name == name {
			return &scored[i]
		}
	}
	return nil
}

func TestListWithScores_HasRateAndNa(t *testing.T) {
	scores := map[string]float64{"sasha": 0.72} // penny absent
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)

	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	require.NotNil(t, sasha.Rate)
	assert.InDelta(t, 0.72, *sasha.Rate, 1e-9)

	penny := scoredByName(scored, "penny")
	require.NotNil(t, penny)
	assert.Nil(t, penny.Rate, "persona with no scorecard data has nil rate (n/a)")
}

func TestListWithScores_CaseInsensitiveJoin(t *testing.T) {
	// Scores map is keyed by lowercase reviewer name; a community persona whose
	// file name differs in case still joins.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "security"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "security", "Owasp.yaml"), []byte(validPersonaYAML), 0o644))

	scores := map[string]float64{"security/owasp": 0.6} // lowercase key
	scored, err := ListWithScores(dir, scores, nil)
	require.NoError(t, err)

	owasp := scoredByName(scored, "security/Owasp")
	require.NotNil(t, owasp)
	require.NotNil(t, owasp.Rate, "case-insensitive lookup must join")
	assert.InDelta(t, 0.6, *owasp.Rate, 1e-9)
}

func TestListWithScores_ZeroRateIsNotNa(t *testing.T) {
	scores := map[string]float64{"sasha": 0.0}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)
	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	require.NotNil(t, sasha.Rate, "rate 0.0 is data, not n/a")
	assert.Equal(t, "0.0%", FormatRate(sasha.Rate))
}

func TestListWithScores_NaNRateTreatedAsNa(t *testing.T) {
	scores := map[string]float64{"sasha": math.NaN()}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)
	sasha := scoredByName(scored, "sasha")
	require.NotNil(t, sasha)
	assert.Nil(t, sasha.Rate, "NaN rate must be treated as n/a")
	assert.Equal(t, "n/a", FormatRate(sasha.Rate))
}

// --- sortScoredPersonas -----------------------------------------------------

func names(ps []ScoredPersona) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func TestSortScoredPersonas_NumericDescThenNaAlpha(t *testing.T) {
	ps := []ScoredPersona{
		{PersonaMeta: PersonaMeta{Name: "tracer"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "sentinel"}, Rate: ratePtr(0.72)},
		{PersonaMeta: PersonaMeta{Name: "guardian"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "idiomatic"}, Rate: ratePtr(0.50)},
	}
	sortScoredPersonas(ps)
	assert.Equal(t, []string{"sentinel", "idiomatic", "guardian", "tracer"}, names(ps))
}

func TestSortScoredPersonas_TieBreakAlphabetical(t *testing.T) {
	ps := []ScoredPersona{
		{PersonaMeta: PersonaMeta{Name: "sentinel"}, Rate: ratePtr(0.60)},
		{PersonaMeta: PersonaMeta{Name: "idiomatic"}, Rate: ratePtr(0.60)},
	}
	sortScoredPersonas(ps)
	assert.Equal(t, []string{"idiomatic", "sentinel"}, names(ps))
}

func TestSortScoredPersonas_AllNaAlphabetical(t *testing.T) {
	ps := []ScoredPersona{
		{PersonaMeta: PersonaMeta{Name: "tracer"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "guardian"}, Rate: nil},
		{PersonaMeta: PersonaMeta{Name: "sentinel"}, Rate: nil},
	}
	sortScoredPersonas(ps)
	assert.Equal(t, []string{"guardian", "sentinel", "tracer"}, names(ps))
}

func TestListWithScores_SortedOutput(t *testing.T) {
	// End-to-end: ListWithScores applies the sort so the first numeric row leads.
	scores := map[string]float64{"sasha": 0.9, "ingrid": 0.4}
	scored, err := ListWithScores(filepath.Join(t.TempDir(), "absent"), scores, nil)
	require.NoError(t, err)
	require.NotEmpty(t, scored)
	assert.Equal(t, "sasha", scored[0].Name, "highest numeric rate sorts first")
}

// --- AC 06-05: the explainability detail joins additively -------------------

func TestJoinScores_AttachesDetailAlongsideTheExistingRate(t *testing.T) {
	// AC 06-05 Scenario 1. The detail map is a SECOND input (D4), keyed by the
	// same strings.ToLower(m.Name) the rate lookup has always used, and it never
	// replaces or perturbs Rate.
	metas := []PersonaMeta{{Name: "dax"}, {Name: "bruce"}}
	scores := map[string]float64{"dax": 0.8, "bruce": 0.4}
	details := map[string]ScoreDetail{
		"dax": {Counted: 20, Excluded: 5, Reasons: map[string]int{
			reasonOutcomeIneligibleForTest: 5,
		}},
		"bruce": {Counted: 40, Excluded: 0},
	}

	scored := joinScores(metas, scores, details)

	dax := scoredByName(scored, "dax")
	require.NotNil(t, dax)
	require.NotNil(t, dax.Rate)
	assert.InDelta(t, 0.8, *dax.Rate, 1e-9, "the rate path is untouched by the detail path")
	require.NotNil(t, dax.Detail)
	assert.Equal(t, 20, dax.Detail.Counted)
	assert.Equal(t, 5, dax.Detail.Excluded)
	assert.Equal(t, 5, dax.Detail.Reasons[reasonOutcomeIneligibleForTest])

	bruce := scoredByName(scored, "bruce")
	require.NotNil(t, bruce)
	require.NotNil(t, bruce.Detail)
	assert.Equal(t, 40, bruce.Detail.Counted)
}

func TestJoinScores_MixedCasePersonaFindsItsDetail(t *testing.T) {
	// The casing convention is shared by both lookups, so a persona cannot be
	// present in one map and missed in the other for a casing reason.
	metas := []PersonaMeta{{Name: "SASHA"}}
	scored := joinScores(metas,
		map[string]float64{"sasha": 0.5},
		map[string]ScoreDetail{"sasha": {Counted: 7}})

	require.Len(t, scored, 1)
	require.NotNil(t, scored[0].Detail)
	assert.Equal(t, 7, scored[0].Detail.Counted)
}

func TestJoinScores_AbsentFromDetailMapIsNilNotAFabricatedZero(t *testing.T) {
	// AC 06-05 Edge Cases 1 and 2. A persona with no history, and a below-floor
	// persona scorecard omitted, must both read as "no data" — never as an
	// evaluated-but-empty history, which would tell a maintainer the lens was
	// measured and found wanting when it was never measured at all.
	metas := []PersonaMeta{{Name: "ghost"}}
	scored := joinScores(metas, map[string]float64{}, map[string]ScoreDetail{})

	require.Len(t, scored, 1)
	assert.Nil(t, scored[0].Rate)
	assert.Nil(t, scored[0].Detail,
		"absent from the detail map must be nil Detail, never a zero-valued struct")
}

func TestJoinScores_NilDetailMapIsLegalAndYieldsNilDetail(t *testing.T) {
	// Every pre-Phase-5 caller passed no detail at all; a nil map must behave as
	// "no explainability available" rather than panicking.
	scored := joinScores([]PersonaMeta{{Name: "dax"}}, map[string]float64{"dax": 0.3}, nil)
	require.Len(t, scored, 1)
	require.NotNil(t, scored[0].Rate)
	assert.Nil(t, scored[0].Detail)
}

func TestSortScoredPersonas_NeverConsultsTheDetailFields(t *testing.T) {
	// AC 06-05 Scenario 2. Identical rates, wildly different detail — the
	// comparator must still fall through to the alphabetical tie-break, because
	// sortScoredPersonas' contract keys on Rate alone (D4/D6).
	metas := []PersonaMeta{{Name: "Zeta"}, {Name: "Alpha"}}
	scores := map[string]float64{"zeta": 0.5, "alpha": 0.5}
	details := map[string]ScoreDetail{
		"zeta":  {Counted: 999, Excluded: 0},
		"alpha": {Counted: 1, Excluded: 500},
	}

	scored := joinScores(metas, scores, details)
	require.Len(t, scored, 2)
	assert.Equal(t, "Alpha", scored[0].Name,
		"equal rates tie-break alphabetically; the new fields must not reorder them")
	assert.Equal(t, "Zeta", scored[1].Name)
}

// reasonOutcomeIneligibleForTest is scorecard.ReasonOutcomeIneligible's literal
// value. It is spelled out rather than imported because internal/personas must
// not import internal/scorecard (see ScoreDetail), and a test import would be
// just as much a boundary violation as a production one — internalImports in
// internal/boundaries_test.go reads every .go file in the package.
//
// The duplication is safe because this package never INTERPRETS the label: it
// carries Reasons through as opaque keys. The value is asserted against
// scorecard's own constant by cli/personas_test.go's
// TestPersonasScoreDetailLabels_MatchScorecardsVocabulary, which legally imports
// both packages — that test is this literal's authority, named here so a reader
// of internal/personas is not left holding a bare string with no provenance.
const reasonOutcomeIneligibleForTest = "outcome-ineligible"

// --- listProject error paths -------------------------------------------------

// TestListProject_MissingDirIsNoRowsNoError pins the documented contract that an
// absent project personas directory is a normal empty result, not a failure.
func TestListProject_MissingDirIsNoRowsNoError(t *testing.T) {
	metas, err := listProject(filepath.Join(t.TempDir(), "never-created"))
	require.NoError(t, err)
	assert.Empty(t, metas)
}

// TestListProject_UnstatableDirIsAnError separates "not there" from "there but
// unreadable": a projectDir whose parent is a regular file stats as ENOTDIR, and
// that must surface rather than silently read as an empty persona set.
func TestListProject_UnstatableDirIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))

	metas, err := listProject(filepath.Join(file, "personas"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read project personas directory")
	assert.Nil(t, metas)
}

// TestListProject_WalkErrorPropagates covers the walk callback's error branch: a
// subdirectory the process cannot open must fail the listing loudly instead of
// returning a partial roster that looks like the whole thing.
func TestListProject_WalkErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "bruce.md"), []byte("# bruce\n"), 0o644))
	locked := filepath.Join(projectDir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o755))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := listProject(projectDir)
	require.Error(t, err)
}

// TestListProject_SkipsBaseTemplateAndSymlinks pins the two exclusions the walk
// makes: _base.md is a shared template at any depth, and a symlink may point
// outside projectDir so it is never followed.
func TestListProject_SkipsBaseTemplateAndSymlinks(t *testing.T) {
	projectDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	require.NoError(t, os.WriteFile(outside, []byte("# outside\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "_base.md"), []byte("# base\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "notes.txt"), []byte("ignored\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "team"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "team", "_base.md"), []byte("# nested base\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "team", "vera.md"), []byte("# vera\n"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(projectDir, "linked.md")))

	metas, err := listProject(projectDir)
	require.NoError(t, err)

	var names []string
	for _, m := range metas {
		names = append(names, m.Name)
		assert.Equal(t, "project", m.Source)
	}
	assert.ElementsMatch(t, []string{"team/vera"}, names)
}

// The two on-disk tiers must agree on what a persona file IS.
//
// listProject admits <name>.md and labels it Source "project"; listCommunity
// admitted only .yaml/.yml and silently skipped .md as if it were a .DS_Store.
// The same file shape was therefore a persona in one directory and noise in the
// other — and on the live store that hid five real lenses (archer, brad, pace,
// ronin, vera are md-only in ~/.config/atcr/personas). A lens nothing lists is a
// lens nobody can audit, drop or repoint.
//
// The rule, applied in both walkers: a bare <name>.md IS a persona. A <name>.md
// co-located with <name>.yaml is the YAML persona's prompt body, not a second
// persona, so it folds into the YAML row rather than being emitted twice.
func TestListCommunity_AdmitsBareMarkdownPersonas(t *testing.T) {
	dir := t.TempDir()
	// paired: the .md is the yaml persona's prompt body, not its own row
	require.NoError(t, os.WriteFile(filepath.Join(dir, "paired.yaml"), []byte("version: 2.1.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "paired.md"), []byte("# paired\n"), 0o600))
	// md-only: a persona in its own right
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lonely.md"), []byte("# lonely\n"), 0o600))
	// shared base template, at any depth — never a persona, same as listProject
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_base.md"), []byte("# base\n"), 0o600))
	// not a persona file at all
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o600))

	got, err := listCommunity(dir)
	require.NoError(t, err)

	byName := map[string]PersonaMeta{}
	for _, m := range got {
		_, dup := byName[m.Name]
		assert.False(t, dup, "%q emitted twice; a co-located .md must fold into the YAML row", m.Name)
		byName[m.Name] = m
	}

	require.Contains(t, byName, "lonely", "a bare <name>.md is a community persona, not noise")
	assert.Equal(t, "-", byName["lonely"].Version, "an md-only persona carries no version pin")
	assert.Equal(t, "community", byName["lonely"].Source)

	require.Contains(t, byName, "paired")
	assert.Equal(t, "2.1.0", byName["paired"].Version,
		"the paired row keeps the YAML's version — the .md folded in, it did not replace it")

	assert.NotContains(t, byName, "_base", "the shared base template is never a persona, in either tier")
	assert.Len(t, got, 2, "exactly two personas: paired and lonely")
}

// An md-only community file whose name collides with a built-in must warn and be
// skipped, exactly as a colliding .yaml already does — otherwise the new
// admission opens a silent shadowing path the YAML one is closed to.
func TestListCommunity_BareMarkdownCollidingWithBuiltinIsSkipped(t *testing.T) {
	dir := t.TempDir()
	name := builtins.Names()[0]
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte("# shadow\n"), 0o600))

	got, err := listCommunity(dir)
	require.Error(t, err, "a built-in collision must be reported, not swallowed")
	assert.Empty(t, got, "the colliding file is skipped rather than shadowing the built-in")
}

// An md-only community persona is NOT a community-repo install: it has no
// <name>.yaml, so it carries no resolved lock, no version pin and no manifest.
//
// Admitting bare .md into listCommunity made that distinction load-bearing for
// the first time. Two consumers filter on `Source == "community"` and then
// assume a YAML behind it — `personas drift` calls LoadLock per row, and
// `personas remove --all` calls Remove per row — and both fail on a name whose
// yaml does not exist. IsCommunityInstalled is the one predicate they share, so
// the assumption is stated in a single place instead of re-derived twice.
func TestIsCommunityInstalled_DistinguishesYAMLBackedFromBareMarkdown(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pinned.yaml"), []byte("version: 1.0.0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pinned.md"), []byte("# pinned\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lonely.md"), []byte("# lonely\n"), 0o600))

	assert.True(t, IsCommunityInstalled(dir, "pinned"),
		"a YAML-backed persona is a community install and has a lock to read")
	assert.False(t, IsCommunityInstalled(dir, "lonely"),
		"a bare .md is a local prompt file, not a community install — it has no lock to drift or remove")
	assert.False(t, IsCommunityInstalled(dir, "absent"),
		"a name with no file at all is not installed")
	assert.False(t, IsCommunityInstalled(dir, "../escape"),
		"a traversal name is refused rather than probed outside the personas directory")
}

func TestJoinScores_RegistryOnlyLensIsNotDroppedSilently(t *testing.T) {
	// joinScores used to iterate the ROSTER alone, so a reviewer with scorecard
	// history but no persona file vanished from `personas list --scores` with no
	// notice. Against the live store that hid five of thirteen measured lenses
	// (archer, vera, brad, pace, ronin) — 38% of the panel — from the surface
	// sprint 36.0 makes the audit mechanism for lens authority. A measured lens
	// the explainability table cannot show is the one outcome it cannot afford.
	metas := []PersonaMeta{{Name: "dax", Version: "built-in", Source: "built-in"}}
	scores := map[string]float64{"dax": 0.8, "archer": 0.6}
	details := map[string]ScoreDetail{
		"dax":    {Counted: 40},
		"archer": {Counted: 221, Excluded: 3},
	}

	scored := joinScores(metas, scores, details)

	archer := scoredByName(scored, "archer")
	require.NotNil(t, archer, "a lens with history but no roster row must still be rendered")
	assert.Equal(t, "registry", archer.Source,
		"a registry-only lens is labeled by where it came from, not by a persona file it has not got")
	require.NotNil(t, archer.Rate)
	assert.InDelta(t, 0.6, *archer.Rate, 1e-9)
	require.NotNil(t, archer.Detail)
	assert.Equal(t, 221, archer.Detail.Counted)
	assert.Equal(t, 3, archer.Detail.Excluded)

	// The roster row is untouched by the tail.
	dax := scoredByName(scored, "dax")
	require.NotNil(t, dax)
	assert.Equal(t, "built-in", dax.Source)
	assert.Len(t, scored, 2, "exactly one row per lens — the tail must not duplicate a roster row")
}

func TestJoinScores_DetailOnlyLensStillSurfaces(t *testing.T) {
	// The tail unions BOTH maps. A lens scorecard could explain but not rate
	// would otherwise be dropped by a rates-only tail, re-opening the same hole
	// one map narrower.
	scored := joinScores(nil, nil, map[string]ScoreDetail{"vera": {Counted: 221}})

	require.Len(t, scored, 1)
	assert.Equal(t, "vera", scored[0].Name)
	assert.Equal(t, "registry", scored[0].Source)
	assert.Nil(t, scored[0].Rate, "no rate means n/a, not a fabricated zero")
	require.NotNil(t, scored[0].Detail)
	assert.Equal(t, 221, scored[0].Detail.Counted)
}

func TestJoinScores_RegistryTailIsCaseInsensitiveAgainstTheRoster(t *testing.T) {
	// The roster is matched by strings.ToLower(m.Name), so the tail must consume
	// the same key or a mixed-case roster entry would be emitted twice — once as
	// itself and once as a phantom "registry" lens.
	scored := joinScores([]PersonaMeta{{Name: "SASHA", Source: "community"}},
		map[string]float64{"sasha": 0.5}, map[string]ScoreDetail{"sasha": {Counted: 7}})

	require.Len(t, scored, 1, "a mixed-case roster persona must not also appear as a registry-only row")
	assert.Equal(t, "community", scored[0].Source)
}

func TestJoinScores_RegistryTailIsDeterministic(t *testing.T) {
	// Map iteration order is randomized, so the tail must be sorted like every
	// other row or the table would reorder between two runs with no data change.
	for i := 0; i < 20; i++ {
		scored := joinScores(nil,
			map[string]float64{"vera": 0.5, "archer": 0.5, "ronin": 0.5},
			nil)
		require.Len(t, scored, 3)
		assert.Equal(t, []string{"archer", "ronin", "vera"},
			[]string{scored[0].Name, scored[1].Name, scored[2].Name},
			"equal rates tie-break alphabetically, for the tail exactly as for the roster")
	}
}

func TestJoinScores_BlankKeyIsNotRenderedAsANamelessLens(t *testing.T) {
	// scorecard's normalizeReviewerName trims and lowercases but does not drop
	// the empty result, so a record with a whitespace-only Reviewer keys these
	// maps "". The roster join hid that by construction; the registry tail must
	// not surface it as a persona with no name.
	scored := joinScores(nil,
		map[string]float64{"": 0.9, "archer": 0.6},
		map[string]ScoreDetail{"": {Counted: 5}})

	require.Len(t, scored, 1, "the blank key contributes no row")
	assert.Equal(t, "archer", scored[0].Name)
}

func TestListTiersWithScores_FreshInstallStillShowsARegistryOnlyLens(t *testing.T) {
	// The path `personas list --scores` actually takes. On a fresh install the
	// community dir is empty, so a registry lens that ships no persona file —
	// archer and vera are the pair epic 35.16's acceptance criteria are written
	// about — has scorecard history and no roster row. Before the tail it was
	// looked up, found, and discarded with no row and no notice.
	projectDir := t.TempDir()
	communityDir := t.TempDir()

	scored, err := ListTiersWithScores(projectDir, communityDir,
		map[string]float64{"archer": 0.6},
		map[string]ScoreDetail{"archer": {Counted: 221, Excluded: 3}})
	require.NoError(t, err)

	archer := scoredByName(scored, "archer")
	require.NotNil(t, archer, "a registry-only lens must reach the rendered table")
	assert.Equal(t, "registry", archer.Source)
	assert.Equal(t, "-", archer.Version, "no persona file means no version, the community marker")
	require.NotNil(t, archer.Rate)
	assert.InDelta(t, 0.6, *archer.Rate, 1e-9)
	require.NotNil(t, archer.Detail)
	assert.Equal(t, 221, archer.Detail.Counted)

	// The built-in roster is unaffected — the tail adds, it never replaces.
	assert.NotNil(t, scoredByName(scored, "bruce"))
}
