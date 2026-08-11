package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// agentBoolFields are the bool-typed fields in AgentConfig whose yaml type
// errors need a field-named hint (the strict decoder message omits them).
var agentBoolFields = []string{"tools", "supports_function_calling"}

// amendWithAgentFieldHints checks whether err is a yaml type-mismatch on a
// known bool agent field. When detected, data is loosely re-parsed via a
// yaml.Node walk to identify the offending agent and field, returning a
// field-named error. Falls back to the original error when the mismatch can't
// be isolated (non-bool error, parse failure, or no match found).
func amendWithAgentFieldHints(err error, data []byte) error {
	if !strings.Contains(err.Error(), "cannot unmarshal") {
		return err
	}
	if hint := findAgentBoolMismatch(data); hint != nil {
		return hint
	}
	return err
}

// findAgentBoolMismatch loosely decodes data into a yaml.Node tree and returns
// the first agent field from agentBoolFields that carries a non-bool YAML tag.
func findAgentBoolMismatch(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "agents" {
			continue
		}
		agentsNode := root.Content[i+1]
		if agentsNode.Kind != yaml.MappingNode {
			return nil
		}
		for j := 0; j+1 < len(agentsNode.Content); j += 2 {
			agentName := agentsNode.Content[j].Value
			agentNode := agentsNode.Content[j+1]
			if agentNode.Kind != yaml.MappingNode {
				continue
			}
			for k := 0; k+1 < len(agentNode.Content); k += 2 {
				fieldKey := agentNode.Content[k].Value
				fieldVal := agentNode.Content[k+1]
				for _, bf := range agentBoolFields {
					if fieldKey == bf && fieldVal.Tag != "!!bool" {
						return fmt.Errorf("agent '%s': %s must be a boolean (true/false), got %s",
							agentName, fieldKey, fieldVal.Tag)
					}
				}
			}
		}
		return nil
	}
	return nil
}

// rejectFloatInIntFields reports the first YAML float that landed in an
// integer-typed field of dst.
//
// gopkg.in/yaml.v3 does not treat this as a type error: it TRUNCATES, so
// `max_context_lines: 1200.5` loaded as 1200, `max_findings: 7.9` as 7, and
// `context_window_tokens: 1.28e6` as 1280000 — an operator typo accepted with
// the fraction silently discarded, which is exactly the class of error the
// strict decoder exists to make loud.
//
// The check is driven by dst's own field types via reflection rather than by a
// list of key names, so it covers every int field the registry, project config,
// settings, sandbox, and trust-file schemas define — including ones added later,
// which a hand-maintained list would silently miss. It runs AFTER a successful
// decode: the values are already in place, and this pass only re-reads the
// source to decide whether any of them arrived lossily.
//
// A genuinely float-typed field (temperature) is untouched, because the field's
// declared kind — not the value's shape — is what selects the rule.
func rejectFloatInIntFields(data []byte, dst any) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil ||
		doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil // unparseable here means the decode above already reported it
	}
	return walkIntFields(doc.Content[0], reflect.TypeOf(dst))
}

// walkIntFields descends node and t in parallel, returning the first float
// scalar sitting in an integer-typed field. Types it cannot map (an `any` field,
// a custom unmarshaler's opaque shape) simply end that branch of the walk —
// missing a case must under-report, never reject a valid document.
func walkIntFields(node *yaml.Node, t reflect.Type) error {
	if node == nil || t == nil {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if node.Kind == yaml.AliasNode {
		return walkIntFields(node.Alias, t)
	}

	switch t.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		fields := yamlFieldTypes(t)
		for i := 0; i+1 < len(node.Content); i += 2 {
			ft, ok := fields[node.Content[i].Value]
			if !ok {
				continue
			}
			if err := checkIntScalar(node.Content[i].Value, node.Content[i+1], ft); err != nil {
				return err
			}
			if err := walkIntFields(node.Content[i+1], ft); err != nil {
				return err
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		elem := t.Elem()
		for i := 0; i+1 < len(node.Content); i += 2 {
			if err := checkIntScalar(node.Content[i].Value, node.Content[i+1], elem); err != nil {
				return err
			}
			if err := walkIntFields(node.Content[i+1], elem); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return nil
		}
		elem := t.Elem()
		for _, item := range node.Content {
			if err := checkIntScalar("", item, elem); err != nil {
				return err
			}
			if err := walkIntFields(item, elem); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkIntScalar rejects a !!float scalar bound for an integer-typed target.
// key names the offending YAML key ("" for a sequence element, where the
// enclosing field name is not available at this level).
func checkIntScalar(key string, val *yaml.Node, t reflect.Type) error {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || val == nil || val.Kind != yaml.ScalarNode || val.Tag != "!!float" {
		return nil
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if key == "" {
			return fmt.Errorf("value %s must be an integer, not a decimal", val.Value)
		}
		return fmt.Errorf("%s: %s must be an integer, not a decimal — yaml would otherwise truncate it", key, val.Value)
	}
	return nil
}

// yamlFieldTypes maps a struct's YAML keys to their field types, flattening
// `,inline` embedded structs so an inlined field is reachable at the parent
// level (the shape AgentConfig has inside communityPersonaFile). A field tagged
// `-` is skipped; an untagged field follows yaml.v3's lowercased-name default.
func yamlFieldTypes(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if strings.Contains(opts, "inline") {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range yamlFieldTypes(ft) {
					out[k] = v
				}
			}
			continue
		}
		if name == "" {
			if !f.IsExported() {
				continue
			}
			name = strings.ToLower(f.Name)
		}
		out[name] = f.Type
	}
	return out
}

// decodeStrictYAML decodes data into dst with KnownFields enabled and rejects
// any second YAML document carrying content. A trailing document separator
// (`---` followed by nothing) is tolerated: yaml.v3 surfaces it as a null
// document, not as EOF.
//
// Returns errEmptyDocument when data holds no YAML content at all
// (whitespace or comments only) so callers can issue their own message.
var errEmptyDocument = errors.New("yaml: no content")

func decodeStrictYAML(data []byte, dst any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errEmptyDocument
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errEmptyDocument
		}
		return err
	}

	if err := rejectFloatInIntFields(data, dst); err != nil {
		return err
	}

	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return err
	case extra.Kind == 0 || extra.IsZero():
		return nil // trailing `---` with no content
	case extra.Kind == yaml.DocumentNode && len(extra.Content) == 1 &&
		extra.Content[0].Tag == "!!null":
		return nil // explicit null second document
	default:
		return fmt.Errorf("unexpected second YAML document")
	}
}
