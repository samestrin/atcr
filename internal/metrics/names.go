package metrics

// The atcr metric contract (Epic 4.4). Every metric name lives here so producers
// (the fan-out engine, the CLI review flow) and consumers (the CLI summary, the
// atcr_metrics Prometheus export) reference one source of truth and the exported
// `/metrics` names never drift across packages. Label values are attached with
// Key (which escapes them); the label NAMES are the Label* constants below.
const (
	// Review-level (recorded in fanout.ExecuteReview).
	NameReviewsTotal          = "atcr_reviews_total"
	NameReviewsSucceeded      = "atcr_reviews_succeeded"
	NameReviewsFailed         = "atcr_reviews_failed"
	NameReviewsInterrupted    = "atcr_reviews_interrupted"
	NameReviewDurationSeconds = "atcr_review_duration_seconds"

	// Agent-level (recorded in the fan-out engine's invokeAgent path).
	NameAgentsTotal          = "atcr_agents_total"
	NameAgentsSucceeded      = "atcr_agents_succeeded"
	NameAgentsFailed         = "atcr_agents_failed"
	NameAgentsTimedOut       = "atcr_agents_timed_out"
	NameAgentDurationSeconds = "atcr_agent_duration_seconds"

	// API + tool calls (recorded from the fan-out boundary).
	NameAPICallsTotal  = "atcr_api_calls_total"
	NameAPIErrorsTotal = "atcr_api_errors_total"
	NameToolCallsTotal = "atcr_tool_calls_total"

	// Findings (recorded in fanout.WritePool from the agents' parsed findings).
	NameFindingsTotal      = "atcr_findings_total"
	NameFindingsBySeverity = "atcr_findings_by_severity"

	// Label names used with Key.
	LabelStatus   = "status"
	LabelSeverity = "severity"
)
