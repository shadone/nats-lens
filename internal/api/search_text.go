package api

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// searchText trims the text filter of a list request and rejects one
// longer than MaxNameLength characters. An empty text filters nothing.
func searchText(s *string) error {
	*s = strings.TrimSpace(*s)
	if utf8.RuneCountInString(*s) > MaxNameLength {
		return invalid("text is longer than %d characters", MaxNameLength)
	}
	return nil
}

// ContainsText reports whether any of the fields contains text, ignoring
// case. An empty text matches everything.
func ContainsText(text string, fields ...string) bool {
	if text == "" {
		return true
	}
	t := strings.ToLower(text)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), t) {
			return true
		}
	}
	return false
}

// FindingHasText is the text filter of ListFindings: the check code, the
// check name or a part of the entity contains the text.
func FindingHasText(f *v1.Finding, text string) bool {
	e := f.GetEntity()
	return ContainsText(text, f.GetCheckCode(), f.GetCheckName(), e.GetKey(), e.GetName(), e.GetAccount(), e.GetAccountName(),
		e.GetStream(), e.GetConsumer(), e.GetServer())
}

// EventHasText is the text filter of ListEvents: the kind, the reason, the
// server, account (its ID or readable name), stream, consumer or client
// name, a Kubernetes event's object (namespace/name, as its summary names
// it), a finding event's finding ID or the ID of a finding the event raised
// contains the text. The event carries no object, so the caller passes it.
func EventHasText(e *v1.Event, text, object string) bool {
	if ContainsText(text, e.GetKind(), e.GetReason(), e.GetServer(), e.GetAccount(), e.GetAccountName(), e.GetStream(),
		e.GetConsumer(), e.GetClientName(), object, e.GetTransition().GetFindingId()) {
		return true
	}
	for _, f := range e.GetRaised() {
		if ContainsText(text, f.GetId()) {
			return true
		}
	}
	return false
}

// KubernetesObject is the object a Kubernetes event's payload names, for
// the text filter: a Kubernetes Event's object as namespace/name, else a
// pod watch event's namespace/pod, else a node watch event's node. A part
// that is empty is left out; a payload that names none gives "".
func KubernetesObject(raw string) string {
	var p struct {
		Object    json.RawMessage `json:"object"`
		Namespace string          `json:"namespace"`
		Pod       string          `json:"pod"`
		Node      string          `json:"node"`
	}
	if json.Unmarshal([]byte(raw), &p) != nil {
		return ""
	}
	var o struct{ Namespace, Name string }
	if len(p.Object) > 0 && p.Object[0] == '{' && json.Unmarshal(p.Object, &o) == nil {
		return joinNonEmpty(o.Namespace, o.Name)
	}
	if p.Pod != "" {
		return joinNonEmpty(p.Namespace, p.Pod)
	}
	return p.Node
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "/")
}
