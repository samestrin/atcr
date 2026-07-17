


HIGH|internal/scorecard/telemetry.go:18|HashPersonaID uses unsalted SHA-256 vulnerable to dictionary/rainbow-table reversal of enumerable persona IDs|Add HMAC-SHA256 with a provisioned, rotatable application secret (server-side keyed hash acceptable per KB entry); add TD-007 tracking label to the function|Cryptography|30|SHA-256 without salt or HMAC over enumerable input set (persona names); digest is trivially reversible with a precomputed table

HIGH|internal/scorecard/telemetry.go:25|unsafe.Slice/unsafe.StringData on potentially nil string will panic|Validate input or use standard []byte(raw) conversion; the allocation savings are not worth the panic surface in a hash function called on untrusted scorecard data|Reliability|15|unsafe.Slice(unsafe.StringData(raw), len(raw)) panics if raw is nil string

LOW|cmd/atcr/flags.go:39|Hardcoded production endpoint URL in binary is a maintenance hazard if the backend migrates|Make the default configurable via build-time ldflags or a well-documented constant; document the migration path in the inline comment|Configuration|15|const defaultCloudEndpoint = "https://atcr.dev/dashboard"

MEDIUM|cmd/atcr/config.go:30|Long help text warns about ATCR_DISABLE_AST_GROUPING direction but does not document what exit code an I/O failure (missing config file) produces|Add exit-code expectation to the Long doc: "on I/O failure (missing/unwritable .atcr/config.yaml) returns exit 1"|Observability|10|docs_audit_test.go already audits the Long text for env-var coverage; adding exit-code documentation closes the observability gap

LOW|internal/scorecard/telemetry.go:32|NewTelemetryPersonaRecord comment documents HMAC hardening as TODO but no TODO comment or TD label appears in the code itself|Add a TD-007 inline reference so resolve-td can capture and track it|Operational|5|Comment notes HMAC hardening is deferred; no TD label in source for reconciler to act on

MEDIUM|internal/selemetry/event.go:10|Event struct has no validation on any field — arbitrary strings can be written to all four allowlisted keys including Lang and Status which serve as enum-like selectors|Add basic validation in any telemetry Send path that guards Event, Lang, and Status against unexpected values; add a test asserting the four-field constraint|Payload Integrity|20|crypto/scorecard/event.go defines exactly four fields with no omitempty but no guard prevents extra fields or enum-busting values