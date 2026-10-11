package jsonrepair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// corpusFixture is one entry of testdata/corpus.json. See testdata/README.md.
type corpusFixture struct {
	Name     string `json:"name"`
	Lane     string `json:"lane"`
	Class    string `json:"class"`
	Position string `json:"position"`
	CutOff   bool   `json:"cut_off"`
	Note     string `json:"note"`
}

// loadCorpus reads the manifest and returns each fixture with its input text
// and its expected decode.
func loadCorpus(t *testing.T) []corpusFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Provenance string          `json:"provenance"`
		Fixtures   []corpusFixture `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("corpus.json: %v", err)
	}
	if !strings.Contains(m.Provenance, "synthetic") {
		t.Errorf("corpus.json provenance must state the fixtures are synthetic, got %q", m.Provenance)
	}
	return m.Fixtures
}

func readFixture(t *testing.T, f corpusFixture) (in string, want any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", f.Lane, f.Name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.ReadFile(filepath.Join("testdata", f.Lane, f.Name+".want.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(w, &want); err != nil {
		t.Fatalf("%s/%s.want.json is not valid JSON: %v", f.Lane, f.Name, err)
	}
	return string(b), want
}

// TestCorpus_ManifestMatchesFiles fails on a listed fixture with no files, a
// fixture file nobody lists, or a note-less entry.
func TestCorpus_ManifestMatchesFiles(t *testing.T) {
	listed := map[string]bool{}
	for _, f := range loadCorpus(t) {
		if f.Lane != "findings" && f.Lane != "verdict" {
			t.Errorf("%s: unknown lane %q", f.Name, f.Lane)
		}
		if f.Class == "" || f.Note == "" {
			t.Errorf("%s/%s: every fixture is noted with its defect class", f.Lane, f.Name)
		}
		for _, suffix := range []string{".txt", ".want.json"} {
			p := filepath.Join(f.Lane, f.Name+suffix)
			if listed[p] {
				t.Errorf("%s listed twice", p)
			}
			listed[p] = true
		}
	}
	var onDisk []string
	for _, lane := range []string{"findings", "verdict"} {
		entries, err := os.ReadDir(filepath.Join("testdata", lane))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			onDisk = append(onDisk, filepath.Join(lane, e.Name()))
		}
	}
	for _, p := range onDisk {
		if !listed[p] {
			t.Errorf("testdata/%s is not listed in corpus.json", p)
		}
		delete(listed, p)
	}
	for p := range listed {
		t.Errorf("corpus.json lists testdata/%s, which does not exist", p)
	}
}

// TestCorpus_ExpectedDecodes checks every fixture's expected value has the
// shape its parser returns, and that only the valid controls already decode.
func TestCorpus_ExpectedDecodes(t *testing.T) {
	for _, f := range loadCorpus(t) {
		t.Run(f.Lane+"/"+f.Name, func(t *testing.T) {
			in, want := readFixture(t, f)
			switch f.Lane {
			case "findings":
				checkFindingsWant(t, want)
				checkStrictness(t, f, in, want)
			case "verdict":
				checkVerdictWant(t, f, want)
				// The verdict parser reads the first balanced object in prose;
				// first '{' to last '}' is that object in every fixture here.
				cand := ""
				if i, j := strings.IndexByte(in, '{'), strings.LastIndexByte(in, '}'); i >= 0 && j > i {
					cand = in[i : j+1]
				}
				checkStrictness(t, f, cand, want)
			}
		})
	}
}

func checkFindingsWant(t *testing.T, want any) {
	t.Helper()
	arr, ok := want.([]any)
	if !ok {
		t.Fatalf("findings want must be an array, got %T", want)
	}
	keys := []string{"category", "est_minutes", "evidence", "file_line", "fix", "problem", "severity"}
	for i, e := range arr {
		obj, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("want[%d] is %T, not an object", i, e)
		}
		got := make([]string, 0, len(obj))
		for k := range obj {
			got = append(got, k)
		}
		sort.Strings(got)
		if !slices.Equal(got, keys) {
			t.Errorf("want[%d] keys = %v, want the 7 model keys %v", i, got, keys)
		}
	}
}

func checkVerdictWant(t *testing.T, f corpusFixture, want any) {
	t.Helper()
	if want == nil {
		if !f.CutOff {
			t.Errorf("only a cut_off fixture may expect no verdict")
		}
		return
	}
	obj, ok := want.(map[string]any)
	if !ok {
		t.Fatalf("verdict want must be an object or null, got %T", want)
	}
	switch obj["verdict"] {
	case "confirmed", "refuted", "unverifiable":
	default:
		t.Errorf("verdict %v is not in the enum", obj["verdict"])
	}
	if _, ok := obj["reasoning"].(string); !ok || len(obj) != 2 {
		t.Errorf("verdict want must hold exactly verdict and reasoning, got %v", obj)
	}
}

// checkStrictness: a valid control decodes strictly to its expected value, so
// repair has nothing to change; every other fixture fails strict decode, so it
// really exercises repair.
func checkStrictness(t *testing.T, f corpusFixture, text string, want any) {
	t.Helper()
	valid := json.Valid([]byte(text))
	if f.Class != "valid" {
		if valid {
			t.Errorf("class %q fixture decodes strictly; it would not exercise repair", f.Class)
		}
		return
	}
	if !valid {
		t.Fatalf("valid control does not decode strictly")
	}
	var got any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("valid control decodes to %v, want %v", got, want)
	}
}

// TestCorpus_Coverage pins the acceptance shape of the corpus: every in-scope
// defect class per lane, damage inside one element between intact elements,
// and cut-off replies whose final element stays dropped.
func TestCorpus_Coverage(t *testing.T) {
	elementClasses := []string{"trailing_comma", "single_quotes", "line_comment", "block_comment", "unquoted_keys", "mismatched_closer", "extra_closer"}
	required := map[string][]string{
		"findings": append([]string{"valid", "missing_closer", "mixed", "cut_off"}, elementClasses...),
		"verdict":  {"valid", "trailing_comma", "single_quotes", "line_comment", "block_comment", "unquoted_keys", "cut_off"},
	}
	have := map[string]map[string]bool{"findings": {}, "verdict": {}}
	betweenIntact := map[string]bool{}
	cutOffs := 0
	for _, f := range loadCorpus(t) {
		have[f.Lane][f.Class] = true
		in, want := readFixture(t, f)
		if f.Lane != "findings" {
			continue
		}
		arr, _ := want.([]any)
		if f.Position == "between_intact" {
			if len(arr) != 3 {
				t.Errorf("%s: damage between intact elements must keep all 3 findings, want has %d", f.Name, len(arr))
			}
			betweenIntact[f.Class] = true
		}
		if f.CutOff {
			cutOffs++
			// One more finding was started than is expected back: the cut-off
			// final element stays dropped.
			if started := strings.Count(in, `"severity"`); started != len(arr)+1 {
				t.Errorf("%s: %d findings started, %d expected; the cut-off element must be the only one dropped", f.Name, started, len(arr))
			}
			if s := strings.TrimSpace(in); strings.HasSuffix(s, "}") || strings.HasSuffix(s, "]") {
				t.Errorf("%s: a cut-off reply must not end on a closer", f.Name)
			}
		}
	}
	for lane, classes := range required {
		for _, c := range classes {
			if !have[lane][c] {
				t.Errorf("%s lane has no %q fixture", lane, c)
			}
		}
	}
	for _, c := range elementClasses {
		if !betweenIntact[c] {
			t.Errorf("findings lane has no %q fixture with the damage between intact elements", c)
		}
	}
	if cutOffs == 0 {
		t.Error("findings lane has no cut-off reply")
	}
}

// TestCorpus_NeutralContent keeps the corpus synthetic: no fixture mentions
// this project, and the valid controls carry the comment and quote markers a
// string-unaware repair would wrongly edit.
func TestCorpus_NeutralContent(t *testing.T) {
	tricky := map[string]bool{}
	for _, f := range loadCorpus(t) {
		in, _ := readFixture(t, f)
		w, err := os.ReadFile(filepath.Join("testdata", f.Lane, f.Name+".want.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(in+string(w)), "atcr") {
			t.Errorf("%s/%s mentions atcr; fixtures use neutral made-up content", f.Lane, f.Name)
		}
		if f.Class == "valid" && strings.Contains(in, "//") && strings.Contains(in, "/*") && strings.Contains(in, "'") {
			tricky[f.Lane] = true
		}
	}
	for _, lane := range []string{"findings", "verdict"} {
		if !tricky[lane] {
			t.Errorf("%s lane has no valid control with //, /* and ' inside strings", lane)
		}
	}
}
