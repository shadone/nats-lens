package investigation

import (
	"encoding/json"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"net/url"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

type EvidenceLink struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	URL  string `json:"url"`
}

func evidenceLinks(method string, response json.RawMessage, c *Context, trusted, source string) []EvidenceLink {
	out := []EvidenceLink{{Kind: "source", URL: source}}
	seen := map[string]bool{source: true}
	add := func(kind, id, path string) {
		copy := *c
		copy.SourcePath = path
		copy.Filters = nil
		link, err := copy.Link(trusted)
		if err == nil && !seen[link] && len(out) < 100 {
			out = append(out, EvidenceLink{kind, id, link})
			seen[link] = true
		}
	}
	add("coverage", "", "/data-history")
	msg := dynamicpb.NewMessage(Method(method).Output())
	if protojson.Unmarshal(response, msg) != nil {
		return out
	}
	var walk func(protoreflect.Message)
	walk = func(m protoreflect.Message) {
		text := func(name protoreflect.Name) string {
			f := m.Descriptor().Fields().ByName(name)
			if f == nil {
				return ""
			}
			return m.Get(f).String()
		}
		switch m.Descriptor().FullName() {
		case "natslens.v1.Finding":
			if id := text("id"); id != "" {
				add("finding", id, "/findings/"+url.PathEscape(id))
			}
		case "natslens.v1.Event", "natslens.v1.EventRef":
			if id := text("id"); id != "" {
				add("event", id, "/events?id="+url.QueryEscape(id))
			}
		case "natslens.v1.EntityRef":
			kind := v1.EntityType(m.Get(m.Descriptor().Fields().ByName("type")).Enum())
			key := text("key")
			path := ""
			// Typed identity follows the generated contract, never a display label.
			switch kind {
			case v1.EntityType_ENTITY_TYPE_SERVER:
				path = "/servers/" + url.PathEscape(key)
			case v1.EntityType_ENTITY_TYPE_STREAM:
				path = "/streams/" + url.PathEscape(text("account")) + "/" + url.PathEscape(text("stream"))
			case v1.EntityType_ENTITY_TYPE_CONSUMER:
				path = "/consumers/" + url.PathEscape(text("account")) + "/" + url.PathEscape(text("stream")) + "/" + url.PathEscape(text("consumer"))
			}
			if path != "" {
				add("entity", key, path)
			}
		}
		m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if f.Message() != nil && !f.IsMap() {
				if f.IsList() {
					l := v.List()
					for i := 0; i < l.Len() && len(out) < 100; i++ {
						walk(l.Get(i).Message())
					}
				} else {
					walk(v.Message())
				}
			}
			return len(out) < 100
		})
	}
	walk(msg.ProtoReflect())
	// Links have an explicit output cap independent of the RPC page size.
	if len(out) == 100 {
		out = append(out, EvidenceLink{Kind: "links_truncated", URL: strings.TrimSuffix(trusted, "/") + "/data-history"})
	}
	return out
}
