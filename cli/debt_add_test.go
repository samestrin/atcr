package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/history"
	"github.com/samestrin/atcr/internal/localdebt"
)

// emptyDebtStore returns a not-yet-created .atcr/debt directory. localdebt.Append
// creates it lazily on first write, so nothing is pre-seeded.
func emptyDebtStore(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".atcr", "debt")
}

// readDebtStore reads every record back out of dir.
func readDebtStore(t *testing.T, dir string) []localdebt.Record {
	t.Helper()
	recs, err := localdebt.ReadAll(dir, localdebt.ReadOpts{})
	require.NoError(t, err)
	return recs
}

func TestDebtAdd_Wiring(t *testing.T) {
	cmd := newDebtCmd()
	var hasAdd bool
	for _, c := range cmd.Commands() {
		if c.Name() == "add" {
			hasAdd = true
		}
	}
	assert.True(t, hasAdd, "debt has an add subcommand")
}

func TestDebtAdd_FlagMode(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "HIGH", "--file", "internal/x/y.go:12",
		"--problem", "boom", "--fix", "guard it", "--category", "correctness",
		"--est", "30",
	)
	require.NoError(t, err)

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	r := recs[0]
	assert.Equal(t, "HIGH", r.Severity)
	assert.Equal(t, "boom", r.Problem)
	assert.Equal(t, "guard it", r.Fix)
	assert.Equal(t, "correctness", r.Category)
	assert.Equal(t, 30, r.EstMinutes)
	assert.Equal(t, "internal/x/y.go", r.File, "file:line is parsed into structured File + Line")
	assert.Equal(t, 12, r.Line)
	assert.Equal(t, localdebt.SchemaVersion, r.SchemaVersion)
	assert.Equal(t, localdebt.OriginManual, r.Origin, "a hand-filed item is origin=manual, not review")
	assert.Equal(t, history.FindingID(r.File, r.Line, r.Problem), r.ID, "the id is the standard content hash")
	assert.Empty(t, r.Status, "the canonical on-disk spelling of open is the empty status")
}

// The synthetic run_id must satisfy monthFromRunID so the append lands in a real
// month shard rather than failing outright.
func TestDebtAdd_LandsInTheRunIDMonthShard(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "LOW", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Regexp(t, regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])\.jsonl$`), entries[0].Name())
	assert.Equal(t, time.Now().UTC().Format("2006-01")+".jsonl", entries[0].Name())

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	assert.True(t, strings.HasSuffix(recs[0].RunID, "-manual"), "run_id records the manual provenance: %q", recs[0].RunID)
}

// The id must be echoed so the manual add -> resolve round trip is usable
// straight from the terminal.
func TestDebtAdd_EchoesTheAppendedID(t *testing.T) {
	dir := emptyDebtStore(t)
	out, err := runDebt(t, "add", "--dir", dir,
		"--severity", "LOW", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	assert.Contains(t, out, recs[0].ID, "the appended id is echoed for copy-paste into `debt resolve`")
}

// TD: StampID hashes file+line+problem, so re-filing an identical finding reuses
// the id of the existing record. When that id already carries a SUPPRESSING
// status, the append is a no-op as far as every reader is concerned — wontfix
// survives re-detection — yet add printed "Added <id>" and exited 0 while
// `debt list` still showed the terminal status. The add still happens (the store
// is append-only); it now says so on stderr, naming the status that wins.
func TestDebtAdd_WarnsWhenTheIDAlreadyCarriesATerminalStatus(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "HIGH", "--file", "a.go:3", "--problem", "P", "--fix", "F", "--category", "c")
	require.NoError(t, err)
	listed, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	id := debtIDFromListOutput(t, listed)
	_, err = runDebt(t, "resolve", "--dir", dir, "--resolve", id,
		"--status", "wontfix", "--reason", "not a real finding")
	require.NoError(t, err)

	out, err := runDebt(t, "add", "--dir", dir,
		"--severity", "HIGH", "--file", "a.go:3", "--problem", "P", "--fix", "F", "--category", "c")

	require.NoError(t, err, "the append itself still succeeds — the store is append-only")
	assert.Contains(t, out, id, "the warning names the colliding id")
	assert.Contains(t, out, "wontfix", "the warning names the status that wins the fold")
}

// The warning is scoped to a collision that actually suppresses the add: filing a
// brand-new finding must stay silent, or every scripted add grows a spurious
// stderr line.
func TestDebtAdd_DoesNotWarnForAFreshID(t *testing.T) {
	dir := emptyDebtStore(t)

	out, err := runDebt(t, "add", "--dir", dir,
		"--severity", "LOW", "--file", "b.go:9", "--problem", "P", "--fix", "F", "--category", "c")

	require.NoError(t, err)
	assert.NotContains(t, out, "warning", "a fresh finding files silently")
}

// AC1 round trip: an item filed by add is visible to list and closeable by
// resolve, with the id copy-pasted out of list's rendered output. Both halves are
// asserted — the open-filtered disappearance is the closure proof, the unfiltered
// row is the proof that list and resolve read ONE store.
func TestDebtNamespace_AddListResolveRoundTrip(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "HIGH", "--file", "a.go:3",
		"--problem", "P", "--fix", "F", "--category", "correctness")
	require.NoError(t, err)

	listed, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	id := debtIDFromListOutput(t, listed)

	_, err = runDebt(t, "resolve", "--dir", dir, "--resolve", id)
	require.NoError(t, err, "the id read out of `debt list` closes through `debt resolve`")

	open, err := runDebt(t, "list", "--dir", dir, "--status", "open")
	require.NoError(t, err)
	assert.NotContains(t, open, id, "the resolved id leaves the open backlog")

	all, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	assert.Contains(t, all, id, "a bare list still shows the folded terminal record")
	assert.Contains(t, all, "resolved")
}

// debtIDFromListOutput parses the id out of the FIRST data row of a rendered
// `debt list` table — the same copy-paste a human performs. Reading it from the
// rendered text rather than from the record struct is the point: it proves the id
// survives rendering intact (untruncated).
func debtIDFromListOutput(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "table has a header and at least one row:\n%s", out)
	fields := strings.Fields(lines[1])
	require.NotEmpty(t, fields)
	return fields[0]
}

func TestDebtAdd_MissingRequiredNonTTYIsUsageError(t *testing.T) {
	dir := emptyDebtStore(t)
	// Missing --severity/--file/etc. and stdin is not a TTY (bytes buffer).
	_, err := runDebt(t, "add", "--dir", dir, "--problem", "only this")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
}

func TestDebtAdd_MissingRequiredNamesSpecificFlags(t *testing.T) {
	dir := emptyDebtStore(t)
	out, err := runDebt(t, "add", "--dir", dir, "--severity", "HIGH")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	lines := strings.Split(out, "\n")
	require.Greater(t, len(lines), 0)
	errLine := lines[0]
	assert.Contains(t, errLine, "--file")
	assert.Contains(t, errLine, "--problem")
	assert.Contains(t, errLine, "--fix")
	assert.Contains(t, errLine, "--category")
	assert.NotContains(t, errLine, "--severity")
}

// The deleted tdmigrate.Item.Validate enforced the severity/status enums and a
// non-negative estimate on the way into the .planning/ store. localdebt.Append
// has no schema validation, so the command must carry those checks itself or the
// port silently drops them.
func TestDebtAdd_InvalidSeverityIsUsageError(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "URGENT", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Empty(t, readDebtStore(t, dir), "a rejected add writes nothing")
}

func TestDebtAdd_InvalidStatusIsUsageError(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir, "--status", "bogus",
		"--severity", "HIGH", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Empty(t, readDebtStore(t, dir))
}

func TestDebtAdd_NegativeEstIsUsageError(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir, "--est", "-1",
		"--severity", "HIGH", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Empty(t, readDebtStore(t, dir))
}

// The deleted tdmigrate.Item.Validate also rejected blank required fields. The
// command's own gate is a bare != "" on the flag values, so without the ported
// trim-check a whitespace-only answer files an item that debt resolve can never
// act on (selectOpenDebt skips records with no File).
func TestDebtAdd_BlankRequiredFieldsAreUsageErrors(t *testing.T) {
	base := []string{"--severity", "HIGH", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c"}
	for _, blank := range []string{"--file", "--problem", "--fix", "--category"} {
		t.Run(blank, func(t *testing.T) {
			dir := emptyDebtStore(t)
			args := append([]string{"add", "--dir", dir}, base...)
			for i, a := range args {
				if a == blank {
					args[i+1] = "   "
				}
			}
			_, err := runDebt(t, args...)
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err))
			assert.Empty(t, readDebtStore(t, dir), "a rejected add writes nothing")
		})
	}
}

// ":42" has no path before the colon. Splitting it would leave an EMPTY File,
// and a record with no File is skipped by selectOpenDebt — it would list but
// never be closeable, breaking the exact round trip this command tree exists to
// provide. It is instead kept verbatim, the same treatment free text gets, so
// the item stays resolvable.
func TestDebtAdd_LocationWithoutAPathStaysResolvable(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir, "--file", ":42",
		"--severity", "HIGH", "--problem", "p", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	require.NotEmpty(t, recs[0].File, "an empty File would make the item unresolvable")
	assert.Equal(t, ":42", recs[0].File)

	// The round trip must still close it.
	listed, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	_, err = runDebt(t, "resolve", "--dir", dir, "--resolve", debtIDFromListOutput(t, listed))
	require.NoError(t, err)

	open, err := runDebt(t, "list", "--dir", dir, "--status", "open")
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(open), "no matching")
}

// Padding must not change a finding's identity: the same finding filed with and
// without surrounding whitespace is one id, not two.
func TestDebtAdd_TrimsBeforeStampingTheID(t *testing.T) {
	tight := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", tight,
		"--severity", "HIGH", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	padded := emptyDebtStore(t)
	_, err = runDebt(t, "add", "--dir", padded,
		"--severity", "HIGH", "--file", "  a.go:1  ", "--problem", "  p  ", "--fix", " f ", "--category", " c ")
	require.NoError(t, err)

	a, b := readDebtStore(t, tight), readDebtStore(t, padded)
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.Equal(t, a[0].ID, b[0].ID)
	assert.Equal(t, "a.go", b[0].File)
	assert.Equal(t, 1, b[0].Line)
}

func TestDebtAdd_CaseNormalizedEnums(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir,
		"--severity", "high", "--status", "Open",
		"--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	assert.Equal(t, "HIGH", recs[0].Severity)
	// "open" normalizes to the store's canonical empty status rather than being
	// written as a distinct literal, so one id never folds against two spellings
	// of the same state.
	assert.Empty(t, recs[0].Status)
}

func TestDebtAdd_TerminalStatusIsWrittenVerbatim(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir, "--status", "deferred",
		"--severity", "LOW", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.NoError(t, err)

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	assert.Equal(t, "deferred", recs[0].Status)
}

func TestDebtAdd_PartialFlagsIsUsageError(t *testing.T) {
	dir := emptyDebtStore(t)
	out, err := runDebt(t, "add", "--dir", dir, "--severity", "HIGH", "--file", "x.go")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	// The error line must name only the missing flags, not the ones provided.
	lines := strings.Split(out, "\n")
	require.Greater(t, len(lines), 0)
	errLine := lines[0]
	assert.Contains(t, errLine, "--problem")
	assert.Contains(t, errLine, "--fix")
	assert.Contains(t, errLine, "--category")
	assert.NotContains(t, errLine, "--severity")
	assert.NotContains(t, errLine, "--file")
}

// Ported from the deleted filePath test, inverted: the old code STRIPPED a line
// suffix off a free-text File; the new code PARSES it into Line. The trailing-
// all-digits rule is the same, so a non-numeric tail is still kept verbatim.
func TestParseDebtFileLine(t *testing.T) {
	cases := []struct {
		in       string
		wantFile string
		wantLine int
	}{
		{"internal/x/y.go:108", "internal/x/y.go", 108},
		{"cmd/atcr/review.go", "cmd/atcr/review.go", 0},
		{"main.go:3", "main.go", 3},
		// A range is not a single line; the whole value stays in File.
		{"cmd/atcr/autofix.go:248-260", "cmd/atcr/autofix.go:248-260", 0},
		// A colon with a non-numeric tail (free text) is left untouched.
		{"see docs: the thing", "see docs: the thing", 0},
		// A trailing colon is not a line suffix.
		{"a.go:", "a.go:", 0},
		// A leading colon would split to an EMPTY File — a record `debt resolve`
		// can never act on. It stays verbatim so the required-field check rejects
		// the add instead of filing an unresolvable item.
		{":42", ":42", 0},
		// A digit run that overflows an int is not a usable line number.
		{"a.go:99999999999999999999", "a.go:99999999999999999999", 0},
		{"", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			file, line, err := parseDebtFileLine(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantFile, file)
			assert.Equal(t, tc.wantLine, line)
		})
	}
}

// Line 0 is the sentinel for "no line recorded", so a ":0" suffix (zero-padded
// included) can never be a real location: it is rejected rather than collapsed
// onto the same StampID hash as the bare path.
func TestParseDebtFileLine_ZeroLineSuffixIsRejected(t *testing.T) {
	for _, in := range []string{"a.go:0", "a.go:00", "a.go:000"} {
		t.Run(in, func(t *testing.T) {
			_, _, err := parseDebtFileLine(in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "1-based")
		})
	}
}

// A ":0" line suffix is not a location: line numbers are 1-based, and Line 0 is
// the sentinel localdebt.Record uses for "no line recorded". Accepting it would
// collapse "a.go:0" and "a.go" onto the same StampID hash, so a second `debt add`
// with the other spelling would silently reuse an existing record's id. It is a
// usage error instead — in both zero and zero-padded form.
func TestDebtAdd_ZeroLineSuffixIsUsageError(t *testing.T) {
	for _, loc := range []string{"a.go:0", "a.go:00"} {
		t.Run(loc, func(t *testing.T) {
			dir := emptyDebtStore(t)
			_, err := runDebt(t, "add", "--dir", dir,
				"--severity", "LOW", "--file", loc,
				"--problem", "p", "--fix", "f", "--category", "c")
			require.Error(t, err)
			assert.Equal(t, exitUsage, exitCode(err))
			assert.Contains(t, err.Error(), "1-based", "the error explains that line numbers are 1-based")
			assert.Empty(t, readDebtStore(t, dir), "a rejected add writes nothing")
		})
	}
}

func TestPromptEntry_ReadsFieldsAndDefaults(t *testing.T) {
	// Answers, in order: severity, file, problem, fix, category, est, status.
	// Empty lines take the seeded default.
	answers := strings.Join([]string{
		"MEDIUM",      // severity
		"pkg/a.go:9",  // file
		"leaky",       // problem
		"close it",    // fix
		"correctness", // category
		"15",          // est
		"",            // status -> default
	}, "\n") + "\n"

	var out bytes.Buffer
	def := wizardDefaults{Status: "open"}
	rec, err := promptEntry(strings.NewReader(answers), &out, def)
	require.NoError(t, err)

	assert.Equal(t, "MEDIUM", rec.Severity)
	assert.Equal(t, "pkg/a.go", rec.File)
	assert.Equal(t, 9, rec.Line)
	assert.Equal(t, "leaky", rec.Problem)
	assert.Equal(t, "close it", rec.Fix)
	assert.Equal(t, "correctness", rec.Category)
	assert.Equal(t, 15, rec.EstMinutes)
	assert.Equal(t, "open", rec.Status, "the wizard returns the raw answer; runDebtAdd normalizes it")
}

func TestPromptEntry_BlankRequiredFieldRePrompts(t *testing.T) {
	answers := strings.Join([]string{
		"",            // severity -> blank, no default: re-prompt
		"HIGH",        // severity
		"a.go:1",      // file
		"p",           // problem
		"f",           // fix
		"correctness", // category
		"5",           // est
		"open",        // status
	}, "\n") + "\n"

	var out bytes.Buffer
	rec, err := promptEntry(strings.NewReader(answers), &out, wizardDefaults{Status: "open"})
	require.NoError(t, err)
	assert.Equal(t, "HIGH", rec.Severity)
	assert.Contains(t, out.String(), "is required")
}

func TestPromptEntry_RequiredFieldMissingAtEOFErrors(t *testing.T) {
	// EOF before severity (required, and with no seeded default to fall back to).
	var out bytes.Buffer
	_, err := promptEntry(strings.NewReader(""), &out, wizardDefaults{Status: "open"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input ended")
}

func TestPromptEntry_NonIntegerEstFallsBackWithWarning(t *testing.T) {
	answers := strings.Join([]string{
		"HIGH", "a.go:1", "p", "f", "c",
		"soon", // est -> not an integer
		"open",
	}, "\n") + "\n"

	var out bytes.Buffer
	rec, err := promptEntry(strings.NewReader(answers), &out, wizardDefaults{Status: "open", Est: 7})
	require.NoError(t, err)
	assert.Equal(t, 7, rec.EstMinutes, "a non-integer est falls back to the seeded default")
	assert.Contains(t, out.String(), "is not an integer")
}

func TestPromptEntry_InputTooLongErrors(t *testing.T) {
	answers := strings.Join([]string{
		"MEDIUM", "a.go:1", "p", "f", "c", "5",
		strings.Repeat("a", 2*1024*1024), // too-long final answer triggers bufio.ErrTooLong
	}, "\n") + "\n"
	var out bytes.Buffer
	_, err := promptEntry(strings.NewReader(answers), &out, wizardDefaults{Status: "open"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input read error")
}

// errReader emits data, then fails every subsequent Read with err — an input
// stream that dies mid-wizard with a real I/O error rather than a clean EOF.
type errReader struct {
	data string
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

// A false Scan() is not always EOF: a scanner failure (bufio.ErrTooLong, an I/O
// error) must surface its real cause. Before this fix, ask() treated every false
// Scan as end-of-input — a field with a seeded default silently TOOK that default
// off a dead scanner, and the first error to surface was the generic "input
// ended" for the next required field, burying the actual failure.
// A ":0" answer in the wizard is the same user-input validation failure the
// flag path rejects as a usage error (exit 2) — and the wizard's own answer
// validation (finalizeDebtRecord) classifies as usage too. It must NOT fall
// into the wizard's exit-1 bucket, which is reserved for stream failures
// ("input ended", "input read error").
func TestPromptEntry_ZeroLineSuffixIsUsageError(t *testing.T) {
	answers := strings.Join([]string{
		"HIGH",   // severity
		"a.go:0", // file — rejected: line numbers are 1-based
		"p",      // problem
		"f",      // fix
		"c",      // category
		"",       // est
		"",       // status
	}, "\n") + "\n"

	var out bytes.Buffer
	_, err := promptEntry(strings.NewReader(answers), &out, wizardDefaults{Status: "open"})
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err),
		"an invalid location answer is a usage error, matching the flag path")
	assert.Contains(t, err.Error(), "1-based")
}

func TestPromptEntry_ScannerFailureReportsTheRealCause(t *testing.T) {
	// The stream answers severity, then dies. File carries a seeded default:
	// with the bug, ask() silently accepts that default and the wizard goes on
	// to report "input ended" for Problem instead of the read error.
	in := &errReader{data: "HIGH\n", err: errors.New("boom")}
	var out bytes.Buffer
	_, err := promptEntry(in, &out, wizardDefaults{File: "a.go:1", Status: "open"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input read error", "the scanner's real failure is reported")
	assert.Contains(t, err.Error(), "boom")
	assert.NotContains(t, err.Error(), "input ended",
		"a dead scanner must not masquerade as a clean end of input")
}

func TestDebtAdd_PartialFlagsOnTTYSeedsWizard(t *testing.T) {
	dir := emptyDebtStore(t)

	// Force the interactive path without a real TTY.
	orig := debtStdinIsTTY
	debtStdinIsTTY = func(_ io.Reader) bool { return true }
	t.Cleanup(func() { debtStdinIsTTY = orig })

	// severity and file are supplied as flags; on a TTY the partial input must
	// drop into the wizard with those values pre-seeded rather than erroring.
	// Empty answers for severity/file take the seeded flag values; the user
	// only types the still-missing fields.
	answers := strings.Join([]string{
		"",            // severity -> seeded default from --severity
		"",            // file -> seeded default from --file
		"leaky",       // problem
		"close it",    // fix
		"correctness", // category
		"5",           // est
		"open",        // status
	}, "\n") + "\n"

	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(answers))
	cmd.SetArgs([]string{"add", "--dir", dir, "--severity", "HIGH", "--file", "pkg/z.go:5"})
	require.NoError(t, cmd.Execute())

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	assert.Equal(t, "HIGH", recs[0].Severity) // carried from --severity flag
	assert.Equal(t, "pkg/z.go", recs[0].File) // carried from --file flag
	assert.Equal(t, 5, recs[0].Line)          //
	assert.Equal(t, "leaky", recs[0].Problem) // typed into the wizard
}

func TestDebtAdd_InteractiveEndToEnd(t *testing.T) {
	dir := emptyDebtStore(t)

	// Force the interactive path without a real TTY.
	orig := debtStdinIsTTY
	debtStdinIsTTY = func(_ io.Reader) bool { return true }
	t.Cleanup(func() { debtStdinIsTTY = orig })

	answers := strings.Join([]string{
		"LOW",      // severity
		"z.go:3",   // file
		"typo",     // problem
		"fix typo", // fix
		"docs",     // category
		"5",        // est
		"open",     // status
	}, "\n") + "\n"

	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(answers))
	cmd.SetArgs([]string{"add", "--dir", dir})
	require.NoError(t, cmd.Execute())

	recs := readDebtStore(t, dir)
	require.Len(t, recs, 1)
	assert.Equal(t, "LOW", recs[0].Severity)
	assert.Equal(t, "z.go", recs[0].File)
	assert.Equal(t, 3, recs[0].Line)
	assert.Equal(t, localdebt.OriginManual, recs[0].Origin)
}

// An invalid severity typed into the wizard is caught by the same validation the
// flag path uses — the wizard is not a validation bypass.
func TestDebtAdd_InteractiveInvalidSeverityIsUsageError(t *testing.T) {
	dir := emptyDebtStore(t)

	orig := debtStdinIsTTY
	debtStdinIsTTY = func(_ io.Reader) bool { return true }
	t.Cleanup(func() { debtStdinIsTTY = orig })

	answers := strings.Join([]string{"URGENT", "z.go:3", "p", "f", "docs", "5", "open"}, "\n") + "\n"

	cmd := newDebtCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(answers))
	cmd.SetArgs([]string{"add", "--dir", dir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Empty(t, readDebtStore(t, dir))
}

// wontfix is the one permanently-suppressing status, and `debt resolve` requires
// a --reason for it. `debt add` has no --reason, so admitting wontfix here would
// allow a permanent suppression with no recorded rationale — and, since resolve
// then treats the id as settled, no way to attach one afterwards.
func TestDebtAdd_WontfixIsNotAnAddStatus(t *testing.T) {
	dir := emptyDebtStore(t)
	out, err := runDebt(t, "add", "--dir", dir, "--status", "wontfix",
		"--severity", "HIGH", "--file", "a.go:1", "--problem", "p", "--fix", "f", "--category", "c")
	require.Error(t, err)
	assert.Equal(t, exitUsage, exitCode(err))
	assert.Empty(t, readDebtStore(t, dir))
	assert.Contains(t, out+err.Error(), "debt resolve", "the error points at the command that can dismiss a finding")
}

// A deferred item is live debt, so every surface must agree it is actionable:
// list and the dashboard show it, and `debt resolve` can close it. Before this
// was unified on IsSettledStatus, the dashboard ranked it top-priority while
// resolve refused it as "already closed" — and because a manual item is never
// re-detected by reconcile, nothing could ever un-stick it.
func TestDebtNamespace_DeferredItemIsStillCloseable(t *testing.T) {
	dir := emptyDebtStore(t)
	_, err := runDebt(t, "add", "--dir", dir, "--status", "deferred",
		"--severity", "HIGH", "--file", "a.go:3", "--problem", "P", "--fix", "F", "--category", "correctness")
	require.NoError(t, err)

	filed := readDebtStore(t, dir)
	require.Len(t, filed, 1)
	id := filed[0].ID

	listed, err := runDebt(t, "list", "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, listed, id, "a deferred item is visible")

	dash, err := runDebt(t, "dashboard", "--dir", dir)
	require.NoError(t, err)
	require.Contains(t, dash, "**Deferred:** 1", "the dashboard counts it as live debt")

	out, err := runDebt(t, "resolve", "--dir", dir, "--resolve", id)
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(out), "already closed",
		"deferred means not now, not done — it must stay closeable")
	assert.Contains(t, strings.ToLower(out), "marked")

	after := readDebtStore(t, dir)
	require.Len(t, after, 2, "the resolution is appended")

	// And now it IS settled, so a second attempt no-ops.
	out, err = runDebt(t, "resolve", "--dir", dir, "--resolve", id)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(out), "already closed as resolved")
}
