package skill

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkill_FileExistsAndNonEmpty(t *testing.T) {
	require.NotEmpty(t, SkillMD, "SKILL.md must be embedded and non-empty")
}

func TestSkill_Frontmatter(t *testing.T) {
	// YAML frontmatter delimited by the first two --- lines, with name + description.
	require.True(t, strings.HasPrefix(SkillMD, "---\n"), "SKILL.md must open with YAML frontmatter")
	end := strings.Index(SkillMD[4:], "\n---")
	require.GreaterOrEqual(t, end, 0, "frontmatter must be closed by ---")
	fm := SkillMD[4 : 4+end]
	assert.Regexp(t, regexp.MustCompile(`(?m)^name:\s*atcr\b`), fm)
	assert.Regexp(t, regexp.MustCompile(`(?m)^description:\s*\S`), fm)
}

func TestSkill_RequiredSections(t *testing.T) {
	for _, h := range []string{
		"## Overview",
		"## Input Format",
		"## Orchestration Steps",
		"## Host Review Instructions",
		"## Ambiguity Adjudication",
		"## Findings Format Reference",
	} {
		assert.Contains(t, SkillMD, h, "SKILL.md must contain section %q", h)
	}
}

func TestSkill_InputForms(t *testing.T) {
	// Range, branch, PR URL, and default-to-current-branch must all be documented.
	assert.Contains(t, SkillMD, "base..head", "git range form")
	assert.Contains(t, SkillMD, "pull/", "PR URL form")
	assert.Regexp(t, regexp.MustCompile(`(?i)branch name`), SkillMD)
	assert.Regexp(t, regexp.MustCompile(`(?i)current branch`), SkillMD, "default-to-current-branch behavior")
}

func TestSkill_OrchestrationSequence(t *testing.T) {
	// The canonical sequence range → review → status → host → reconcile → report
	// must appear in order.
	// Backtick-prefixed so the `.atcr/reviews/` path (which contains the substring
	// "atcr review") does not produce a false match.
	steps := []string{"`atcr range", "`atcr review", "`atcr status", "sources/host/findings.txt", "`atcr reconcile", "`atcr report"}
	last := -1
	for _, s := range steps {
		idx := strings.Index(SkillMD, s)
		require.GreaterOrEqual(t, idx, 0, "orchestration must reference %q", s)
		assert.Greater(t, idx, last, "%q must appear after the previous step", s)
		last = idx
	}
}

func TestSkill_HostFindingsFormat(t *testing.T) {
	assert.Contains(t, SkillMD, "# atcr-findings/v1", "version header")
	assert.Contains(t, SkillMD, "SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE|REVIEWER", "8-column v1 row")
	// The REVIEWER column must be set to host in the example row.
	assert.Regexp(t, regexp.MustCompile(`\|host\b`), SkillMD, "example host row ends with the host reviewer")
}

func TestSkill_SeverityEnum(t *testing.T) {
	for _, sev := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		assert.Contains(t, SkillMD, sev)
	}
}

func TestSkill_AdversarialClause(t *testing.T) {
	// Adversarial, no-praise personality must be explicit.
	assert.Regexp(t, regexp.MustCompile(`(?i)not praise|no praise|adversarial`), SkillMD)
	assert.Regexp(t, regexp.MustCompile(`(?i)problems the author would prefer`), SkillMD)
}

func TestSkill_AdjudicationDocumented(t *testing.T) {
	assert.Contains(t, SkillMD, "ambiguous.json")
	assert.Contains(t, SkillMD, "adjudication.json")
	for _, verb := range []string{"merge", "distinct", "skipped"} {
		assert.Contains(t, SkillMD, verb)
	}
}

// TestSkill_NoAbsoluteOrClaudePaths enforces AC 05-01 Edge Case 2: the body
// references only the atcr binary and review-directory-relative paths — no
// .claude-specific paths and no absolute filesystem paths.
func TestSkill_NoAbsoluteOrClaudePaths(t *testing.T) {
	assert.NotContains(t, SkillMD, ".claude", "no .claude-specific paths in the skill body")
	for _, abs := range []string{"/Users/", "/home/", "/opt/", "C:\\"} {
		assert.NotContains(t, SkillMD, abs, "no absolute filesystem path %q", abs)
	}
}
