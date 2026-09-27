package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/registry"
	"github.com/stretchr/testify/require"
)

// docs/registry.md's thinking, thinking_level, and thinking_style rows are the
// operator-facing statement of the per-agent thinking keys (Epic 35.16.11.2.2).
// Legal values are read from the registry accessors, not restated here, so
// renaming or adding one without touching the doc fails this test.
//
// Asserted on PHRASES inside single-line table rows, per
// response_format_doc_test.go, so no phrase can straddle a hard wrap.
func TestRegistryDoc_ThinkingRows(t *testing.T) {
	doc := readRepoFile(t, "../../docs/registry.md")
	shared := []struct{ token, why string }{
		{"never inherited through `fallback:`", "a fallback sends its OWN declaration, never the primary's, like response_format"},
		{"rejected on a community persona", "rejectMachineLocalFields bans the key; a persona author must not be surprised at install"},
	}

	rows := []struct {
		key    string
		values []string
		extra  []struct{ token, why string }
	}{
		{"`thinking`", registry.ThinkingValues(), []struct{ token, why string }{
			{"byte-identical", "an agent with no thinking keys sends the same request body as before the keys existed"},
			{"bare `true`/`false` is rejected", "the field is a string so a YAML bool is not aliased to on/off"},
		}},
		{"`thinking_level`", registry.ThinkingLevels(), []struct{ token, why string }{
			{"level alone implies `thinking: on`", "a level without thinking is not a missing-value error"},
			{"`thinking: off` with a level is rejected at load", "off plus a level is contradictory config"},
		}},
		{"`thinking_style`", registry.ThinkingStyles(), []struct{ token, why string }{
			{"there is no default style", "a thinking key without a style is a load error"},
			{"style alone is inert", "a style with no thinking or level sends nothing"},
		}},
	}
	for _, r := range rows {
		row := docRow(t, doc, r.key)
		var must []struct{ token, why string }
		for _, v := range r.values {
			must = append(must, struct{ token, why string }{"`" + v + "`", "the row must name every value validateAgent accepts, spelled as the registry constant"})
		}
		must = append(must, shared...)
		must = append(must, r.extra...)
		for _, m := range must {
			if !strings.Contains(row, m.token) {
				t.Errorf("docs/registry.md's %s row must state %q: %s\nrow was: %s", r.key, m.token, m.why, row)
			}
		}
	}
}

// The thinking row says a bare YAML bool is rejected and the level row says off
// with a level is rejected. Those are claims about validateAgent, so they are
// checked against a real load.
func TestRegistryDoc_ThinkingRejectsWhatTheDocExcludes(t *testing.T) {
	load := func(keys string) error {
		path := filepath.Join(t.TempDir(), "registry.yaml")
		body := "providers:\n  p:\n    api_key_env: KEY\nagents:\n  a:\n    provider: p\n    model: m\n" + keys
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := registry.LoadRegistry(path)
		return err
	}
	style := "    thinking_style: " + registry.ThinkingStyleQwen + "\n"
	require.NoError(t, load("    thinking: "+registry.ThinkingOff+"\n"+style), "the documented off value must load")
	for _, bad := range []string{"true", "false"} {
		require.Errorf(t, load("    thinking: "+bad+"\n"+style), "the doc says bare %s is rejected at load", bad)
	}
	require.Error(t, load("    thinking: "+registry.ThinkingOff+"\n    thinking_level: "+registry.ThinkingLevelLow+"\n"+style),
		"the doc says off with a level is rejected at load")
	require.Error(t, load("    thinking: "+registry.ThinkingOn+"\n"), "the doc says there is no default style")
}
