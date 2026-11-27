package cli

// axiPending is the gap list of the AXI coverage guard (cli/axi_coverage_test.go):
// visible commands that print text when piped and have a task scheduled to give
// them a TOON mode (epics 165.2 and 165.2.1). One entry per group, so the task
// that covers a group deletes its own entry and no two tasks edit one line.
// 165.2.1 T5 leaves the map empty.
var axiPending = map[string][]string{
	// diagnostics: epic 165.2 T2.
	"diagnostics": {
		"atcr config set",
		"atcr config reset-install-id",
		"atcr doctor",
		"atcr models",
		"atcr models check",
		"atcr models refresh",
		"atcr trust",
	},
	// setup: epic 165.2 T3.
	"setup": {
		"atcr init",
		"atcr quickstart",
		"atcr skill",
		"atcr skill export",
	},
	// reports: epic 165.2 T4.
	"reports": {
		"atcr history",
		"atcr scorecard",
		"atcr audit-report",
		"atcr quality-report",
		"atcr leaderboard",
	},
	// debt capture and locks: epic 165.2 T5.
	"debt capture": {
		"atcr debt list",
		"atcr debt add",
		"atcr debt resolve",
		"atcr debt lock",
		"atcr debt unlock",
		"atcr debt locks",
		"atcr ignore",
	},
	// personas: epic 165.2.1 T1.
	"personas": {
		"atcr personas list",
		"atcr personas search",
		"atcr personas install",
		"atcr personas remove",
		"atcr personas test",
		"atcr personas submit",
		"atcr personas upgrade",
	},
	// benchmark: epic 165.2.1 T2 (benchmark export and leaderboard export are
	// written exceptions).
	"benchmark": {
		"atcr benchmark verify",
		"atcr benchmark run",
		"atcr benchmark fit",
	},
	// github, diff-smell and dedupe: epic 165.2.1 T3.
	"github and diff-smell": {
		"atcr github",
		"atcr diff-smell",
		"atcr dedupe",
	},
	// debt maintenance: epic 165.2.1 T4 (debt report and debt dashboard are
	// written exceptions).
	"debt maintenance": {
		"atcr debt import",
		"atcr debt sync-outcomes",
		"atcr debt compact",
		"atcr debt convert",
		"atcr debt backfill-justifications",
		"atcr debt recluster",
	},
}
