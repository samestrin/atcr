# Frozen v1 characterization baseline

These fixtures freeze the v1 `# atcr-findings/v1` parse and write behavior as of sprint 35.16.11.2 (Story 05). They are a baseline, not a target. If a test in `internal/stream/golden_characterization_test.go` fails, v1 behavior changed: fix the code, not these files.

The `*.roundtrip.txt` files hold the exact bytes the v1 writer must produce after parsing their sibling fixture. They cannot carry a comment line, because the writer never emits one, so this note stands in for it.
