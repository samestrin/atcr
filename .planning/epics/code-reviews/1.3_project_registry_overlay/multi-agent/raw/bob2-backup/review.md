

MEDIUM|cmd/atcr/trust.go:92|Incorrect pluralization in trust command output: "wrote %d trust entr%s" produces "entry" for 1 and "entrys" for 2|Change to "wrote %d trust %s" and fix plural to return "entry" for 1 and "entries" for others|maintainability|5|wrote %d trust entr%s to %s\n|bruce