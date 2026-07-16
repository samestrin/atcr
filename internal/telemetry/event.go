package telemetry

// Event is the sole allowlisted telemetry payload. It has exactly four fields
// with no omitempty tags, and deliberately does NOT embed or extend any
// scorecard struct — so no source code, file path, or identifier can ever leak
// beyond {event, lang, lines, status}. Adding any field here is a privacy
// regression that TestClient_Send_PayloadHasExactlyFourAllowlistedKeys guards.
type Event struct {
	Event  string `json:"event"`
	Lang   string `json:"lang"`
	Lines  int    `json:"lines"`
	Status string `json:"status"`
}
