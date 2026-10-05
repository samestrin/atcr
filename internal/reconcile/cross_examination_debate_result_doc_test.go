package reconcile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The atcr_debate MCP result must keep publishing `withheld` separately from
// `overflow`: the two carry opposite remedies, so a conflated count misleads a client
// (TD internal/mcp/handlers.go:871). BIDIRECTIONAL: the doc needle pins the published
// field list, the code needle pins the struct tag and the handler's assignment.
func TestCrossExaminationDoc_DebateResultSplitsWithheldFromOverflow(t *testing.T) {
	doc := readRepoFile(t, "../../docs/cross-examination.md")
	tools := readRepoFile(t, "../../internal/mcp/tools.go")
	handlers := readRepoFile(t, "../../internal/mcp/handlers.go")

	assert.Contains(t, doc, "`withheld`",
		"the MCP result field list must publish the withheld count")
	assert.Contains(t, tools, `Withheld   int         `+"`"+`json:"withheld"`+"`",
		"DebateResult must emit the withheld key the doc names")
	assert.Contains(t, handlers, "Withheld:   res.Withheld,",
		"the handler must populate it from debate.Result.Withheld")
	if strings.Contains(tools, `json:"overflow_withheld"`) {
		t.Errorf("the keys must stay separate fields, not one conflated value")
	}
}
