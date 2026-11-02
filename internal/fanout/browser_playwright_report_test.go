package fanout

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestParsePlaywrightReportFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/playwright_report.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParsePlaywrightReport(data)
	if err != nil {
		t.Fatalf("ParsePlaywrightReport: %v", err)
	}
	errs := 0
	for _, c := range res.Console {
		if c.Level == "error" {
			errs++
		}
	}
	if errs != 2 || len(res.Console) != 3 {
		t.Errorf("console = %+v, want 2 errors of 3 entries", res.Console)
	}
	if len(res.NetworkFailures) != 1 {
		t.Fatalf("network failures = %+v, want 1", res.NetworkFailures)
	}
	if nf := res.NetworkFailures[0]; nf.URL != "http://127.0.0.1:3000/api/items" || nf.Status != 500 || nf.Method != "GET" {
		t.Errorf("network failure = %+v", nf)
	}
	if len(res.Screenshots) != 1 || res.Screenshots[0].Path != "/work/test-results/home-failed-1.png" {
		t.Errorf("screenshots = %+v", res.Screenshots)
	}
	if len(res.FailedTests) != 1 {
		t.Fatalf("failed tests = %+v, want 1", res.FailedTests)
	}
	if ft := res.FailedTests[0]; ft.Title != "home page > renders without errors" || ft.File != "preview.spec.ts" || ft.Status != "failed" || ft.Error == "" {
		t.Errorf("failed test = %+v", ft)
	}
}

func TestParsePlaywrightReportRetryPassIsNotFailure(t *testing.T) {
	data := []byte(`{"suites":[{"title":"a.spec.ts","file":"a.spec.ts","specs":[{"title":"t","tests":[{"results":[{"status":"failed"},{"status":"passed"}]}]}]}]}`)
	res, err := ParsePlaywrightReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FailedTests) != 0 {
		t.Errorf("failed tests = %+v, want none", res.FailedTests)
	}
}

func TestParsePlaywrightReportMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":           ``,
		"not json":        `not json`,
		"truncated":       `{"suites":[{"title"`,
		"wrong shape":     `{"hello":"world"}`,
		"suites not list": `{"suites":"x"}`,
		"bad network":     `{"suites":[{"specs":[{"title":"t","tests":[{"results":[{"status":"failed","attachments":[{"name":"network","body":"bm90IGpzb24="}]}]}]}]}]}`,
		"bad console":     `{"suites":[{"specs":[{"title":"t","tests":[{"results":[{"status":"failed","attachments":[{"name":"console","body":"bm90IGpzb24="}]}]}]}]}]}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := ParsePlaywrightReport([]byte(in))
			if err == nil {
				t.Fatalf("want error, got result %+v", res)
			}
		})
	}
}

func TestParsePlaywrightReportEmptySuitesIsValid(t *testing.T) {
	// Valid only with no top-level errors; with them, see
	// TestParsePlaywrightReportTopLevelErrors.
	for _, in := range []string{`{"suites":[]}`, `{"suites":[],"errors":[]}`} {
		if _, err := ParsePlaywrightReport([]byte(in)); err != nil {
			t.Fatalf("empty report %s: %v", in, err)
		}
	}
}

// TestParsePlaywrightReportTopLevelErrors covers Playwright's report-level
// "errors": a run that fails before any test loads (a config or spec that
// does not compile, a global setup that throws) writes no suites and the
// cause there. An empty result would read as a clean run, so it is an error
// that carries the cause.
func TestParsePlaywrightReportTopLevelErrors(t *testing.T) {
	data := []byte(`{"suites":[],"errors":[{"message":"Error: Cannot find module './helpers'"},{"message":"SyntaxError: Unexpected token"}]}`)
	res, err := ParsePlaywrightReport(data)
	if err == nil {
		t.Fatalf("want an error for a report with only top-level errors, got %+v", res)
	}
	for _, want := range []string{"Cannot find module './helpers'", "SyntaxError: Unexpected token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to carry %q", err, want)
		}
	}

	t.Run("suites present keep their result", func(t *testing.T) {
		data := []byte(`{"errors":[{"message":"global teardown failed"}],"suites":[{"title":"a.spec.ts","file":"a.spec.ts","specs":[{"title":"t","tests":[{"results":[{"status":"failed","errors":[{"message":"expect failed"}]}]}]}]}]}`)
		res, err := ParsePlaywrightReport(data)
		if err != nil {
			t.Fatalf("ParsePlaywrightReport: %v", err)
		}
		if len(res.FailedTests) != 1 || res.FailedTests[0].Error != "expect failed" {
			t.Fatalf("failed tests = %+v, want the test's own error", res.FailedTests)
		}
		if got := fmt.Sprintf("%+v", res); strings.Contains(got, "global teardown failed") {
			t.Fatalf("result = %s, want the top-level error kept out of it", got)
		}
	})

	t.Run("joined messages are capped", func(t *testing.T) {
		// A position-revealing fixture, so a cut one byte off shows in the text.
		text := func(n int) string { return strings.Repeat("0123456789", n/10+1)[:n] }
		const prefix = "fanout: playwright: no tests ran, Playwright reported %d error(s): "
		long := text(4 * playwrightMaxErrorText)
		joined := long + "; " + long
		cases := []struct {
			name string
			msgs []string
			want string
		}{
			{"under the cap", []string{text(playwrightMaxErrorText - 1)},
				fmt.Sprintf(prefix, 1) + text(playwrightMaxErrorText-1)},
			{"at the cap", []string{text(playwrightMaxErrorText)},
				fmt.Sprintf(prefix, 1) + text(playwrightMaxErrorText)},
			{"one byte over the cap", []string{text(playwrightMaxErrorText + 1)},
				fmt.Sprintf(prefix, 1) + text(playwrightMaxErrorText) + " [...truncated 1 bytes]"},
			{"two messages far over the cap", []string{long, long},
				fmt.Sprintf(prefix, 2) + joined[:playwrightMaxErrorText] + fmt.Sprintf(" [...truncated %d bytes]", len(joined)-playwrightMaxErrorText)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				errs := make([]string, len(tc.msgs))
				for i, m := range tc.msgs {
					errs[i] = `{"message":"` + m + `"}`
				}
				data := []byte(`{"suites":[],"errors":[` + strings.Join(errs, ",") + `]}`)
				_, err := ParsePlaywrightReport(data)
				if err == nil {
					t.Fatal("want an error for a report with only top-level errors")
				}
				if got := err.Error(); got != tc.want {
					t.Fatalf("err (%d bytes) =\n%s\nwant (%d bytes)\n%s", len(got), got, len(tc.want), tc.want)
				}
			})
		}
	})
}

func TestParsePlaywrightReportInputShapes(t *testing.T) {
	// bodies: "[{\"level\":\"error\",\"text\":\"boom\"}]"
	const levelConsole = "W3sibGV2ZWwiOiJlcnJvciIsInRleHQiOiJib29tIn1d"
	data := []byte(`{"suites":[{"title":"s","file":"suite.spec.ts","specs":[
	 {"title":"nofile","tests":[{"results":[{"status":"timedOut"}]}]},
	 {"title":"noresults","file":"x.spec.ts","tests":[{"results":[]}]},
	 {"title":"att","file":"y.spec.ts","tests":[{"results":[{"status":"passed","attachments":[
	  {"name":"console"},{"name":"network"},
	  {"name":"console","body":"` + levelConsole + `"},
	  {"name":"screenshot"}]}]}]}]}]}`)
	res, err := ParsePlaywrightReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FailedTests) != 1 || res.FailedTests[0].File != "suite.spec.ts" || res.FailedTests[0].Status != "timedOut" {
		t.Errorf("failed tests = %+v", res.FailedTests)
	}
	if len(res.Console) != 1 || res.Console[0].Level != "error" || res.Console[0].Text != "boom" {
		t.Errorf("console = %+v", res.Console)
	}
	if len(res.NetworkFailures) != 0 || len(res.Screenshots) != 0 {
		t.Errorf("want no network failures or screenshots, got %+v %+v", res.NetworkFailures, res.Screenshots)
	}
}
