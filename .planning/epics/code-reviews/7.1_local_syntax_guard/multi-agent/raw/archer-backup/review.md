SECURITY|internal/reconcile/emit.go:128|Missing input validation|Add length check|security|5|user input passed directly to query|bruce
SECURITY|internal/report/render.go:164|XSS vulnerability|Escape HTML output|security|10|user-controlled content rendered in markdown|bruce
PERFORMANCE|internal/verify/syntaxguard.go:100|Unbounded loop|Limit iterations|performance|5|regex FindStringSubmatch in loop|bruce
CORRECTNESS|internal/verify/syntaxguard.go:145|Off-by-one error|Check bounds|correctness|3|slice access without bounds check|bruce
MAINTAINABILITY|internal/verify/executor.go:140|Dead code|Remove unused variable|maintainability|2|unused variable 'fileErr'|bruce
MAINTAINABILITY|internal/verify/syntaxguard.go:55|Unclear naming|Rename variable|maintainability|4|poorly named 'hadFence'|bruce
MAINTAINABILITY|internal/verify/syntaxguard.go:80|Tight coupling|Extract function|maintainability|6|complex validation logic in one function|bruce
OUT-OF-SCOPE|internal/report/render_test.go:35|Test case redundancy|Remove duplicate test|maintainability|1|redundant test case for same functionality|bruce