package api

import (
	"context"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// AccountNamer is implemented by a provider that knows the readable names
// of accounts. The handler then fills account_name in every message of a
// response that has both an account and an account_name field (entity
// references, events, connections), so that each RPC need not do it.
type AccountNamer interface {
	AccountNames(ctx context.Context) (map[string]string, error)
}

// nameAccounts sets account_name from names wherever a message in m holds
// an account ID with a known name, leaving a name already set.
func nameAccounts(m protoreflect.Message, names map[string]string) {
	if len(names) == 0 || !m.IsValid() {
		return
	}
	fields := m.Descriptor().Fields()
	acc, name := fields.ByName("account"), fields.ByName("account_name")
	if acc != nil && name != nil && acc.Kind() == protoreflect.StringKind && name.Kind() == protoreflect.StringKind {
		if n := names[m.Get(acc).String()]; n != "" && m.Get(name).String() == "" {
			m.Set(name, protoreflect.ValueOfString(n))
		}
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
			return true
		}
		switch {
		case fd.IsList():
			l := v.List()
			for i := range l.Len() {
				nameAccounts(l.Get(i).Message(), names)
			}
		case fd.IsMap():
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				if fd.MapValue().Kind() == protoreflect.MessageKind {
					nameAccounts(mv.Message(), names)
				}
				return true
			})
		default:
			nameAccounts(v.Message(), names)
		}
		return true
	})
}

// withAccountNames names the accounts in res when the provider can. A
// failed lookup leaves the names unset rather than failing the call.
func (h *Handler) withAccountNames(ctx context.Context, res any) {
	n, ok := h.P.(AccountNamer)
	if !ok {
		return
	}
	msg, ok := res.(proto.Message)
	if !ok {
		return
	}
	names, err := n.AccountNames(ctx)
	if err != nil {
		if h.Log != nil {
			h.Log.Warn("account names unavailable", "err", err)
		}
		return
	}
	nameAccounts(msg.ProtoReflect(), names)
}
