package stream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/metrics"
)

// corpusDir is the shared JSON repair fixture corpus (epic 35.16.11.2.2.10 T2).
var corpusDir = filepath.Join("..", "jsonrepair", "testdata")

// repairedFindings reads atcr_json_repaired_total{lane="findings"}.
func repairedFindings() int64 {
	return metrics.Counter(metrics.Key("atcr_json_repaired_total", "lane", "findings")).Value()
}

// TestJSONRepairedFindingsKey_MatchesMetricsKey pins the spelled-out counter
// key in v2.go to the one metrics.Key builds.
func TestJSONRepairedFindingsKey_MatchesMetricsKey(t *testing.T) {
	assert.Equal(t, metrics.Key("atcr_json_repaired_total", "lane", "findings"), jsonRepairedFindingsKey)
}

// knownRepairGaps maps a findings fixture jsonrepair.Repair declines today to
// how many leading findings element recovery still keeps. A closer that
// mismatches inside a middle element is read as the array's closer, so the
// elements after it trail a finished value (epic 35.16.11.2.2.10 T5 records it).
var knownRepairGaps = map[string]int{
	"mismatched_closer_in_element": 1,
}

// TestParseModelOutput_RepairCorpus runs every findings-lane fixture through a
// fenced reply and checks the decode against its .want.json: damage inside an
// element between intact ones keeps all of them, a lone damaged object yields
// its finding, and a cut-off reply still ends at its last complete element.
// The counter moves once for a repaired value and never for a valid or cut-off
// one.
func TestParseModelOutput_RepairCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(corpusDir, "corpus.json"))
	require.NoError(t, err)
	var m struct {
		Fixtures []struct {
			Name   string `json:"name"`
			Lane   string `json:"lane"`
			Class  string `json:"class"`
			CutOff bool   `json:"cut_off"`
		} `json:"fixtures"`
	}
	require.NoError(t, json.Unmarshal(raw, &m))

	seen := 0
	for _, f := range m.Fixtures {
		if f.Lane != "findings" {
			continue
		}
		seen++
		t.Run(f.Name, func(t *testing.T) {
			in, err := os.ReadFile(filepath.Join(corpusDir, "findings", f.Name+".txt"))
			require.NoError(t, err)
			w, err := os.ReadFile(filepath.Join(corpusDir, "findings", f.Name+".want.json"))
			require.NoError(t, err)
			var wantObjs []modelFinding
			require.NoError(t, json.Unmarshal(w, &wantObjs))
			var want []Finding
			for _, o := range wantObjs {
				file, line := splitFileLine(o.FileLine)
				want = append(want, Finding{Severity: NormalizeSeverity(o.Severity), File: file, Line: line,
					Problem: o.Problem, Fix: o.Fix, Category: o.Category, EstMinutes: int(o.EstMinutes), Evidence: o.Evidence})
			}

			reply := "Here is my review.\n\n```json\n" + string(in)
			if !f.CutOff {
				reply += "\n```\nDone.\n"
			}
			metrics.DefaultRegistry.Reset()
			got := ParseModelOutput([]byte(reply))
			if gap, ok := knownRepairGaps[f.Name]; ok {
				// Repair declines this fixture today; element recovery still
				// keeps the findings before the damage. T5 measures the gap.
				assert.Equal(t, want[:gap], got)
				assert.Zero(t, repairedFindings())
				return
			}
			assert.Equal(t, want, got)

			// The counter moves only when the repaired value is what was
			// accepted: never for a valid or cut-off value, nor for one element
			// recovery alone already reads in full.
			strict, _ := decodeJSONValue(string(in))
			wantCount := int64(0)
			if f.Class != "valid" && !f.CutOff && !reflect.DeepEqual(strict, want) {
				wantCount = 1
			}
			assert.Equal(t, wantCount, repairedFindings(), "atcr_json_repaired_total{lane=\"findings\"}")
		})
	}
	require.NotZero(t, seen, "no findings-lane fixtures in the corpus")
}

// TestParseModelOutput_RepairCountsOncePerValue checks the counter moves once
// per repaired value, not once per element, and stays put for a strict value.
func TestParseModelOutput_RepairCountsOncePerValue(t *testing.T) {
	damaged := `{"severity":"LOW","file_line":"x.go:1",}`
	metrics.DefaultRegistry.Reset()
	got := ParseModelOutput([]byte(jsonBlock("[" + damaged + ",\n" + damaged + "]")))
	assert.Len(t, got, 2)
	assert.Equal(t, int64(1), repairedFindings())

	metrics.DefaultRegistry.Reset()
	assert.Equal(t, []Finding{findA, findB}, ParseModelOutput([]byte(jsonBlock("["+objA+",\n"+objB+"]"))))
	assert.Zero(t, repairedFindings())
}

// TestParseModelOutput_BareValueStaysStrict checks a damaged value outside a
// fence is not repaired: it keeps only what element recovery keeps.
func TestParseModelOutput_BareValueStaysStrict(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	bare := "Findings:\n[" + objA + ",\n" + `{"severity":"LOW","file_line":"x.go:1",}` + ",\n" + objB + "]\n"
	assert.Equal(t, []Finding{findA}, ParseModelOutput([]byte(bare)))
	assert.Zero(t, repairedFindings())

	metrics.DefaultRegistry.Reset()
	assert.Empty(t, ParseModelOutput([]byte(`{"severity":"LOW","file_line":"x.go:1",}`+"\n")))
	assert.Zero(t, repairedFindings())
}

// TestParseSource_StaysStrict checks an on-disk v2 envelope with a damaged
// finding is refused, not repaired.
func TestParseSource_StaysStrict(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	env := `{"axi_format":"json","axi_notice":"","data":{"findings":[` +
		`{"severity":"HIGH","file_line":"a.go:1","problem":"pa","fix":"fa","category":"correctness","est_minutes":5,"evidence":"ev","reviewer":"rev",}]}}`
	_, err := ParseSource([]byte(VersionV2 + "\n" + env + "\n"))
	assert.Error(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, findingsFileV2), []byte(VersionV2+"\n"+env+"\n"), 0o644))
	_, err = ReadPoolFindings(dir)
	assert.Error(t, err)
	assert.Zero(t, repairedFindings())
}
