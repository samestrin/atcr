package reconcile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The ID is a content hash over (file, line, problem), not a unique key: the
// format doc must say two records identical in those three share an id.
func TestFindingsFormatDocSaysIDIsNotUnique(t *testing.T) {
	doc := readRepoFile(t, "../../docs/findings-format.md")
	i := strings.Index(doc, "**What the ID identifies.**")
	if !assert.GreaterOrEqual(t, i, 0) {
		return
	}
	para := doc[i:]
	if j := strings.Index(para, "\n\n"); j >= 0 {
		para = para[:j]
	}
	assert.Contains(t, para, "not a unique key")
	assert.Contains(t, para, "share an id")
}

// Findings rebuilt after reconcile leave CanonicalCategory empty on purpose;
// each rebuild site must say so.
func TestEmitRebuildSitesDocumentEmptyCanonicalCategory(t *testing.T) {
	src := readRepoFile(t, "emit.go")
	assert.GreaterOrEqual(t, strings.Count(src, "CanonicalCategory is intentionally left empty"), 2)
}
