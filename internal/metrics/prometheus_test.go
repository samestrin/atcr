package metrics

import (
	"strings"
	"testing"
)

func TestWritePrometheusEmpty(t *testing.T) {
	r := NewRegistry()
	if got := r.WritePrometheus(); got != "" {
		t.Fatalf("empty registry WritePrometheus() = %q, want empty", got)
	}
}

func TestWritePrometheusCounters(t *testing.T) {
	r := NewRegistry()
	r.Counter("atcr_reviews_total").Add(5)
	// Labeled family: two keyed variants must share one # TYPE header.
	r.Counter(Key("atcr_api_errors_total", "status", "429")).Add(3)
	r.Counter(Key("atcr_api_errors_total", "status", "500")).Inc()

	out := r.WritePrometheus()

	wantLines := []string{
		"# TYPE atcr_api_errors_total counter",
		`atcr_api_errors_total{status="429"} 3`,
		`atcr_api_errors_total{status="500"} 1`,
		"# TYPE atcr_reviews_total counter",
		"atcr_reviews_total 5",
	}
	for _, w := range wantLines {
		if !strings.Contains(out, w) {
			t.Errorf("output missing line %q\n---\n%s", w, out)
		}
	}
	// Exactly one TYPE header per family.
	if n := strings.Count(out, "# TYPE atcr_api_errors_total counter"); n != 1 {
		t.Errorf("api_errors_total TYPE header appeared %d times, want 1\n%s", n, out)
	}
}

func TestWritePrometheusHistogramUnlabeled(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("atcr_review_duration_seconds")
	h.Observe(1)
	h.Observe(2)
	h.Observe(3)
	h.Observe(4)

	out := r.WritePrometheus()
	for _, w := range []string{
		"# TYPE atcr_review_duration_seconds summary",
		`atcr_review_duration_seconds{quantile="0.5"} `,
		`atcr_review_duration_seconds{quantile="0.99"} `,
		"atcr_review_duration_seconds_sum 10",
		"atcr_review_duration_seconds_count 4",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("histogram output missing %q\n---\n%s", w, out)
		}
	}
}

func TestWritePrometheusHistogramLabeled(t *testing.T) {
	r := NewRegistry()
	// A labeled histogram key exercises the inner-label merge branches.
	r.Histogram(Key("atcr_agent_duration_seconds", "persona", "skeptic")).Observe(2)

	out := r.WritePrometheus()
	for _, w := range []string{
		"# TYPE atcr_agent_duration_seconds summary",
		`atcr_agent_duration_seconds{persona="skeptic",quantile="0.5"} `,
		`atcr_agent_duration_seconds_sum{persona="skeptic"} 2`,
		`atcr_agent_duration_seconds_count{persona="skeptic"} 1`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("labeled histogram output missing %q\n---\n%s", w, out)
		}
	}
}

func TestWritePrometheusHistogramSameFamilyOneTypeHeader(t *testing.T) {
	r := NewRegistry()
	// Two labeled histograms sharing a family must emit exactly one TYPE header
	// (duplicate TYPE lines are invalid Prometheus exposition format).
	r.Histogram(Key("atcr_agent_duration_seconds", "persona", "a")).Observe(1)
	r.Histogram(Key("atcr_agent_duration_seconds", "persona", "b")).Observe(2)

	out := r.WritePrometheus()
	if n := strings.Count(out, "# TYPE atcr_agent_duration_seconds summary"); n != 1 {
		t.Errorf("same-family histograms emitted %d TYPE headers, want 1\n%s", n, out)
	}
	for _, w := range []string{
		`atcr_agent_duration_seconds{persona="a",quantile="0.5"} `,
		`atcr_agent_duration_seconds{persona="b",quantile="0.5"} `,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\n%s", w, out)
		}
	}
}

func TestKeyEscapesLabelValue(t *testing.T) {
	// A value with a quote, backslash, and newline must be escaped so it cannot
	// break out of the label and inject extra Prometheus series.
	k := Key("m", "label", "a\"b\\c\nd")
	want := `m{label="a\"b\\c\nd"}`
	if k != want {
		t.Fatalf("Key escaping = %q, want %q", k, want)
	}
	// Rendered output keeps the escaped form intact.
	r := NewRegistry()
	r.Counter(k).Inc()
	out := r.WritePrometheus()
	if !strings.Contains(out, want+" 1") {
		t.Errorf("rendered output missing escaped key %q\n%s", want, out)
	}
}

func TestKeyEscapesCarriageReturn(t *testing.T) {
	// A carriage return must be escaped to \r so a label value cannot inject a
	// line break into the exposition stream (escapeLabelValue's doc comment calls
	// the escaping a security boundary).
	k := Key("m", "label", "a\rb")
	want := `m{label="a\rb"}`
	if k != want {
		t.Fatalf("Key escaping of CR = %q, want %q", k, want)
	}
	// A CRLF must produce \r\n, not a raw CR followed by escaped \n.
	if got := Key("m", "label", "x\r\ny"); got != `m{label="x\r\ny"}` {
		t.Fatalf("Key escaping of CRLF = %q, want %q", got, `m{label="x\r\ny"}`)
	}
}

func TestMetricFamilyAndSplitLabels(t *testing.T) {
	if got := metricFamily("m"); got != "m" {
		t.Errorf("metricFamily(m) = %q, want m", got)
	}
	if got := metricFamily(`m{a="1"}`); got != "m" {
		t.Errorf("metricFamily labeled = %q, want m", got)
	}
	if fam, inner := splitLabels("m"); fam != "m" || inner != "" {
		t.Errorf("splitLabels(m) = %q,%q want m,''", fam, inner)
	}
	if fam, inner := splitLabels(`m{a="1"}`); fam != "m" || inner != `a="1"` {
		t.Errorf("splitLabels labeled = %q,%q want m,a=\"1\"", fam, inner)
	}
}

func TestFormatFloat(t *testing.T) {
	cases := map[float64]string{0: "0", 1.5: "1.5", 10: "10"}
	for in, want := range cases {
		if got := formatFloat(in); got != want {
			t.Errorf("formatFloat(%v) = %q, want %q", in, got, want)
		}
	}
}
