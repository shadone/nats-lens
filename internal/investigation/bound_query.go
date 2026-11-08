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
	base := *c
	base.Query = nil
	base.Filters = nil
	base.Entity = nil
	base.Series = nil
	base.Evidence = nil
	base.Metric = ""
	b, _, err := Prepare(q.Method, &base, q.Request)
	if err != nil {
		return errors.New("invalid bound read fields or request budget")
	}
	msg := dynamicpb.NewMessage(Method(q.Method).Input())
	if err = protojson.Unmarshal(b, msg); err != nil {
		return errors.New("invalid bound read request")
	}
	// A bound request must name the same system the connection handshake uses.
	if f := msg.Descriptor().Fields().ByJSONName("system"); f != nil && msg.Get(f).String() != c.System {
		return errors.New("bound read system differs from context")
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
		if c.Query.Method == "GetReplay" {
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
