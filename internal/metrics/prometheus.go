package metrics

import (
	"fmt"
	"math"
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
	// Snapshot the metric pointers under the registry lock, then release it before
	// rendering. Counter values are atomic and each histogram is read under its
	// own lock via snapshot(), so no metric is read while r.mu is held — a scrape
	// no longer serializes against metric registration for its whole duration.
	r.mu.Lock()
	counters := make(map[string]*counter, len(r.counters))
	for k, c := range r.counters {
		counters[k] = c
	}
	gauges := make(map[string]*gauge, len(r.gauges))
	for k, g := range r.gauges {
		gauges[k] = g
	}
	histograms := make(map[string]*histogram, len(r.histograms))
	for k, h := range r.histograms {
		histograms[k] = h
	}
	r.mu.Unlock()

	var b strings.Builder

	ctrKeys := make([]string, 0, len(counters))
	for k := range counters {
		ctrKeys = append(ctrKeys, k)
	}
	writeFamily(&b, ctrKeys, "counter", func(_, k string) {
		fmt.Fprintf(&b, "%s %d\n", k, counters[k].Value())
	})

	gaugeKeys := make([]string, 0, len(gauges))
	for k := range gauges {
		gaugeKeys = append(gaugeKeys, k)
	}
	writeFamily(&b, gaugeKeys, "gauge", func(_, k string) {
		fmt.Fprintf(&b, "%s %s\n", k, formatFloat(gauges[k].Value()))
	})

	histKeys := make([]string, 0, len(histograms))
	for k := range histograms {
		histKeys = append(histKeys, k)
	}
	// One lock + one sort per histogram per scrape: snapshot returns every
	// quantile, the sum, and the count together, so a family's lines are also
	// internally consistent against a concurrent Observe.
	writeFamily(&b, histKeys, "summary", func(fam, k string) {
		_, inner := splitLabels(k)
		sum, count, pcts := histograms[k].snapshot(summaryQuantiles)
		for i, q := range summaryQuantiles {
			fmt.Fprintf(&b, "%s%s %s\n", fam, withQuantile(inner, q), formatFloat(pcts[i]))
		}
		fmt.Fprintf(&b, "%s_sum%s %s\n", fam, labelSuffix(inner), formatFloat(sum))
		fmt.Fprintf(&b, "%s_count%s %d\n", fam, labelSuffix(inner), count)
	})

	return b.String()
}

// snapshot reads a histogram's aggregate state in a single lock acquisition: the
// running sum, the observation count, and the nearest-rank value for each
// requested quantile (each given as a fraction in [0,1]), all computed from one
// sorted copy of the sample window. It reuses the same sorted cache as
// Percentile, so a scrape pays the O(n log n) sort at most once even across the
// four summary quantiles. Replaces four independent Percentile() calls (four
// locks, four sorts) per histogram per scrape.
func (h *histogram) snapshot(quantiles []float64) (sum float64, count int64, percentiles []float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sum = h.sum
	count = h.count
	percentiles = make([]float64, len(quantiles))
	if len(h.values) == 0 {
		return sum, count, percentiles
	}
	if h.cacheDirty || h.sortedCache == nil {
		h.sortedCache = make([]float64, len(h.values))
		copy(h.sortedCache, h.values)
		sort.Float64s(h.sortedCache)
		h.cacheDirty = false
	}
	n := len(h.sortedCache)
	for i, q := range quantiles {
		q = min(max(q, 0), 1)
		rank := min(max(int(math.Ceil(q*float64(n))), 1), n)
		percentiles[i] = h.sortedCache[rank-1]
	}
	return sum, count, percentiles
}

// writeFamily groups keys by Prometheus metric family, emits one TYPE header per
// family, then calls renderKey(fam, key) for each key within the family. Keys and
// families are both emitted in sorted order for stable output.
func writeFamily(b *strings.Builder, keys []string, typeKeyword string, renderKey func(fam, key string)) {
	families := make(map[string][]string, len(keys))
	for _, k := range keys {
		f := metricFamily(k)
		families[f] = append(families[f], k)
	}
	for _, fam := range sortedKeys(families) {
		fmt.Fprintf(b, "# TYPE %s %s\n", fam, typeKeyword)
		ks := families[fam]
		sort.Strings(ks)
		for _, k := range ks {
			renderKey(fam, k)
		}
	}
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
