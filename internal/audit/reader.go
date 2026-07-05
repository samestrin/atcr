package audit

// Load is stubbed for the RED stage: it always returns an empty ledger, so the
// round-trip and tolerant-parse tests fail until the GREEN implementation lands.
func Load(path string) ([]Record, error) {
	return nil, nil
}
