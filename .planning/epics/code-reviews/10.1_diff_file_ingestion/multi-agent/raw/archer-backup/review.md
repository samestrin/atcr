SECURITY|internal/fanout/ingest_review_test.go:15|Missing input validation|Add length check and path traversal guard|security|5|diff text passed directly to query|bruce
SECURITY|internal/fanout/ingest_review_test.go:25|Missing input validation|Add size cap mirroring DefaultMaxDiffBytes|security|5|oversized diff exceeds memory limit|bruce
PERFORMANCE|internal/fanout/ingest_review_test.go:45|Unbounded loop|Limit read size to DefaultMaxDiffBytes|performance|3|large diff exhausts memory|bruce
CORRECTNESS|internal/fanout/ingest_review_test.go:65|Logic error|Handle empty diff before scaffold|correctness|2|empty diff produces vacuous review|bruce
MAINTAINABILITY|internal/fanout/ingest_review_test.go:85| unclear naming|Rename diffReq to diffReviewRequest|maintainability|1|ambiguous function name|bruce
MAINTAINABILITY|internal/fanout/ingest_review_test.go:105| unclear naming|Rename PrepareReviewFromDiff to PrepareReviewFromDiffText|maintainability|1|misleading function name|bruce