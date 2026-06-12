package registry

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

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
