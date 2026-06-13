package tools

// Default per-call caps for tool results. They bound how much a single tool
// invocation can return to the model, independent of per-agent budgets.
const (
	DefaultMaxReadFileBytes = 64 * 1024 // read_file rendered-output cap
	DefaultMaxGrepMatches   = 200       // grep match-line cap
	DefaultMaxGrepLineBytes = 512       // grep per-match-line length cap
	DefaultMaxListDepth     = 8         // list_files recursion depth cap
	DefaultMaxListFiles     = 1000      // list_files entry cap
	DefaultMaxResultBytes   = 64 * 1024 // dispatcher final-content cap
)

// Limits bounds the size of tool results. A zero field disables that specific
// cap, which is convenient in tests that exercise one cap in isolation.
type Limits struct {
	MaxReadFileBytes int
	MaxGrepMatches   int
	MaxGrepLineBytes int
	MaxListDepth     int
	MaxListFiles     int
	MaxResultBytes   int
}

// DefaultLimits returns the production cap set.
func DefaultLimits() Limits {
	return Limits{
		MaxReadFileBytes: DefaultMaxReadFileBytes,
		MaxGrepMatches:   DefaultMaxGrepMatches,
		MaxGrepLineBytes: DefaultMaxGrepLineBytes,
		MaxListDepth:     DefaultMaxListDepth,
		MaxListFiles:     DefaultMaxListFiles,
		MaxResultBytes:   DefaultMaxResultBytes,
	}
}
