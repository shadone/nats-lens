package investigation

import (
	"errors"
	"github.com/shadone/nats-lens/internal/eventselection"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Require positive acknowledgement of additive exact-selection fields. A prior
// server can legally ignore unknown protobuf fields; that is not a scoped read.
func checkEventSelectionReply(method string, in, out protoreflect.Message) error {
	if method == "ListFindingEpisodes" {
		requested := in.Descriptor().Fields().ByJSONName("readEpoch")
		if requested == nil || !in.Has(requested) {
			return nil
		}
		boundary := in.Get(requested).Int()
		returned := out.Descriptor().Fields().ByJSONName("readEpoch")
		if returned == nil || out.Get(returned).Int() != boundary {
			return errors.New("finding history boundary was not honored by this server")
		}
		if boundary == 0 {
			count := out.Descriptor().Fields().ByJSONName("totalCount")
			rows := out.Descriptor().Fields().ByJSONName("episodes")
			if count != nil && out.Get(count).Int() != 0 || rows != nil && out.Get(rows).List().Len() != 0 {
				return errors.New("finding history boundary was not honored by this server")
			}
		}
		return nil
	}

	if method == "ListEventSelection" {
		mismatch := func() error { return errors.New("event selection page was not honored by this server") }
		values := in.Get(in.Descriptor().Fields().ByJSONName("ids")).List()
		ids := make([]string, values.Len())
		for i := range ids {
			ids[i] = values.Get(i).String()
		}
		integer := func(m protoreflect.Message, name string) int64 {
			return m.Get(m.Descriptor().Fields().ByJSONName(name)).Int()
		}
		boundaryField := in.Descriptor().Fields().ByJSONName("eventReadId")
		var boundary *int64
		if in.Has(boundaryField) {
			n := in.Get(boundaryField).Int()
			boundary = &n
		}
		page, err := eventselection.PageFor(ids, boundary, int32(integer(in, "offset")), int32(integer(in, "pageSize")))
		if err != nil {
			return mismatch()
		}
		for name, wanted := range map[string]int64{"offset": int64(page.Offset), "pageSize": int64(page.Size), "nextOffset": int64(page.Next), "selectedTotalCount": int64(page.Total)} {
			if integer(out, name) != wanted {
				return mismatch()
			}
		}
		echoed := out.Get(out.Descriptor().Fields().ByJSONName("selectedIds")).List()
		if echoed.Len() != len(ids) {
			return mismatch()
		}
		for i, id := range ids {
			if echoed.Get(i).String() != id {
				return mismatch()
			}
		}
		outBoundary := out.Descriptor().Fields().ByJSONName("eventReadId")
		if boundary != nil && (!out.Has(outBoundary) || out.Get(outBoundary).Int() != *boundary) {
			return mismatch()
		}
		rows := out.Get(out.Descriptor().Fields().ByJSONName("events")).List()
		if integer(out, "totalCount") != int64(rows.Len()) {
			return mismatch()
		}
		allowed := map[string]bool{}
		for _, id := range page.IDs {
			allowed[id] = true
		}
		seen := map[string]bool{}
		for i := 0; i < rows.Len(); i++ {
			row := rows.Get(i).Message()
			id := row.Get(row.Descriptor().Fields().ByJSONName("id")).String()
			if !allowed[id] || seen[id] {
				return mismatch()
			}
			seen[id] = true
		}
		return nil
	}
	if method != "ListEvents" && method != "GetEvent" && method != "GetStatusTimeline" && method != "GetEventBatch" {
		return nil
	}
	mismatch := func() error {
		return errors.New("event selection/boundary was not honored by this server; update the compatible server/client or use an explicitly broader read")
	}
	requestedBoundary := in.Descriptor().Fields().ByJSONName("eventReadId")
	boundarySet := requestedBoundary != nil && in.Has(requestedBoundary)
	if boundarySet {
		acknowledged := out.Descriptor().Fields().ByJSONName("eventReadId")
		if acknowledged == nil || !out.Has(acknowledged) || out.Get(acknowledged).Int() != in.Get(requestedBoundary).Int() {
			return mismatch()
		}
	}
	if method == "GetEvent" && boundarySet {
		event := out.Descriptor().Fields().ByJSONName("event")
		if event == nil || !out.Has(event) || in.Get(requestedBoundary).Int() == 0 {
			return mismatch()
		}
		actual := out.Get(event).Message()
		id := actual.Descriptor().Fields().ByJSONName("id")
		wanted := in.Descriptor().Fields().ByJSONName("id")
		if id == nil || wanted == nil || actual.Get(id).String() != in.Get(wanted).String() {
			return mismatch()
		}
	}
	if method == "ListEvents" || method == "GetEventBatch" {
		field := "selectedIds"
		if method == "GetEventBatch" {
			field = "ids"
		}
		selected := in.Descriptor().Fields().ByJSONName(field)
		if selected == nil || in.Get(selected).List().Len() == 0 {
			return nil
		}
		values := in.Get(selected).List()
		echo := out.Descriptor().Fields().ByJSONName("selectedIds")
		if echo == nil || out.Get(echo).List().Len() != values.Len() {
			return mismatch()
		}
		ids := map[string]bool{}
		for i := 0; i < values.Len(); i++ {
			v := values.Get(i).String()
			if out.Get(echo).List().Get(i).String() != v {
				return mismatch()
			}
			ids[v] = true
		}
		count := out.Descriptor().Fields().ByJSONName("totalCount")
		if count != nil && (out.Get(count).Int() < 0 || out.Get(count).Int() > int64(len(ids))) {
			return mismatch()
		}
		events := out.Descriptor().Fields().ByJSONName("events")
		if method == "GetEventBatch" && count != nil && events != nil && out.Get(count).Int() != int64(out.Get(events).List().Len()) {
			return mismatch()
		}
		if events != nil {
			rows := out.Get(events).List()
			seen := map[string]bool{}
			for i := 0; i < rows.Len(); i++ {
				row := rows.Get(i).Message()
				id := row.Descriptor().Fields().ByJSONName("id")
				if id == nil {
					return mismatch()
				}
				v := row.Get(id).String()
				if !ids[v] || seen[v] {
					return mismatch()
				}
				seen[v] = true
			}
		}
	}
	return nil
}
