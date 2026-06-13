package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
