package stream

import (
	"errors"
	"strings"
)

// EncodeV2EnvelopeForTest writes a v2 source file in the JSON-envelope form
// (the one WriteSourceV2 falls back to when TOON cannot carry a row) with one
// finding per category, so the external category_preservation_test can drive
// that parse path too.
func EncodeV2EnvelopeForTest(categories []string) (string, error) {
	rows := make([]lossyRow, len(categories))
	for i, c := range categories {
		rows[i] = lossyRow{Severity: "HIGH", FileLine: "a.go:1", Category: c, EstMinutes: 5, Reviewer: "bruce", Extra: textish{"t"}}
	}
	var b strings.Builder
	if err := encodeV2(&b, lossyPayload{Findings: rows}); err != nil {
		return "", err
	}
	if !strings.Contains(b.String(), envelopePrefix) {
		return "", errors.New("encodeV2 did not produce the JSON envelope")
	}
	return b.String(), nil
}
