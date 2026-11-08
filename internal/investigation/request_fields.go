package investigation

import (
	"bytes"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/reflect/protoreflect"
	"io"
)

// Canonicalize only descriptor field names. Values, including false, empty,
// null and identity whitespace, are untouched. Reject duplicate spellings
// before merging so neither JSON nor Go map ordering chooses an override.
func requestFields(method string, raw []byte) (map[string]json.RawMessage, error) {
	md := Method(method)
	if md == nil {
		return nil, errors.New("unsupported read method")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("request must be one JSON object")
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, errors.New("invalid request object")
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid request field")
		}
		f := md.Input().Fields().ByJSONName(name)
		if f == nil {
			f = md.Input().Fields().ByName(protoreflect.Name(name))
		}
		if f == nil {
			return nil, errors.New("unsupported request field; use describe METHOD")
		}
		if _, exists := out[f.JSONName()]; exists {
			return nil, errors.New("duplicate request field; use one descriptor spelling")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, errors.New("invalid request value")
		}
		out[f.JSONName()] = value
	}
	if _, err = d.Token(); err != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("request must be one JSON object")
	}
	return out, nil
}
