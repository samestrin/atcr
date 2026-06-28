package reconcile

import (
	"os"

	"github.com/samestrin/atcr/internal/astgroup"
	reclib "github.com/samestrin/atcr/reconcile"
)

// astGroupingDisabledEnv opts out of AST-isomorphism grouping, reverting the
// reconciler to line-proximity-only clustering. AST grouping is ON by default —
// the adopted primary signal once the epic 13.1 benchmark validated AC3 (AST
// recall 1.00 / precision 1.00 vs proximity recall 0.77 with false merges). The
// env var is the reversible-adoption escape hatch: set it to fall back to the
// legacy ±3 behavior without a code change.
const astGroupingDisabledEnv = "ATCR_DISABLE_AST_GROUPING"

// astGrouperFor builds the AST-isomorphism grouper rooted at root (relative
// finding paths resolve against it), or returns a nil grouper — proximity-only —
// when the env opt-out is set. The returned cleanup is always safe to call.
//
// Construction never fails. If a language's .wasm parser cannot load at group
// time, or the source file is absent (e.g. an MCP reconcile without a checked-out
// tree), GroupKey returns "" and that finding falls back to proximity grouping —
// so enabling AST grouping can only refine clustering, never break a reconcile.
func astGrouperFor(root string) (reclib.Grouper, func()) {
	if os.Getenv(astGroupingDisabledEnv) != "" {
		return nil, func() {}
	}
	g := astgroup.NewGrouper(root)
	return g, func() { _ = g.Close() }
}
