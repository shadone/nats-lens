package collect

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// UnknownValue is a value a reply sent for a field whose type in this
// nats-server version accepts only the values it knows, such as a stream's
// retention or a consumer's ack policy, and which it did not accept.
type UnknownValue struct {
	// Path locates the field in the reply's data, for example
	// "account_details[0].stream_detail[2].config.retention".
	Path string
	// Value is the JSON the reply sent, for example `"future"`.
	Value string
}

// Field is the path without list positions, the same for every stream or
// consumer: "account_details[].stream_detail[].config.retention".
func (u UnknownValue) Field() string {
	var b strings.Builder
	skip := false
	for _, r := range u.Path {
		switch {
		case r == '[':
			skip = true
			b.WriteString("[]")
		case r == ']':
			skip = false
		case !skip:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var unmarshalerType = reflect.TypeFor[json.Unmarshaler]()

// decodeTolerant decodes b into target, which must be a pointer. When the
// plain decode fails, each value that a field of a nats-server enum type
// (any non-struct type with its own UnmarshalJSON) refuses is taken out
// and the reply is decoded again; the field then holds -1 (the largest
// value for an unsigned type), which no such enum knows, so it encodes as
// an error and never as a known value. Fields of other kinds are left at
// their zero value. A reply that still does not decode returns the first
// error.
func decodeTolerant(b []byte, target any) ([]UnknownValue, error) {
	err := json.Unmarshal(b, target)
	if err == nil {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var tree any
	if dec.Decode(&tree) != nil {
		return nil, err
	}
	var found []unknownAt
	dropUnknown(tree, reflect.TypeOf(target).Elem(), nil, &found)
	if len(found) == 0 {
		return nil, err
	}
	cleaned, merr := json.Marshal(tree)
	if merr != nil {
		return nil, err
	}
	tv := reflect.ValueOf(target).Elem()
	tv.SetZero()
	if json.Unmarshal(cleaned, target) != nil {
		tv.SetZero()
		return nil, err
	}
	out := make([]UnknownValue, 0, len(found))
	for _, f := range found {
		markUnknown(tv, f.steps)
		out = append(out, UnknownValue{Path: pathString(f.steps), Value: f.value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// step is one move from a value to a part of it: a struct field (index
// and its JSON name), a list position, or a map key.
type step struct {
	field []int
	name  string
	index int
	key   string
	kind  reflect.Kind
}

type unknownAt struct {
	steps []step
	value string
}

func pathString(steps []step) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.kind {
		case reflect.Struct:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.name)
		case reflect.Slice:
			b.WriteString("[" + strconv.Itoa(s.index) + "]")
		case reflect.Map:
			b.WriteString("[" + strconv.Quote(s.key) + "]")
		}
	}
	return b.String()
}

// dropUnknown walks node, a value decoded into any, along t, the type it
// is decoded into, and deletes from its object each value an enum field
// refuses, recording where it was.
func dropUnknown(node any, t reflect.Type, at []step, found *[]unknownAt) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := node.(map[string]any)
		if !ok || reflect.PointerTo(t).Implements(unmarshalerType) {
			return
		}
		fields := jsonFields(t)
		for key, val := range obj {
			f, ok := fields.lookup(key)
			if !ok {
				continue
			}
			here := append(append([]step{}, at...), step{kind: reflect.Struct, field: f.index, name: f.name})
			if refused(val, f.typ) {
				raw, _ := json.Marshal(val)
				*found = append(*found, unknownAt{steps: here, value: string(raw)})
				delete(obj, key)
				continue
			}
			dropUnknown(val, f.typ, here, found)
		}
	case reflect.Slice, reflect.Array:
		list, ok := node.([]any)
		if !ok {
			return
		}
		for i, val := range list {
			dropUnknown(val, t.Elem(), append(append([]step{}, at...), step{kind: reflect.Slice, index: i}), found)
		}
	case reflect.Map:
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		for key, val := range obj {
			dropUnknown(val, t.Elem(), append(append([]step{}, at...), step{kind: reflect.Map, key: key}), found)
		}
	}
}

// refused reports whether val is a value that t, an enum type with its
// own UnmarshalJSON, does not accept. Other types are never refused here.
func refused(val any, t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if val == nil || t.Kind() == reflect.Struct || !reflect.PointerTo(t).Implements(unmarshalerType) {
		return false
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return false
	}
	return reflect.New(t).Interface().(json.Unmarshaler).UnmarshalJSON(raw) != nil
}

// markUnknown sets the field at steps, below v, to the value no enum
// knows. A field of a kind other than an integer keeps its zero value.
func markUnknown(v reflect.Value, steps []step) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if len(steps) == 0 {
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v.SetInt(-1)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			v.SetUint(math.MaxUint64 >> (64 - v.Type().Bits()))
		}
		return
	}
	s, rest := steps[0], steps[1:]
	switch s.kind {
	case reflect.Struct:
		for _, i := range s.field {
			for v.Kind() == reflect.Pointer {
				if v.IsNil() {
					v.Set(reflect.New(v.Type().Elem()))
				}
				v = v.Elem()
			}
			v = v.Field(i)
		}
		markUnknown(v, rest)
	case reflect.Slice:
		if s.index < v.Len() {
			markUnknown(v.Index(s.index), rest)
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}
		k := reflect.ValueOf(s.key).Convert(v.Type().Key())
		cur := v.MapIndex(k)
		if !cur.IsValid() {
			return
		}
		cp := reflect.New(cur.Type()).Elem()
		cp.Set(cur)
		markUnknown(cp, rest)
		v.SetMapIndex(k, cp)
	}
}

// jsonField is a struct field as encoding/json names it.
type jsonField struct {
	name  string
	index []int
	typ   reflect.Type
}

type fieldSet []jsonField

// lookup finds the field a JSON key decodes into: an exact name first,
// then one that differs only in case, as encoding/json matches.
func (fs fieldSet) lookup(key string) (jsonField, bool) {
	for _, f := range fs {
		if f.name == key {
			return f, true
		}
	}
	for _, f := range fs {
		if strings.EqualFold(f.name, key) {
			return f, true
		}
	}
	return jsonField{}, false
}

// jsonFields lists the fields encoding/json decodes into for struct type t,
// with the fields of untagged embedded structs promoted; a shallower field
// hides a deeper one of the same name.
func jsonFields(t reflect.Type) fieldSet {
	var out fieldSet
	seen := map[string]bool{}
	type level struct {
		t     reflect.Type
		index []int
	}
	current := []level{{t: t}}
	visited := map[reflect.Type]bool{}
	for len(current) > 0 {
		var next []level
		names := map[string]bool{}
		for _, l := range current {
			if visited[l.t] {
				continue
			}
			visited[l.t] = true
			for i := 0; i < l.t.NumField(); i++ {
				sf := l.t.Field(i)
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name, _, _ := strings.Cut(tag, ",")
				index := append(append([]int{}, l.index...), i)
				ft := sf.Type
				if sf.Anonymous && name == "" {
					for ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					if ft.Kind() == reflect.Struct {
						next = append(next, level{t: ft, index: index})
						continue
					}
				}
				if !sf.IsExported() {
					continue
				}
				if name == "" {
					name = sf.Name
				}
				if seen[name] {
					continue
				}
				names[name] = true
				out = append(out, jsonField{name: name, index: index, typ: sf.Type})
			}
		}
		for n := range names {
			seen[n] = true
		}
		current = next
	}
	return out
}

// unknownKey is what logUnknownValues logs once.
type unknownKey struct {
	server, endpoint, field, value string
}

// logUnknownValues logs each unknown value a server sent, once per server,
// endpoint, field and value in the life of the collector rather than every
// epoch, with the first place it was seen and how many places held it.
func (c *Collector) logUnknownValues(snap *Snapshot) {
	for _, v := range snap.SortedViews() {
		for _, e := range sortedEndpoints(v.Status) {
			groups := map[unknownKey][]UnknownValue{}
			var order []unknownKey
			for _, u := range v.Status[e].Unknown {
				k := unknownKey{server: v.Info.Name, endpoint: string(e), field: u.Field(), value: u.Value}
				if c.unknownLogged[k] {
					continue
				}
				if _, ok := groups[k]; !ok {
					order = append(order, k)
				}
				groups[k] = append(groups[k], u)
			}
			for _, k := range order {
				if c.unknownLogged == nil {
					c.unknownLogged = map[unknownKey]bool{}
				}
				c.unknownLogged[k] = true
				c.log.Warn("reply holds a value this nats-server version does not know; decoded without it, logged once per server, field and value",
					"server", k.server, "endpoint", k.endpoint, "field", k.field, "value", k.value,
					"first", groups[k][0].Path, "places", len(groups[k]))
			}
		}
	}
}

func sortedEndpoints(m map[Endpoint]EndpointStatus) []Endpoint {
	out := make([]Endpoint, 0, len(m))
	for e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
