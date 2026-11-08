package investigation

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Entity struct {
	Type     string `json:"type"`
	Key      string `json:"key"`
	Account  string `json:"account,omitempty"`
	Stream   string `json:"stream,omitempty"`
	Consumer string `json:"consumer,omitempty"`
	Server   string `json:"server,omitempty"`
}
type Bounds struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type Availability struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type Evidence struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type SeriesReference struct {
	Label             string  `json:"label"`
	Metric            string  `json:"metric,omitempty"`
	Entity            *Entity `json:"entity,omitempty"`
	UnavailableReason string  `json:"unavailableReason,omitempty"`
}
type Context struct {
	Query      *BoundQuery       `json:"query,omitempty"`
	ChartLabel string            `json:"chartLabel,omitempty"`
	Series     []SeriesReference `json:"series,omitempty"`
	Version    int               `json:"version"`
	Deployment string            `json:"deployment"`
	System     string            `json:"system"`
	Account    string            `json:"account,omitempty"`
	Range      Bounds            `json:"range"`
	SourcePath string            `json:"sourcePath"`
	Filters    map[string]string `json:"filters,omitempty"`
	Entity     *Entity           `json:"entity,omitempty"`
	Metric     string            `json:"metric,omitempty"`
	Point      string            `json:"point,omitempty"`
	Evidence   []Evidence        `json:"evidence,omitempty"`
	Provenance Availability      `json:"provenance"`
	Annotation string            `json:"annotation,omitempty"`
}

const MaxInputBytes = 64 << 10
const MaxRange = 30 * 24 * time.Hour

var metricPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
var filterKeys = strings.Fields("sev state muted check src kind client show type key server account stream consumer id filter q sort dir a b kv ephemeral name tooling process epoch subject cmp servers asset epochA epochB")

func validText(s string, n int) bool {
	if !utf8.ValidString(s) || len(s) > n {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func allowedFilter(k string) bool {
	for _, v := range filterKeys {
		if k == v {
			return true
		}
	}
	return false
}
func origin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("deployment must be a configured HTTP(S) origin without credentials, path or query")
	}
	u.Path = ""
	return u, nil
}
func bounds(b Bounds) (time.Time, time.Time, error) {
	a, err := time.Parse(time.RFC3339Nano, b.Start)
	if err != nil || !strings.HasSuffix(b.Start, "Z") {
		return a, a, errors.New("range start must be UTC RFC3339")
	}
	z, err := time.Parse(time.RFC3339Nano, b.End)
	if err != nil || !strings.HasSuffix(b.End, "Z") || !a.Before(z) || z.Sub(a) > MaxRange {
		return a, z, errors.New("range needs ordered UTC endpoints, at most 30 days apart")
	}
	return a, z, nil
}

// Split route segments before unescaping: a finding ID can itself contain
// account/stream/consumer separators encoded inside the single :id segment.
func pathSegments(p string) []string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "." || decoded == ".." || !validText(decoded, 1024) || strings.Contains(decoded, "\\") {
			return nil
		}
		parts[i] = decoded
	}
	return parts
}
func supportedPath(p string) bool {
	parts := pathSegments(p)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "capacity":
		return len(parts) == 1 || len(parts) == 2 && parts[1] == "storage-change"
	case "", "fleet", "data-history", "replay", "topology", "placement", "connections", "accounts", "subjects", "micro-services", "applications":
		return len(parts) == 1
	case "servers", "findings", "checks":
		return len(parts) <= 2
	case "events":
		return len(parts) == 1
	case "streams", "kv-stores", "object-stores":
		return len(parts) == 1 || len(parts) == 3
	case "consumers":
		return len(parts) == 1 || len(parts) == 4
	}
	return false
}
func validateSource(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || !supportedPath(u.EscapedPath()) || len(raw) > 8192 {
		return errors.New("unsupported source UI path")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid UI query")
	}
	for k, v := range q {
		if (!allowedFilter(k) && k != "system" && k != "range" && k != "from" && k != "to") || len(v) != 1 || !validText(v[0], 1024) {
			return errors.New("unsupported or duplicate UI parameter; remove credentials and unknown parameters")
		}
	}
	return nil
}

// ParseContext treats copied context as untrusted data. ASVS 2.2.1/2.2.2:
// bounds and types are checked here; RPC handlers still validate every query.
func ParseContext(r io.Reader, trusted string) (*Context, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputBytes {
		return nil, errors.New("context exceeds 64 KiB")
	}
	var c Context
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return nil, errors.New("invalid context JSON or unsupported fields; use a compatible client")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("context must contain one JSON value")
	}
	if err = c.Validate(trusted); err != nil {
		return nil, err
	}
	return &c, nil
}
func (c *Context) Validate(trusted string) error {
	t, err := origin(trusted)
	if err != nil {
		return err
	}
	u, err := origin(c.Deployment)
	if err != nil || u.String() != t.String() {
		return errors.New("context deployment does not match the configured trusted profile")
	}
	if c.Version != 1 {
		return errors.New("unsupported context version; use version 1")
	}
	if !validText(c.System, 256) || !validText(c.Account, 256) || (!utf8.ValidString(c.Annotation) || len(c.Annotation) > 4000 || strings.ContainsRune(c.Annotation, 0)) {
		return errors.New("invalid scope or annotation")
	}
	a, z, err := bounds(c.Range)
	if err != nil {
		return err
	}
	if err = validateSource(c.SourcePath); err != nil {
		return err
	}
	if c.Metric != "" && (!metricPattern.MatchString(c.Metric) || len(c.Metric) > 128) {
		return errors.New("invalid metric ID; discover using ListMetrics")
	}
	if c.Point != "" {
		p, err := time.Parse(time.RFC3339Nano, c.Point)
		if err != nil || !strings.HasSuffix(c.Point, "Z") || p.Before(a) || !p.Before(z) {
			return errors.New("selected point must be within the interval")
		}
	}
	if len(c.Filters) > 32 || len(c.Evidence) > 16 {
		return errors.New("too many context filters or evidence references")
	}
	for k, v := range c.Filters {
		if !allowedFilter(k) || !validText(v, 1024) {
			return errors.New("unsupported filter")
		}
	}
	if err := validateEntity(c.Entity); err != nil {
		return err
	}
	if !validText(c.ChartLabel, 1024) || len(c.Series) > 32 {
		return errors.New("invalid chart label or too many series")
	}
	for _, ref := range c.Series {
		if ref.Label == "" || !validText(ref.Label, 1024) || !validText(ref.UnavailableReason, 1000) {
			return errors.New("invalid series label or availability reason")
		}
		if ref.Metric == "" {
			if ref.UnavailableReason == "" {
				return errors.New("unknown series metric needs an unavailable reason")
			}
		} else if !metricPattern.MatchString(ref.Metric) || len(ref.Metric) > 128 {
			return errors.New("invalid series metric ID")
		}
		if err := validateEntity(ref.Entity); err != nil {
			return err
		}
	}

	for _, e := range c.Evidence {
		if (e.Kind != "finding" && e.Kind != "event" && e.Kind != "collection") || e.ID == "" || !validText(e.ID, 256) {
			return errors.New("invalid evidence reference")
		}
	}
	if err := c.validateQuery(); err != nil {
		return err
	}
	if c.Provenance.Status != "unavailable" || !validText(c.Provenance.Reason, 1000) || c.Provenance.Reason == "" {
		return errors.New("this context version cannot pin API collections; provenance must explicitly be unavailable")
	}
	return nil
}
func ContextFromURL(raw, trusted string, now time.Time) (*Context, error) {
	t, err := origin(trusted)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != t.Scheme || u.Host != t.Host {
		return nil, errors.New("page URL must match the configured trusted deployment")
	}
	source := u.RequestURI()
	if err = validateSource(source); err != nil {
		return nil, err
	}
	q := u.Query()
	end := now.UTC()
	start := end.Add(-24 * time.Hour)
	if q.Has("from") || q.Has("to") {
		start, err = time.Parse(time.RFC3339Nano, q.Get("from"))
		if err != nil {
			return nil, errors.New("invalid URL from")
		}
		end, err = time.Parse(time.RFC3339Nano, q.Get("to"))
		if err != nil {
			return nil, errors.New("invalid URL to")
		}
	} else if q.Has("range") {
		var d time.Duration
		switch q.Get("range") {
		case "1h":
			d = time.Hour
		case "6h":
			d = 6 * time.Hour
		case "24h":
			d = 24 * time.Hour
		case "7d":
			d = 7 * 24 * time.Hour
		default:
			return nil, errors.New("unsupported relative range")
		}
		start = end.Add(-d)
	}
	c := &Context{Version: 1, Deployment: t.String(), System: q.Get("system"), Account: q.Get("account"), Range: Bounds{start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)}, SourcePath: u.EscapedPath(), Filters: map[string]string{}, Provenance: Availability{"unavailable", "API does not provide a pinned collection for copied context; follow Data history for collection evidence"}}
	for k, v := range q {
		if allowedFilter(k) {
			c.Filters[k] = v[0]
		}
	}
	if u.Path == "/findings" {
		if c.Filters["state"] == "" {
			c.Filters["state"] = "open"
		}
		if c.Filters["muted"] == "" {
			c.Filters["muted"] = "1"
		}
	}
	if u.Path == "/events" && c.Filters["show"] == "" {
		c.Filters["show"] = "worth"
	}
	c.infer(u.EscapedPath(), q)
	// A compound selected resource names its own account, as in the UI.
	if c.Entity != nil && c.Entity.Account != "" {
		c.Account = c.Entity.Account
	}
	if err = c.Validate(trusted); err != nil {
		return nil, err
	}
	return c, nil
}
func (c *Context) infer(path string, q url.Values) {
	p := pathSegments(path)
	if len(p) == 0 {
		return
	}
	if len(p) == 2 {
		switch p[0] {
		case "servers":
			if p[1] != "slow-consumers" {
				c.Entity = &Entity{Type: "ENTITY_TYPE_SERVER", Key: p[1], Server: p[1]}
			}
		case "findings":
			c.Evidence = []Evidence{{"finding", p[1]}}
		}
	}
	if len(p) == 3 && (p[0] == "streams" || p[0] == "kv-stores" || p[0] == "object-stores") {
		stream := p[2]
		if p[0] == "kv-stores" {
			stream = "KV_" + stream
		}
		if p[0] == "object-stores" {
			stream = "OBJ_" + stream
		}
		c.Entity = &Entity{Type: "ENTITY_TYPE_STREAM", Key: p[1] + "/" + stream, Account: p[1], Stream: stream}
		c.Account = p[1]
	}
	if len(p) == 4 && p[0] == "consumers" {
		c.Entity = &Entity{Type: "ENTITY_TYPE_CONSUMER", Key: strings.Join(p[1:], "/"), Account: p[1], Stream: p[2], Consumer: p[3]}
		c.Account = p[1]
	}
	if c.Entity == nil {
		if key := q.Get("server"); key != "" && (path == "/events" || path == "/connections" || path == "/servers/slow-consumers" || path == "/placement") {
			c.Entity = &Entity{Type: "ENTITY_TYPE_SERVER", Key: key, Server: key}
		}
		if key := q.Get("stream"); key != "" && (path == "/events" || path == "/consumers") {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) == 2 {
				c.Entity = &Entity{Type: "ENTITY_TYPE_STREAM", Key: key, Account: parts[0], Stream: parts[1]}
			}
		}
		if key := q.Get("consumer"); key != "" && path == "/events" {
			parts := strings.SplitN(key, "/", 3)
			if len(parts) == 3 {
				c.Entity = &Entity{Type: "ENTITY_TYPE_CONSUMER", Key: key, Account: parts[0], Stream: parts[1], Consumer: parts[2]}
			}
		}
	}
	if p[0] == "events" && q.Get("id") != "" {
		c.Evidence = []Evidence{{"event", q.Get("id")}}
	}
}
func (c *Context) Link(trusted string) (string, error) {
	if err := c.Validate(trusted); err != nil {
		return "", err
	}
	u, _ := url.Parse(c.SourcePath)
	q := u.Query()
	q.Del("range")
	q.Set("from", c.Range.Start)
	q.Set("to", c.Range.End)
	for k, v := range c.Filters {
		q.Set(k, v)
	}
	if c.System != "" {
		q.Set("system", c.System)
	} else {
		q.Del("system")
	}
	if c.Account != "" {
		q.Set("account", c.Account)
	} else {
		q.Del("account")
	}

	u.RawQuery = q.Encode()
	return strings.TrimSuffix(trusted, "/") + u.String(), nil
}

// A deliberate narrower query is a new selection. Preserve the original point
// only while it remains inside that query; never relax imported validation.
func (c *Context) keepPointWithinRange() {
	if c.Point == "" {
		return
	}
	a, z, err := bounds(c.Range)
	p, parseErr := time.Parse(time.RFC3339Nano, c.Point)
	if err == nil && parseErr == nil && (p.Before(a) || !p.Before(z)) {
		c.Point = ""
	}
}

func validateEntity(e *Entity) error {
	if e != nil {
		switch e.Type {
		case "ENTITY_TYPE_SYSTEM", "ENTITY_TYPE_SERVER", "ENTITY_TYPE_ACCOUNT", "ENTITY_TYPE_STREAM", "ENTITY_TYPE_CONSUMER", "ENTITY_TYPE_CONNECTION", "ENTITY_TYPE_CLIENT":
		default:
			return errors.New("invalid entity type")
		}
		for _, v := range []string{e.Key, e.Account, e.Stream, e.Consumer, e.Server} {
			if !validText(v, 256) {
				return errors.New("invalid entity ID")
			}
		}
	}
	return nil
}
