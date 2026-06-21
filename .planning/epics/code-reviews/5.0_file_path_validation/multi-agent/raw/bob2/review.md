

HIGH|internal/stream/validate.go:28|Incomplete path escaping check|Add symlink resolution check|maintenance|15|Missing EvalSymlinks for absolute path safety|bruce
HIGH|internal/stream/validate.go:42|Incorrect error handling for stat failures|Return indeterminate state instead of ignoring|correctness|10|Default case leaves PathValid/PathWarning unchanged on stat errors|bruce
MEDIUM|internal/stream/validate_test.go:92|Missing test for symlink escape|Add test for symlink pointing outside root|maintenance|20|No test coverage for symlink-based path escapes|bruce
LOW|internal/reconcile/validate_test.go:132|Unclear test variable naming|Rename 'byFile' to 'findingsByFile' for clarity|maintenance|5|Variable name 'byFile' is ambiguous|bruce
LOW|internal/report/render.go:312|Missing newline after path warning|Add newline after warning line|maintenance|2|Warning line doesn't end with newline|bruce
LOW|internal/report/render.go:356|Missing newline after path warning in refuted section|Add newline after warning line|maintenance|2|Warning line doesn't end with newline|bruce
LOW|internal/stream/validate.go:15|Unused import|Remove unused 'strings' import|maintenance|2|Import 'strings' declared but not used|bruce
LOW|internal/stream/validate_test.go:12|Unused import|Remove unused 'require' import|maintenance|2|Import 'require' declared but not used|bruce
LOW|internal/reconcile/validate_test.go:12|Unused import|Remove unused 'require' import|maintenance|2|Import 'require' declared but not used|bruce
LOW|internal/report/validate_test.go:12|Unused import|Remove unused 'require' import|maintenance|2|Import 'require' declared but not used|bruce
LOW|internal/stream/validate_test.go:11|Unused import|Remove unused 'filepath' import|maintenance|2|Import 'filepath' declared but not used|bruce
LOW|internal/stream/validate_test.go:10|Unused import|Remove unused 'os' import|maintenance|2|Import 'os' declared but not used|bruce
LOW|internal/reconcile/validate_test.go:11|Unused import|Remove unused 'filepath' import|maintenance|2|Import 'filepath' declared but not used|bruce
LOW|internal/reconcile/validate_test.go:10|Unused import|Remove unused 'os' import|maintenance|2|Import 'os' declared but not used|bruce
LOW|internal/report/validate_test.go:11|Unused import|Remove unused 'filepath' import|maintenance|2|Import 'filepath' declared but not used|bruce
LOW|internal/report/validate_test.go:10|Unused import|Remove unused 'os' import|maintenance|2|Import 'os' declared but not used|bruce
LOW|internal/stream/validate.go:54|Redundant variable declaration|Simplify joined path calculation|maintenance|3|Variable 'joined' declared then immediately used|bruce
LOW|internal/stream/validate.go:62|Redundant variable declaration|Simplify relative path calculation|maintenance|3|Variable 'rel' declared then immediately used|bruce
LOW|internal/stream/validate.go:68|Inconsistent error handling|Use early return for indeterminate case|maintenance|5|Default case could use early return|bruce
LOW|internal/stream/validate_test.go:66|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:115|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:164|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:213|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:262|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:311|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:360|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:409|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go:458|Hardcoded test path|Use t.TempDir() for base path|maintenance|5|Test uses hardcoded 'internal/auth/validate.go'|bruce
LOW|internal/stream/validate_test.go