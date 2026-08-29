package reconcile

import "github.com/samestrin/atcr/internal/astgroup"

// tier4Outcome is the verdict of a Tier 4 symbol lookup. STUB — see GREEN.
type tier4Outcome int

const (
	tier4NoMatch tier4Outcome = iota
	tier4Inconclusive
	tier4Resolved
)

// maxSymbolIndexFiles caps the tracked files a Tier 4 index build will parse.
// STUB value — see GREEN.
const maxSymbolIndexFiles = 5000

// symbolIndex maps a declared identifier to the tracked files declaring it.
// STUB — see GREEN.
type symbolIndex struct {
	byName map[string][]string
}

// resolve is the Tier 4 lookup. STUB — see GREEN.
func (x *symbolIndex) resolve(anchors []string) (string, tier4Outcome) {
	return "", tier4NoMatch
}

// parserFactory obtains a parser for a language id. STUB — see GREEN.
type parserFactory func(lang string) (astgroup.Parser, error)

// lazySymbolIndex builds a symbolIndex on first use. STUB — see GREEN.
type lazySymbolIndex struct {
	root      string
	paths     []string
	newParser parserFactory
}

func newLazySymbolIndex(root string, paths []string) *lazySymbolIndex {
	return &lazySymbolIndex{root: root, paths: paths}
}

func (lz *lazySymbolIndex) resolve(anchors []string) (string, tier4Outcome) {
	return "", tier4NoMatch
}
