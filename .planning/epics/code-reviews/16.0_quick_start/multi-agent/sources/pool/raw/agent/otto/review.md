LOW|cmd/atcr/quickstart.go:152|`scaffoldWorkflow` uses a hardcoded 0o755 for directories and 0o644 for files|Use a constant or a configuration for default file permissions to ensure consistency across the tool|style|5|`os.MkdirAll(filepath.Dir(wfPath), 0o755)` and `os.WriteFile(wfPath, ..., 0o644)`

LOW|cmd/atcr/quickstart.go:238|`appendExport` uses a hardcoded 0o600 for shell profiles|Ensure this permission is appropriate for all target OSs or make it configurable|style|5|`os.OpenFile(profile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)`

MEDIUM|internal/quickstart/manifest.go:105|`RegistryYAML` uses `fmt.Fprintf` for a simple string builder|Use `b.WriteString` for static strings to avoid the overhead of fmt parsing|idiom|5|`fmt.Fprintf(&b, "  %s:\n", m.Provider.Name)`

LOW|internal/quickstart/workflow.go:36|`WorkflowYAML` uses `go install ...@latest` in a generated CI scaffold|Pin the version to a specific tag (e.g., @v1.0.0) to prevent breaking changes from silently entering consumer CI pipelines|style|10|`run: go install github.com/samestrin/atcr/cmd/atcr@latest`