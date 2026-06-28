package reconcile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/samestrin/atcr/internal/astgroup"
	reclib "github.com/samestrin/atcr/reconcile"
)

// astGroupingDisabledEnv opts out of AST-isomorphism grouping, reverting the
// reconciler to line-proximity-only clustering. AST grouping is ON by default —
// the adopted primary signal once the epic 13.1 benchmark validated AC3 (AST
// recall 1.00 / precision 1.00 vs proximity recall 0.77 with false merges). The
// env var is the reversible-adoption escape hatch: set it to fall back to the
// legacy ±3 behavior without a code change.
//
// The value is parsed as a boolean (see astGroupingDisabled): a truthy value
// (1, t, T, TRUE, true, True) disables grouping; a falsy value (0, false, …), an
// unparseable value, and an unset var all KEEP grouping on — so
// ATCR_DISABLE_AST_GROUPING=false / =0 do the intuitive thing rather than the
// legacy presence-only footgun where any non-empty value disabled.
const astGroupingDisabledEnv = "ATCR_DISABLE_AST_GROUPING"

// lazyGrouper wraps an astgroup.Grouper and defers obtaining the shared wazero
// Host until the first finding with a supported language extension is seen. This
// avoids touching the runtime on reconciles whose findings do not target a parsed
// language; once obtained, the process-lifetime Host (astgroup.SharedHost) is
// reused across reconciles so WASI plus per-language module instantiation is paid
// at most once per process, not once per RunReconcile call.
type lazyGrouper struct {
	root       string
	newGrouper func(string) *astgroup.Grouper
	once       sync.Once
	g          *astgroup.Grouper
}

func newLazyGrouper(root string) *lazyGrouper {
	return &lazyGrouper{
		root:       root,
		newGrouper: func(root string) *astgroup.Grouper { return astgroup.NewGrouper(root, astgroup.SharedHost()) },
	}
}

// GroupKey returns the AST-isomorphism key for f, or "" to fall back to
// proximity. It short-circuits for files whose extension has no parser,
// avoiding runtime construction entirely on those files.
func (lg *lazyGrouper) GroupKey(f reclib.Finding) string {
	if lang := astgroup.LanguageForExt(strings.ToLower(filepath.Ext(f.File))); lang == "" {
		return ""
	}
	lg.once.Do(func() { lg.g = lg.newGrouper(lg.root) })
	if lg.g == nil {
		return ""
	}
	return lg.g.GroupKey(f)
}

// Close releases the grouper's per-run file-tree cache if it was constructed. The
// shared wazero Host (astgroup.SharedHost) is process-lifetime and is deliberately
// not closed here, so its compiled-parser cache survives for the next reconcile.
func (lg *lazyGrouper) Close() error {
	if lg.g != nil {
		return lg.g.Close()
	}
	return nil
}

// astGrouperFor builds the AST-isomorphism grouper rooted at root (relative
// finding paths resolve against it), or returns a nil grouper — proximity-only —
// when the env opt-out is set. The returned cleanup is always safe to call.
//
// Construction is lazy: if no finding maps to a supported language extension,
// the underlying wazero runtime is never created. If a language's .wasm parser
// cannot load at group time, or the source file is absent (e.g. an MCP reconcile
// without a checked-out tree), GroupKey returns "" and that finding falls back
// to proximity grouping, so a missing parser or absent tree never errors a
// reconcile.
//
// AST grouping does NOT strictly refine proximity clustering, however. ClusterWith
// pulls keyed findings out of the line stream before proximity-clustering the
// unkeyed remainder, so a keyed finding and an unkeyed near-duplicate straddling
// it can land in separate clusters and never be compared — AST grouping can thus
// REDUCE the merge count relative to proximity-only in mixed keyed/unkeyed
// scenarios, not only increase precision.
func astGrouperFor(root string) (reclib.Grouper, func()) {
	if astGroupingDisabled() {
		return nil, func() {}
	}
	lg := newLazyGrouper(root)
	return lg, func() {
		if err := lg.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warn: astgroup grouper close: %v\n", err)
		}
	}
}

// astGroupingDisabled reports whether the opt-out env var is set to a truthy
// boolean. The value is parsed with strconv.ParseBool: a truthy value disables
// grouping, while a falsy value, an unparseable value, and an unset var all keep
// it on (so =0/=false do the intuitive thing, not the legacy presence-only
// behavior where any non-empty value disabled).
func astGroupingDisabled() bool {
	disabled, err := strconv.ParseBool(os.Getenv(astGroupingDisabledEnv))
	return err == nil && disabled
}
