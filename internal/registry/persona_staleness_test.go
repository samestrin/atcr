package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/personas"
)

// ruleLessPersona is a persona body written before Epic 35.16.9: it carries neither
// predicate-rule anchor, so personas.CarriesPredicateRule rejects it.
const ruleLessPersona = "# reviewer\n\n## Focus\n1. Correctness: logic errors\n"

func embeddedPersona(t *testing.T, name string) string {
	t.Helper()
	if name == "_base" {
		text, err := personas.Base()
		require.NoError(t, err)
		return text
	}
	text, err := personas.Get(name)
	require.NoError(t, err)
	return text
}

// A workspace initialized before Epic 35.16.9 has no stamp and copies without
// the rule. Both the built-in's own copy and the `_base.md` a non-built-in agent
// falls to are named, through the PredicateRuleGaps fallback.
func TestStalePersonas_NoStampRuleAbsentReportsStale(t *testing.T) {
	project := t.TempDir()
	writePersona(t, project, "bruce", ruleLessPersona)
	writePersona(t, project, "_base", ruleLessPersona)

	got := StalePersonas(map[string]string{
		"bruce":  "bruce",
		"helper": "helper", // no helper.md: resolves to the project _base.md
		"other":  "other",
	}, PersonaDirs{Project: project})

	require.Equal(t, []StalePersona{
		{File: "_base.md", Path: filepath.Join(project, "_base.md"), Agents: []string{"helper", "other"}},
		{File: "bruce.md", Path: filepath.Join(project, "bruce.md"), Agents: []string{"bruce"}},
	}, got)
}

// A workspace initialized after Epic 35.16.9 but before the stamp: no stamp,
// and copies that carry the rule — whether verbatim or hand-edited — are
// current, so a stamp-less workspace is not reported stale without cause.
func TestStalePersonas_NoStampRulePresentReportsCurrent(t *testing.T) {
	project := t.TempDir()
	writePersona(t, project, "bruce", embeddedPersona(t, "bruce"))
	writePersona(t, project, "greta", embeddedPersona(t, "greta")+"\n7. House rule: no panics\n")
	writePersona(t, project, "_base", embeddedPersona(t, "_base"))

	got := StalePersonas(map[string]string{
		"bruce": "bruce", "greta": "greta", "helper": "helper",
	}, PersonaDirs{Project: project})

	require.Empty(t, got)
}

// A stamped copy whose embedded content has since changed is stale; it is
// edited only when the file also drifted from what init wrote.
func TestStalePersonas_StampedEmbeddedChangedReportsStale(t *testing.T) {
	project := t.TempDir()
	// Both copies carry the rule, so the fallback would call them current: only
	// the stamp can see that the embedded content moved on.
	old := embeddedPersona(t, "bruce") + "\n<!-- an older release -->\n"
	writePersona(t, project, "bruce", old)
	oldGreta := embeddedPersona(t, "greta") + "\n<!-- an older release -->\n"
	writePersona(t, project, "greta", oldGreta+"\n7. House rule: no panics\n")

	stamp := NewPersonaStamp()
	stamp.Record("bruce.md", []byte(old))
	stamp.Record("greta.md", []byte(oldGreta))
	require.NoError(t, WritePersonaStamp(project, stamp))

	got := StalePersonas(map[string]string{"bruce": "bruce", "greta": "greta"}, PersonaDirs{Project: project})

	require.Equal(t, []StalePersona{
		{File: "bruce.md", Path: filepath.Join(project, "bruce.md"), Agents: []string{"bruce"}, Stamped: true},
		{File: "greta.md", Path: filepath.Join(project, "greta.md"), Agents: []string{"greta"}, Edited: true, Stamped: true},
	}, got)
}

// A stamp entry that matches the embedded content means init wrote what the
// binary ships, so any drift is the operator's hand edit — never stale, even
// when the edit removed the rule.
func TestStalePersonas_StampedCurrentHandEditIsNotStale(t *testing.T) {
	project := t.TempDir()
	writePersona(t, project, "bruce", ruleLessPersona)
	writePersona(t, project, "greta", embeddedPersona(t, "greta"))

	stamp := NewPersonaStamp()
	stamp.Record("bruce.md", []byte(embeddedPersona(t, "bruce")))
	stamp.Record("greta.md", []byte(embeddedPersona(t, "greta")))
	require.NoError(t, WritePersonaStamp(project, stamp))

	got := StalePersonas(map[string]string{"bruce": "bruce", "greta": "greta"}, PersonaDirs{Project: project})

	require.Empty(t, got)
}

// A file that matches the embedded content is current even when its stamp entry
// is behind (the operator refreshed it by hand).
func TestStalePersonas_FileMatchesEmbeddedIsCurrent(t *testing.T) {
	project := t.TempDir()
	writePersona(t, project, "bruce", embeddedPersona(t, "bruce"))

	stamp := NewPersonaStamp()
	stamp.Record("bruce.md", []byte(ruleLessPersona))
	require.NoError(t, WritePersonaStamp(project, stamp))

	require.Empty(t, StalePersonas(map[string]string{"bruce": "bruce"}, PersonaDirs{Project: project}))
}

// An explicit ref that resolves to a built-in's stale copy under
// `.atcr/personas/` is reported under that copy, with the referring agent among
// its Agents.
func TestStalePersonas_ExplicitRefToBuiltinCopyReported(t *testing.T) {
	project := t.TempDir()
	writePersona(t, project, "bruce", ruleLessPersona)

	got := StalePersonas(map[string]string{
		"bruce":     "bruce",
		"bruce-bot": "bruce", // explicit ref to a stale built-in copy
	}, PersonaDirs{Project: project})

	require.Equal(t, []StalePersona{
		{File: "bruce.md", Path: filepath.Join(project, "bruce.md"), Agents: []string{"bruce", "bruce-bot"}},
	}, got)
}

// A stamped built-in copy reached only through an explicit ref is judged by the
// same stamp comparison as one reached through the agent's own name.
func TestStalePersonas_ExplicitRefStampedCopyReported(t *testing.T) {
	project := t.TempDir()
	old := embeddedPersona(t, "greta") + "\n<!-- an older release -->\n"
	writePersona(t, project, "greta", old)
	stamp := NewPersonaStamp()
	stamp.Record("greta.md", []byte(old))
	require.NoError(t, WritePersonaStamp(project, stamp))

	got := StalePersonas(map[string]string{"reviewer": "greta"}, PersonaDirs{Project: project})

	require.Equal(t, []StalePersona{
		{File: "greta.md", Path: filepath.Join(project, "greta.md"), Agents: []string{"reviewer"}, Stamped: true},
	}, got)
}

// Community personas, project-only personas and refs that fail to resolve have
// no built-in counterpart and are never reported, however far behind the rule
// they are — whether reached through the agent's name or an explicit ref; nor is
// a built-in's copy in the Registry tier, which is not under `.atcr/personas/`.
func TestStalePersonas_CommunityAndNonBuiltinRefNeverReported(t *testing.T) {
	project := t.TempDir()
	registryDir := t.TempDir()
	writePersona(t, registryDir, "owasp", ruleLessPersona) // community persona
	writePersona(t, registryDir, "kai", ruleLessPersona)   // built-in name, Registry tier
	writePersona(t, project, "strict", ruleLessPersona)    // project-only persona
	writePersona(t, registryDir, "_base", ruleLessPersona) // Registry-tier base

	got := StalePersonas(map[string]string{
		"sec-agent":  "owasp",  // explicit ref to a community persona
		"kai":        "kai",    // resolves to the Registry tier
		"strict":     "strict", // project persona with no embedded counterpart
		"strict-bot": "strict", // explicit ref to a project-only persona
		"kai-bot":    "kai",    // explicit ref to a built-in's Registry-tier copy
		"helper":     "helper", // falls to the Registry _base.md
		"missing":    "nowhere",
	}, PersonaDirs{Project: project, Registry: registryDir})

	require.Empty(t, got)
}

// A workspace that never ran `atcr init` resolves every persona from the
// embedded set and reports nothing.
func TestStalePersonas_UninitializedWorkspaceReportsNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), ".atcr", "personas")
	roster := map[string]string{"bruce": "bruce", "helper": "helper"}

	require.Empty(t, StalePersonas(roster, PersonaDirs{Project: missing}))
	require.Empty(t, StalePersonas(roster, PersonaDirs{}))
}

// A damaged stamp reads as no stamp, so the fallback decides.
func TestStalePersonas_UnparseableStampFallsBack(t *testing.T) {
	project := t.TempDir()
	writePersona(t, project, "bruce", ruleLessPersona)
	require.NoError(t, os.WriteFile(PersonaStampPath(project), []byte("{not json"), 0o644))

	got := StalePersonas(map[string]string{"bruce": "bruce"}, PersonaDirs{Project: project})

	require.Equal(t, []StalePersona{
		{File: "bruce.md", Path: filepath.Join(project, "bruce.md"), Agents: []string{"bruce"}},
	}, got)
}
