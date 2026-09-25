package cli

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/localdebt"
	"github.com/samestrin/atcr/internal/scorecard"
	"github.com/stretchr/testify/assert"
)

// scorecard.Confirmation hand-mirrors localdebt.QualityRow's counted outcomes
// without importing localdebt (internal/scorecard must not). cli/ imports both,
// so the parity pin lives here, like fanout_outcome_parity_test.go.
//
// The expected set is DERIVED from QualityRow's own "<Outcome>Count" fields, not
// restated. A new counted status needs a new QualityRow counter (the localdebt
// aggregation switch pin forces that), and this test then fails until
// Confirmation gains the matching field. Without it the new outcome would compile
// clean on both sides and never reach trust scoring.
//
// Every integer field is inspected, whatever its kind (int8..int64, uint..uint64,
// or a named integer type), and one that is neither an "<Outcome>Count" counter
// nor a listed non-counter fails the test: a counter added as int64 or under
// another suffix must not drop out of the guard silently.
//
// The adapter that makes these counters live is TD-039's work and stays unwired.
func TestScorecardConfirmation_MirrorsLocaldebtCountedOutcomes(t *testing.T) {
	// nonCounters are QualityRow integer fields that are not per-outcome counters.
	nonCounters := map[string]bool{"TerminalOutcomes": true}

	var counted []string
	row := reflect.TypeOf(localdebt.QualityRow{})
	for i := 0; i < row.NumField(); i++ {
		f := row.Field(i)
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		default:
			continue
		}
		switch {
		case strings.HasSuffix(f.Name, "Count"):
			counted = append(counted, strings.TrimSuffix(f.Name, "Count"))
		case nonCounters[f.Name]:
			delete(nonCounters, f.Name)
		default:
			t.Errorf("localdebt.QualityRow integer field %q is neither an <Outcome>Count counter nor a listed non-counter", f.Name)
		}
	}
	assert.Empty(t, nonCounters, "guard on the guard: every listed non-counter must still exist as an integer field")

	var mirrored []string
	conf := reflect.TypeOf(scorecard.Confirmation{})
	for i := 0; i < conf.NumField(); i++ {
		mirrored = append(mirrored, conf.Field(i).Name)
	}

	sort.Strings(counted)
	sort.Strings(mirrored)
	assert.NotEmpty(t, counted, "guard on the guard: QualityRow's counters must be found")
	assert.Equal(t, counted, mirrored,
		"scorecard.Confirmation must carry exactly one field per localdebt.QualityRow counted outcome")
}
