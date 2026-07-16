package main

import (
	"path/filepath"
	"strings"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/payload"
	"github.com/samestrin/atcr/internal/telemetry"
)

// defaultTelemetryEndpoint is the compiled-in usage-ping ingestion URL. It is
// intentionally empty for now: the external ingestion backend is owned outside
// this repository, and an empty endpoint makes every telemetry Send a safe no-op
// (zero network in dev, CI, and production alike). When a real endpoint is wired
// it MUST be an https:// URL — telemetry.Client refuses plaintext http.
const defaultTelemetryEndpoint = ""

// reviewTelemetryEvent builds the anonymous usage Event for a completed review
// from already-computed grounding data only: a changed-line count and a
// dominant-language label derived from file extensions. It never copies raw diff
// content, file paths, or findings text into the payload (AC 01-04) — only the
// four allowlisted, aggregate fields.
func reviewTelemetryEvent(prep *fanout.PreparedReview, status string) telemetry.Event {
	return telemetry.Event{
		Event:  "review_run",
		Lang:   dominantLang(prep.Changed),
		Lines:  changedLineCount(prep.Changed),
		Status: status,
	}
}

// reconcileTelemetryEvent builds the usage Event for a completed reconcile. A
// reconcile run spans every source and has no single language or line count, so
// lang is empty and lines is zero by an explicit, documented contract (TD-005) —
// deliberately minimal, never accidentally-empty values derived from content.
func reconcileTelemetryEvent(status string) telemetry.Event {
	return telemetry.Event{Event: "reconcile_run", Status: status}
}

// changedLineCount sums the changed head-side line count across all files in the
// review's grounding data — a pure aggregate count, never the line text itself.
func changedLineCount(changed payload.ChangedLines) int {
	n := 0
	for _, fc := range changed {
		n += len(fc.ChangedText)
	}
	return n
}

// dominantLang returns the file-extension label (e.g. "go") of the file with the
// most changed lines, or "" when no file is present or has an extension. The
// output is an aggregate language classification — it leaks neither the path nor
// the content it was derived from.
func dominantLang(changed payload.ChangedLines) string {
	best, bestN := "", 0
	for path, fc := range changed {
		if n := len(fc.ChangedText); n > bestN {
			best = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
			bestN = n
		}
	}
	return best
}
