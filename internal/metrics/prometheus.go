package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// summaryQuantiles are the quantiles emitted for every histogram in the
// Prometheus summary exposition. p50/p90/p95/p99 are the latency cut points
// operators dashboard on (Epic 4.4: p95 review latency).
var summaryQuantiles = []float64{0.5, 0.9, 0.95, 0.99}

// Key builds a single-label metric key in Prometheus syntax —
// `name{label="value"}` — escaping the value so a backslash, double-quote, or
// newline in it cannot break out of the label and inject extra series. Label
// values can originate from model output (e.g. a finding severity), so the
// escaping is a security boundary, not a cosmetic nicety. The result is used as
// the registry key AND rendered verbatim, so escaping happens once, here.
func Key(name, label, value string) string {
	return name + "{" + label + `="` + escapeLabelValue(value) + `"}`
}

// escapeLabelValue applies the Prometheus text-format label-value escapes.
// Backslash MUST be replaced first so the escapes it introduces are not
// re-escaped. Carriage return and newline are both escaped so no label value can
// inject a line break into the exposition stream.
func escapeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\r", `\r`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// metricFamily returns the family name for a registry key: the portion before
// the first '{'. Keys without labels are their own family. Same-family keys
// (e.g. atcr_api_errors_total{status="429"} and {status="500"}) share one
// "# TYPE" header.
func metricFamily(key string) string {
	if i := strings.IndexByte(key, '{'); i >= 0 {
		return key[:i]
	}
	return key
}

// splitLabels splits a registry key into its family and the inner label text
// (without the braces): `m{a="1"}` → ("m", `a="1"`); `m` → ("m", "").
func splitLabels(key string) (family, inner string) {
	i := strings.IndexByte(key, '{')
	if i < 0 {
		return key, ""
	}
	return key[:i], strings.TrimSuffix(key[i+1:], "}")
}

// WritePrometheus renders the whole registry in Prometheus text exposition
// format, deterministically (families and keys sorted) so the output is stable
// for scraping and tests. Counters render as `counter`; histograms render as
// `summary` (per-quantile lines plus _sum and _count). Metrics are cumulative
// since the registry was created (Epic 4.4: cumulative since server start).
func (r *Registry) WritePrometheus() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var b strings.Builder

	// Counters, grouped by family so each family emits exactly one TYPE header
	// even when keys with a shared family do not sort contiguously next to a
	// neighbouring family's keys.
	families := make(map[string][]string, len(r.counters))
	for k := range r.counters {
		f := metricFamily(k)
		families[f] = append(families[f], k)
	}
	for _, fam := range sortedKeys(families) {
		fmt.Fprintf(&b, "# TYPE %s counter\n", fam)
		keys := families[fam]
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %d\n", k, r.counters[k].Value())
		}
	}

	// Histograms as Prometheus summaries, grouped by family so each family emits
	// exactly one TYPE header even when it has several labeled keyed variants
	// (duplicate TYPE lines are invalid exposition format).
	histFamilies := make(map[string][]string, len(r.histograms))
	for k := range r.histograms {
		f := metricFamily(k)
		histFamilies[f] = append(histFamilies[f], k)
	}
	for _, fam := range sortedKeys(histFamilies) {
		fmt.Fprintf(&b, "# TYPE %s summary\n", fam)
		keys := histFamilies[fam]
		sort.Strings(keys)
		for _, k := range keys {
			h := r.histograms[k]
			_, inner := splitLabels(k)
			for _, q := range summaryQuantiles {
				fmt.Fprintf(&b, "%s%s %s\n", fam, withQuantile(inner, q), formatFloat(h.Percentile(q*100)))
			}
			fmt.Fprintf(&b, "%s_sum%s %s\n", fam, labelSuffix(inner), formatFloat(h.Sum()))
			fmt.Fprintf(&b, "%s_count%s %d\n", fam, labelSuffix(inner), h.Count())
		}
	}

	return b.String()
}

// sortedKeys returns the map keys in sorted order.
func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// withQuantile renders the label set for a summary quantile line, merging the
// quantile label into any pre-existing labels on the key.
func withQuantile(inner string, q float64) string {
	if inner == "" {
		return fmt.Sprintf(`{quantile="%s"}`, formatFloat(q))
	}
	return fmt.Sprintf(`{%s,quantile="%s"}`, inner, formatFloat(q))
}

// labelSuffix renders the label braces for the _sum/_count lines, or "" when the
// key carries no labels.
func labelSuffix(inner string) string {
	if inner == "" {
		return ""
	}
	return "{" + inner + "}"
}

// formatFloat renders a metric value the way Prometheus expects: shortest exact
// decimal, no trailing zeros (1.5 → "1.5", 10 → "10", 0 → "0").
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
