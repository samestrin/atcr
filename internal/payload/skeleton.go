package payload

// Skeleton block markers. They deliberately avoid every prefix the rendered-
// payload splitter recognizes as a new file section (`diff --git`, `=== FILE: `,
// `[deleted file: `, `[binary file changed: `, `diff --cc`, `diff --combined`)
// and every prefix the diff path scanner reads (`---`, `+++`, `@@`), so a
// skeleton folds into the file body it belongs to instead of starting a section.
const (
	skeletonStart = ">>> SKELETON (HEAD) <<<"
	skeletonEnd   = ">>> END SKELETON <<<"
)

// renderSkeleton formats a file's extracted declaration headers as a payload
// block, or returns "" when there is nothing structural to show.
func renderSkeleton(entries []skeletonEntry) string { return "" }

// injectSkeleton splices skel into body immediately after body's first line.
func injectSkeleton(body, skel string) string { return body }

// skeletonEntry mirrors astgroup.SkeletonEntry so the rendering helpers stay
// testable without a wasm host.
type skeletonEntry struct {
	StartLine int
	Header    string
}

// fileContext is the per-file measurement and skeleton pass result.
type fileContext struct {
	signals  fileSignals
	skeleton string
}

// analyzeFile measures a changed file and extracts its HEAD skeleton.
func (g *gitRunner) analyzeFile(head string, f changedFile) (fileContext, bool) {
	return fileContext{}, false
}
