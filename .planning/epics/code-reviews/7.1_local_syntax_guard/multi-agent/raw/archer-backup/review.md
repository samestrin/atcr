SECURITY|internal/reconcile/emit.go:128|Missing input validation|Add length check for log message|security|5|user input passed directly to query|bruce
SECURITY|internal/verify/syntaxguard.go:102|Missing input validation|Sanitize fix string before parsing|security|5|user input passed directly to query|bruce
ERROR|internal/report/render.go:164|Missing error handling|Add error check for Fprintf|error-handling|5|user input passed directly to query|bruce
LOW|internal/verify/executor_test.go:92|Dead code|Remove unused variable 'hadFence'|maintainability|2|variable declared but not used|bruce
LOW|internal/verify/syntaxguard.go:143|Unclear naming|Rename 's' parameter to 'src'|maintainability|3|parameter name is too generic|bruce
MEDIUM|internal/verify/syntaxguard.go:150|Missing error handling|Check result of extractFencedCode|error-handling|5|function return values ignored|bruce
MEDIUM|internal/verify/syntaxguard.go:180|Missing error handling|Check result of parseGoFix|error-handling|5|function return values ignored|brice