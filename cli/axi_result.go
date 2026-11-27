package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	toon "github.com/toon-format/toon-go"
)

// The shared AXI result layer (epic 165.2 T1). A command that gains a TOON mode
// marks itself with markAXIDefault, so the root's resolveAXIMode decides its
// mode like any --axi command's, and renders its result with writeAXIResult.
// Its help[] lines come from the suggestion registry; a command group adds its
// keys with registerSuggestions from an init() in its own cli/axi_help_<group>.go.

// axiResultAnnotation is the axiDefaultAnnotation value markAXIDefault sets.
const axiResultAnnotation = "result"

// markAXIDefault marks cmd as having an AXI mode without an --axi flag, so the
// agent-path default (resolveAXIMode) applies to it, and returns cmd.
func markAXIDefault(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[axiDefaultAnnotation] = axiResultAnnotation
	return cmd
}

// axiResult is one command's AXI result: ok, the summary fields, and for a
// list-shaped result a count and the items, then the help[] lines.
type axiResult struct {
	// Failed renders ok: false (a check that found problems); ok is true otherwise.
	Failed bool
	// Summary is what the command did or found, rendered after ok in order.
	Summary []toon.Field
	// List marks a list-shaped result: count and items are rendered, and an
	// empty list is the definitive empty state.
	List bool
	// Items are the rows, each with the command's minimal default fields.
	Items []toon.Object
	// EmptyHelp is the help[] line on how to get data, written first when a
	// list result has no items. Required for a list result.
	EmptyHelp string
	// Help are the next steps (renderSuggestions), after EmptyHelp.
	Help []string
}

// errAXIEmptyWithoutHelp is writeAXIResult's refusal of an empty list with no
// line on how to get data: a bare count: 0 leaves the agent nowhere to go.
var errAXIEmptyWithoutHelp = errors.New("axi result: an empty list needs an EmptyHelp line")

// axiResultKeys are the keys writeAXIResult owns; a Summary field may not reuse
// one, or the document would carry the key twice.
var axiResultKeys = map[string]bool{"ok": true, "count": true, "items": true, "help": true}

// writeAXIResult writes r to w as one TOON document followed by its help[]
// block. Every string is sanitized by the encoder, so a value carrying a newline
// or TOON punctuation stays one quoted value. Nothing is written on error.
func writeAXIResult(w io.Writer, r axiResult) error {
	for _, f := range r.Summary {
		if axiResultKeys[f.Key] {
			return fmt.Errorf("axi result: summary field %q is a key the result owns", f.Key)
		}
	}
	fields := append([]toon.Field{{Key: "ok", Value: !r.Failed}}, r.Summary...)
	help := r.Help
	if r.List {
		fields = append(fields, toon.Field{Key: "count", Value: len(r.Items)})
		if len(r.Items) > 0 {
			fields = append(fields, toon.Field{Key: "items", Value: r.Items})
		} else {
			if strings.TrimSpace(r.EmptyHelp) == "" {
				return errAXIEmptyWithoutHelp
			}
			help = append([]string{r.EmptyHelp}, r.Help...)
		}
	}
	return writeAXIHelp(w, toon.NewObject(fields...), help)
}

// registerSuggestions adds key's next steps to suggestionRegistry, for a file
// other than cli/suggestions.go (whose registry is one map literal). Call it
// from an init(). A key registered twice, or with no steps, is a programming
// error and panics at startup rather than silently dropping one file's steps.
func registerSuggestions(key suggestionKey, steps ...nextStep) {
	if _, dup := suggestionRegistry[key]; dup {
		panic(fmt.Sprintf("registerSuggestions: key %q is already registered", key))
	}
	if len(steps) == 0 {
		panic(fmt.Sprintf("registerSuggestions: key %q has no steps", key))
	}
	suggestionRegistry[key] = steps
}
