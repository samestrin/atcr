package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/samestrin/atcr/internal/reconcile"
)

// RenderDisagreements writes the focused `atcr report --disagreements` view: a
// ranked list of the highest-tension spots in a change, each with the model
// positions side by side. Free text is HTML-escaped and newline-flattened and
// file paths render in backtick code spans, the same injection defenses the main
// report applies.
func RenderDisagreements(w io.Writer, df reconcile.DisagreementsFile) error {
	var b bytes.Buffer
	b.WriteString("# atcr Disagreement Radar\n\n")
	if len(df.Items) == 0 {
		b.WriteString("No disagreements detected.\n")
		_, err := w.Write(b.Bytes())
		return err
	}
	fmt.Fprintf(&b, "%d tension spot(s), highest first.\n", len(df.Items))
	// Standalone radar view uses "## " item headings and escTrunc (500-rune cap)
	// for display; the shared renderer lives in reconcile (report imports
	// reconcile; the reverse would be circular).
	reconcile.WriteRadarItems(&b, df.Items, "## ", escTrunc)
	_, err := w.Write(b.Bytes())
	return err
}

// RenderDisagreementsJSON writes the disagreements radar as indented JSON. The
// DisagreementsFile already carries the schema version and machine-contract
// field names (see TestDisagreementsSchema_StableContract); this is the format
// `atcr report --disagreements --format json` emits.
func RenderDisagreementsJSON(w io.Writer, df reconcile.DisagreementsFile) error {
	data, err := json.MarshalIndent(df, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
