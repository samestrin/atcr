package audit

// Append is stubbed for the RED stage: it accepts records but does not persist
// them yet, so the behavioral tests fail until the GREEN implementation lands.
func Append(path string, records []Record) error {
	return nil
}
