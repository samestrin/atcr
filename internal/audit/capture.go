package audit

import "time"

// RecordReview is stubbed for the RED stage: it records nothing yet, so the
// capture tests fail until the GREEN implementation lands.
func RecordReview(auditPath, reviewDir string, ts time.Time, pr int, base, head string) (int, error) {
	return 0, nil
}
