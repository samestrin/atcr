package tdmigrate

// ParseReadme splits the technical-debt README into its static preamble
// (everything before the first dated "### [..." section) and the ordered list
// of Items parsed from every dated section's table. IDs are assigned
// sequentially (TD-0001, TD-0002, ...) in document order.
func ParseReadme(content string) (preamble string, items []Item, err error) {
	return "", nil, nil
}

// RenderTable regenerates the dated section tables from items, grouped by their
// Section header in first-seen order. It is the inverse of the table-parsing
// half of ParseReadme and is used to prove round-trip fidelity (the
// items -> README "summary generator").
func RenderTable(items []Item) string {
	return ""
}
