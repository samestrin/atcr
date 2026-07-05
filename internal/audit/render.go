package audit

import "time"

// RenderReport is stubbed for the RED stage: it returns an empty report, so the
// render tests fail until the GREEN implementation lands.
func RenderReport(recs []Record, pr int, generatedAt time.Time) string {
	return ""
}
