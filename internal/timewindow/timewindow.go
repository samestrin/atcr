// Package timewindow holds the single grammar behind every `--since`-style
// window flag in atcr.
//
// It exists because there used to be two. `atcr history --since` delegated to
// time.ParseDuration (extended with d/w), so "3m" was three MINUTES; `atcr
// leaderboard --since` read integer+d/w/m, so "3m" was three MONTHS. Both
// accepted the input without a warning and the resulting windows differed by
// roughly 43,200x. A flag name that means two things is worse than a flag that
// rejects an input, so the clock units are gone: a window is expressed in days,
// weeks, or 30-day months, or it is the literal "all".
package timewindow

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// All is the window "all" resolves to: ~292 years, the largest duration
// time.Duration can hold. It is a real duration rather than a zero sentinel on
// purpose — callers subtract it from now to build a cutoff, and several of them
// (internal/history's bounded readers) treat a non-positive window as "select
// nothing". Handing those a large positive number keeps "all" meaning "every
// record" without weakening the bounded-query contract they document.
const All = time.Duration(math.MaxInt64)

// AllLiteral is the spelling that disables the window.
const AllLiteral = "all"

const (
	perDay   = 24 * time.Hour
	perWeek  = 7 * 24 * time.Hour
	perMonth = 30 * 24 * time.Hour // a rolling window, not a calendar month
)

// Parse converts a window string into a duration.
//
// The grammar is a positive integer followed by one unit — "d" days, "w" weeks,
// "m" 30-day months — or the literal "all", which returns All. Everything else
// is a usage error, including the clock units time.ParseDuration accepts
// ("48h", "1h30m", "30s") and fractional counts ("1.5d"): a window flag that
// silently accepted both "3 months" and "3 minutes" for the same text is the
// defect this package exists to remove, so those spellings fail loudly rather
// than resolving to one of the two old meanings.
//
// "m" is a fixed 30-day month, independent of the calendar. That is a deliberate
// approximation: --since defines a rolling window, so "1m" is always exactly 30
// days. It intentionally does NOT match the on-disk month-file rotation in
// internal/history, which uses real calendar months (28-31 days); window
// semantics and storage rotation are independent concerns.
func Parse(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, AllLiteral) {
		return All, nil
	}
	if len(s) < 2 {
		return 0, invalidErr(s)
	}

	var per time.Duration
	switch s[len(s)-1] {
	case 'd':
		per = perDay
	case 'w':
		per = perWeek
	case 'm':
		per = perMonth
	default:
		return 0, invalidErr(s)
	}

	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, invalidErr(s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("invalid window %q: must be a positive duration", s)
	}
	if int64(n) > math.MaxInt64/int64(per) {
		return 0, fmt.Errorf("window %q is out of range", s)
	}
	return time.Duration(n) * per, nil
}

// invalidErr is the one rejection message. It names the whole accepted set so a
// user who typed a clock unit learns the grammar from the error rather than from
// the source.
func invalidErr(s string) error {
	return fmt.Errorf(`invalid window %q: use Nd (days), Nw (weeks), Nm (30-day months), or "all" (e.g. 30d, 2w, 3m, all). Clock units (h/m/s as hours/minutes/seconds) are not accepted — "3m" is three months`, s)
}
