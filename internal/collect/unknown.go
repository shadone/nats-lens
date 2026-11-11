package collect

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// Encoded is how nats-server encodes the value the field holds in its
	// place, such as `"undefined"` for a deliver policy, or empty when
	// it refuses to, as it does for most enums.
	Encoded string
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
// plain decode fails, each value that an enum field refuses is taken out
// and the reply is decoded again. An enum field is one of an integer type
// with its own UnmarshalJSON, as nats-server's retention, storage and
// policy types are; a value it refuses may be an unknown name, a number or
// null. The field then holds -1 (the largest value for an unsigned type),
// which no such enum knows. A value that a field of another kind refuses
// is not taken out, so such a reply still does not decode and returns the
// first error. The unknown values are returned in the order of the reply.
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
	var w walker
	w.drop(tree, reflect.TypeOf(target).Elem())
	if len(w.found) == 0 {
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
	// A field sent twice, under keys that differ in case, gives two values
	// at one path in the decoder's map order; their text orders them.
	sort.Slice(w.found, func(i, j int) bool {
		a, b := w.found[i], w.found[j]
		if lessSteps(a.steps, b.steps) || lessSteps(b.steps, a.steps) {
			return lessSteps(a.steps, b.steps)
		}
		return a.value < b.value
	})
	out := make([]UnknownValue, 0, len(w.found))
	for _, f := range w.found {
		u := UnknownValue{Path: pathString(f.steps), Value: f.value}
		if fv := markUnknown(tv, f.steps); fv.IsValid() {
			if enc, err := json.Marshal(fv.Interface()); err == nil {
				u.Encoded = string(enc)
			}
		}
		out = append(out, u)
	}
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

// lessSteps orders two paths as the reply lists them: list positions by
// number, field names and map keys as text.
func lessSteps(a, b []step) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		x, y := a[i], b[i]
		switch {
		case x.kind != y.kind:
			return x.kind < y.kind
		case x.index != y.index:
			return x.index < y.index
		case x.name != y.name:
			return x.name < y.name
		case x.key != y.key:
			return x.key < y.key
		}
	}
	return len(a) < len(b)
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

// walker deletes refused enum values from a value decoded into any. at is
// the path to the node being walked, shared and copied only for a find.
type walker struct {
	at    []step
	found []unknownAt
}

// drop walks node along t, the type it is decoded into, skipping the
// parts whose type holds no enum field.
func (w *walker) drop(node any, t reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if !typeInfo(t).holdsEnum {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		fields := typeInfo(t).fields
		for key, val := range obj {
			f, ok := fields.lookup(key)
			if !ok {
				continue
			}
			w.at = append(w.at, step{kind: reflect.Struct, field: f.index, name: f.name})
			if refused(val, f.typ) {
				raw, _ := json.Marshal(val)
				w.found = append(w.found, unknownAt{steps: append([]step(nil), w.at...), value: string(raw)})
				delete(obj, key)
			} else {
				w.drop(val, f.typ)
			}
			w.at = w.at[:len(w.at)-1]
		}
	case reflect.Slice, reflect.Array:
		list, ok := node.([]any)
		if !ok {
			return
		}
		for i, val := range list {
			w.at = append(w.at, step{kind: reflect.Slice, index: i})
			w.drop(val, t.Elem())
			w.at = w.at[:len(w.at)-1]
		}
	case reflect.Map:
		obj, ok := node.(map[string]any)
		if !ok {
			return
		}
		for key, val := range obj {
			w.at = append(w.at, step{kind: reflect.Map, key: key})
			w.drop(val, t.Elem())
			w.at = w.at[:len(w.at)-1]
		}
	}
}

// isEnum reports whether t is an enum type: an integer type with its own
// UnmarshalJSON.
func isEnum(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.PointerTo(t).Implements(unmarshalerType)
	}
	return false
}

// refused reports whether val is a value that t, an enum type, does not
// accept. A null for a pointer field is accepted: it leaves the pointer nil.
func refused(val any, t reflect.Type) bool {
	pointer := false
	for t.Kind() == reflect.Pointer {
		t, pointer = t.Elem(), true
	}
	if !isEnum(t) || (val == nil && pointer) {
		return false
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return false
	}
	return reflect.New(t).Interface().(json.Unmarshaler).UnmarshalJSON(raw) != nil
}

// markUnknown sets the field at steps, below v, to the value no enum
// knows, and returns it; the zero Value when it is not reached.
func markUnknown(v reflect.Value, steps []step) reflect.Value {
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
		return v
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
		return markUnknown(v, rest)
	case reflect.Slice:
		if s.index < v.Len() {
			return markUnknown(v.Index(s.index), rest)
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return reflect.Value{}
		}
		k := reflect.ValueOf(s.key).Convert(v.Type().Key())
		cur := v.MapIndex(k)
		if !cur.IsValid() {
			return reflect.Value{}
		}
		cp := reflect.New(cur.Type()).Elem()
		cp.Set(cur)
		got := markUnknown(cp, rest)
		v.SetMapIndex(k, cp)
		if got.IsValid() {
			// The map holds a copy; got is the copy's field, equal in value.
			return got
		}
	}
	return reflect.Value{}
}

// HoldsUnknown reports whether v, a decoded value such as a stream
// configuration, holds an enum field that a reply's unknown value was
// taken out of (decodeTolerant).
func HoldsUnknown(v any) bool {
	return holdsUnknown(reflect.ValueOf(v))
}

func holdsUnknown(v reflect.Value) bool {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	t := v.Type()
	if isEnum(t) {
		switch t.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return v.Uint() == math.MaxUint64>>(64-t.Bits())
		default:
			return v.Int() == -1
		}
	}
	if !typeInfo(t).holdsEnum {
		return false
	}
	switch t.Kind() {
	case reflect.Struct:
		for _, f := range typeInfo(t).fields.exact {
			fv, err := v.FieldByIndexErr(f.index)
			if err == nil && holdsUnknown(fv) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if holdsUnknown(v.Index(i)) {
				return true
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if holdsUnknown(it.Value()) {
				return true
			}
		}
	}
	return false
}

// jsonField is a struct field as encoding/json names it.
type jsonField struct {
	name  string
	index []int
	typ   reflect.Type
}

// fieldSet holds a struct's fields by JSON name, and by the name in
// lower case for the match encoding/json makes when the case differs.
type fieldSet struct {
	exact, folded map[string]jsonField
}

func (fs fieldSet) lookup(key string) (jsonField, bool) {
	if f, ok := fs.exact[key]; ok {
		return f, true
	}
	f, ok := fs.folded[strings.ToLower(key)]
	return f, ok
}

// typeFacts is what the walk needs to know of a type, kept per type.
type typeFacts struct {
	fields fieldSet
	// holdsEnum is whether a value of the type can hold an enum field.
	holdsEnum bool
}

var typeCache sync.Map // reflect.Type -> *typeFacts

// typeInfo returns the facts of t, a type that is not a pointer.
func typeInfo(t reflect.Type) *typeFacts {
	if f, ok := typeCache.Load(t); ok {
		return f.(*typeFacts)
	}
	f := &typeFacts{holdsEnum: holdsEnum(t, map[reflect.Type]bool{})}
	if t.Kind() == reflect.Struct {
		fs := fieldSet{exact: map[string]jsonField{}, folded: map[string]jsonField{}}
		for _, jf := range jsonFields(t) {
			fs.exact[jf.name] = jf
			if _, dup := fs.folded[strings.ToLower(jf.name)]; !dup {
				fs.folded[strings.ToLower(jf.name)] = jf
			}
		}
		f.fields = fs
	}
	actual, _ := typeCache.LoadOrStore(t, f)
	return actual.(*typeFacts)
}

// holdsEnum reports whether a value of t can hold an enum field, below
// any depth; visiting guards the types being looked at against cycles.
func holdsEnum(t reflect.Type, visiting map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if isEnum(t) {
		return true
	}
	if visiting[t] {
		return false
	}
	visiting[t] = true
	defer delete(visiting, t)
	switch t.Kind() {
	case reflect.Struct:
		if reflect.PointerTo(t).Implements(unmarshalerType) {
			return false
		}
		for _, f := range jsonFields(t) {
			if holdsEnum(f.typ, visiting) {
				return true
			}
		}
	case reflect.Slice, reflect.Array, reflect.Map:
		if reflect.PointerTo(t).Implements(unmarshalerType) {
			return false
		}
		return holdsEnum(t.Elem(), visiting)
	}
	return false
}

// jsonFields lists the fields encoding/json decodes into for struct type t,
// with the fields of untagged embedded structs promoted; a shallower field
// hides a deeper one of the same name.
func jsonFields(t reflect.Type) []jsonField {
	var out []jsonField
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
