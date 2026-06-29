LOW|internal/astgroup/parsers/src/braceparser/parse_core.go:174|Shadowed variable name|Rename inner 'line' variable|correctness|5|shadowing 'line' with 'line := i+1' inside same scope|bruce

MEDIUM|internal/astgroup/parsers/src/braceparser/parse_core.go:289|Unused 'heredocPending' tracking|Remove heredocPending variable or use it|maintainability|3|heredocPending set but never read before state change|bruce

MEDIUM|internal/astgroup/parsers/src/braceparser/parse_core.go:522|Incomplete heredoc handling|Add explicit handling for unclosed heredoc|error-handling|10|heredoc without terminator leaves scanner in stHeredoc at EOF|bruce

LOW|internal/astgroup/parsers/src/braceparser/main.go:51|Empty source returns EndLine=1 instead of 0|Document or fix EndLine for empty source|maintainability|2|EndLine:1 for empty input may surprise callers expecting 0|bruce

MEDIUM|internal/astgroup/parsers/src/braceparser/parse_core.go:167|No bounds check before unsafe pointer cast|Add length check before unsafe.Pointer(&b[0])|security|5|make([]byte, n) with n<=0 sets n=1 but no validation of buf access|bruce

LOW|internal/astgroup/parsers/src/braceparser/parse_core.go:456|Inefficient string allocation|Use bytes.Equal instead of string conversion|performance|3|string(src[i:i+len(s)]) == s converts to string unnecessarily|bruce

MEDIUM|internal/astgroup/parsers/src/braceparser/configs.go:86|Missing 'use strict' or module-level strict for JS|Document TS/JS config covers strict mode implicitly|maintainability|2|no explicit handling of "use strict" as keyword vs directive|bruce