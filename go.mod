module github.com/samestrin/atcr

go 1.25.0

require (
	github.com/bluekeyes/go-gitdiff v0.8.1
	github.com/cli/go-gh/v2 v2.13.0
	github.com/google/jsonschema-go v0.4.3
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	github.com/tetratelabs/wazero v1.12.0
	golang.org/x/mod v0.37.0
)

require (
	github.com/cli/safeexec v1.0.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
)

require (
	github.com/modelcontextprotocol/go-sdk v1.6.1
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/spf13/pflag v1.0.9
	gopkg.in/yaml.v3 v3.0.1
)

// reconcile is the extracted nested module (Epic 8.0), published as its own
// versioned module (github.com/samestrin/atcr/reconcile, tag reconcile/vX.Y.Z)
// so `go install github.com/samestrin/atcr/cmd/atcr@latest` works — a published
// go.mod may not carry replace directives. To develop ./reconcile in-tree,
// create a LOCAL go.work (uncommitted; `go install` ignores it) with only
// `use (. ./reconcile)` — do NOT commit it and do NOT add the isolated wasip1
// parser modules (internal/astgroup/parsers/src/*), whose nested go.mod builds
// break under a root workspace.
require github.com/samestrin/atcr/reconcile v0.1.0
