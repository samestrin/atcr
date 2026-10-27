package fanout

import (
	"os"
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
	if _, err := ParsePlaywrightReport([]byte(`{"suites":[]}`)); err != nil {
		t.Fatalf("empty report: %v", err)
	}
}
