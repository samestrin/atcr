```
LOW|internal/fanout/status.go:52|Data race on nowFunc|Inject clock via struct or use atomic.Value|correctness|20|nowFunc is a mutable package-level variable used in ReadReviewStatus. While current tests are non-parallel, any future parallel test that swaps this clock will cause a data race.|otto
```