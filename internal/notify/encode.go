package notify

import (
	"bytes"
	"encoding/json"
)

// marshal is json.Marshal without HTML escaping, so that a Slack link's
// angle brackets and a detail's "&" arrive as written.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
