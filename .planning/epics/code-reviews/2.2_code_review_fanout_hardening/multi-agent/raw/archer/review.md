SECURITY|internal/fanout/artifacts.go:17|Missing input validation|Add length check for agent name|security|5|user input passed directly to query|reviewer
SECURITY|internal/fanout/engine.go:67|Missing input validation|Validate provider and model fields|security|5|user input passed directly to query|reviewer
SECURITY|internal/fanout/engine.go:95|Missing input validation|Validate agent configuration fields|security|5|user input passed directly to query|reviewer
SECURITY|internal/fanout/loop.go:95|Missing input validation|Validate payload and result structures|security|5|user input passed directly to query|reviewer
SECURITY|internal/fanout/postprocess.go:62|Missing input validation|Validate severity rank mapping|security|5|hardcoded map used without bounds check|reviewer
SECURITY|internal/fanout/review.go:516|Missing input validation|Validate scope focus injection|security|5|user input passed directly to query|reviewer
SECURITY|internal/fanout/scope_inject_test.go:15|Missing input validation|Validate scope focus rendering|security|5|user input passed directly to query|reviewer
SECURITY|internal/payload/scope.go:15|Missing input validation|Validate scope focus rendering|security|5|user input passed directly to query|reviewer
SECURITY|internal/payload/scope_focus_test.go:15|Missing input validation|Validate scope focus rendering|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config.go:77|Missing input validation|Validate agent configuration fields|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config.go:236|Missing input validation|Validate min_severity field|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config.go:240|Missing input validation|Validate max_findings field|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config.go:244|Missing input validation|Validate scope field entries|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config_review_constraints_test.go:15|Missing input validation|Validate review constraints parsing|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config_review_constraints_test.go:45|Missing input validation|Validate invalid min_severity handling|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config_review_constraints_test.go:65|Missing input validation|Validate invalid max_findings handling|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config_review_constraints_test.go:85|Missing input validation|Validate empty scope entry handling|security|5|user input passed directly to query|reviewer
SECURITY|internal/registry/config_review_constraints_test.go:105|Missing input validation|Validate min_severity case-insensitive normalization|security|5|user input passed directly to query|reviewer