// Isolated module: built only for GOOS=wasip1 via build.sh. See goparser/go.mod.
module github.com/samestrin/atcr/internal/astgroup/parsers/src/pyparser

go 1.26

require github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi v0.0.0

replace github.com/samestrin/atcr/internal/astgroup/parsers/src/guestabi => ../guestabi
