package investigation

import (
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// BoundQuery preserves a prior read request independently of the UI's lossy
// filter vocabulary. It is data, not permission, and only applies to this RPC.
// Descriptors, the read allowlist, request budgets and handlers remain the
// same boundaries used for an explicit request file.
type BoundQuery struct {
	Method  string          `json:"method"`
	Request json.RawMessage `json:"request"`
}

func (c *Context) validateQuery() error {
	if c.Query == nil {
		return nil
	}
	q := c.Query
	if Method(q.Method) == nil || len(q.Request) == 0 || len(q.Request) > MaxInputBytes || string(q.Request) == "null" {
		return errors.New("unsupported bound read request")
	}
	msg := dynamicpb.NewMessage(Method(q.Method).Input())
	if err := protojson.Unmarshal(q.Request, msg); err != nil {
		return errors.New("invalid bound read fields")
	}
	if f := msg.Descriptor().Fields().ByJSONName("system"); f != nil && msg.Get(f).String() != c.System {
		return errors.New("bound read system differs from context")
	}
	if f := msg.Descriptor().Fields().ByJSONName("range"); f != nil {
		if !msg.Has(f) {
			return errors.New("bound read requires frozen range")
		}
		raw, _ := protojson.Marshal(msg.Get(f).Message().Interface())
		var r Bounds
		_ = json.Unmarshal(raw, &r)
		a, z, err := bounds(r)
		ca, cz, ce := bounds(c.Range)
		if err != nil || ce != nil || !a.Equal(ca) || !z.Equal(cz) {
			return errors.New("bound read range differs from context")
		}
	}
	if q.Method == "GetReplay" || q.Method == "GetStorageChange" {
		var endpoints struct {
			A string `json:"a"`
			B string `json:"b"`
		}
		_ = json.Unmarshal(q.Request, &endpoints)
		a, z, err := bounds(Bounds{endpoints.A, endpoints.B})
		ca, cz, ce := bounds(c.Range)
		if err != nil || ce != nil || !a.Equal(ca) || !z.Equal(cz) {
			return errors.New("bound Replay endpoints differ from context range")
		}
	}
	base := *c
	base.Query = nil
	base.Filters = nil
	base.Entity = nil
	base.Series = nil
	base.Evidence = nil
	base.Metric = ""
	if _, _, err := Prepare(q.Method, &base, q.Request); err != nil {
		return errors.New("invalid bound read fields or request budget")
	}
	return nil
}

// Flags deliberately change a selection. Rebase common scope fields before
// validation; budgets/tokens and all other explicit API fields remain exact.
func (c *Context) rebaseQuery(systemChanged, rangeChanged, accountChanged, metricChanged bool) error {
	if c.Query == nil {
		return nil
	}
	md := Method(c.Query.Method)
	if md == nil {
		return errors.New("unsupported bound read request")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(c.Query.Request, &fields) != nil || fields == nil {
		return errors.New("invalid bound read request")
	}
	put := func(k string, v any) {
		if md.Input().Fields().ByJSONName(k) != nil {
			fields[k], _ = json.Marshal(v)
		}
	}
	if systemChanged {
		put("system", c.System)
	}
	if rangeChanged {
		put("range", c.Range)
		if c.Query.Method == "GetReplay" || c.Query.Method == "GetStorageChange" {
			put("a", c.Range.Start)
			put("b", c.Range.End)
		}
	}
	if accountChanged {
		put("account", c.Account)
	}
	if metricChanged {
		put("metric", c.Metric)
	}
	b, _ := json.Marshal(fields)
	c.Query = &BoundQuery{Method: c.Query.Method, Request: b}
	return nil
}
