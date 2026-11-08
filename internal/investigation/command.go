package investigation

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

const Help = `natslens investigate methods
natslens investigate describe METHOD
natslens investigate context -profiles FILE -profile NAME -url URL
natslens investigate query METHOD -profiles FILE -profile NAME [-context FILE | -url URL]
  [-request FILE] [-system ID] [-account ID] [-from UTC -to UTC] [-metric ID]
  [-page-size N] [-page-token TOKEN] [-max-points N] [-timeout 45s]

Requests use protobuf JSON; -request - or -context - reads stdin (choose one).
One query sends at most two RPCs (connection info, then the selected read).
No automatic pagination, credential discovery, SQL, uploads or NATS connections.
Use ListSystems, ListMetrics and ListCheckDefinitions to discover live facts.
Use GetDataHistory to inspect coverage, partial collections and retention.
The methods list is the local client contract, not a claim about server support.
`

func writeJSON(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func readInput(name string, stdin io.Reader) ([]byte, error) {
	var r io.Reader = stdin
	if name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return nil, errors.New("cannot open input file")
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, MaxInputBytes+1))
	if err != nil || len(b) > MaxInputBytes {
		return nil, errors.New("input unavailable or exceeds 64 KiB")
	}
	return b, nil
}

func Run(ctx context.Context, args []string, stdin io.Reader, out, errout io.Writer) (err error) {
	defer func() {
		if err != nil && !errors.Is(err, flag.ErrHelp) {
			_ = writeJSON(out, map[string]any{"version": 1, "error": map[string]string{"code": "investigation_failed", "message": err.Error(), "nextStep": "Check help, configured access, method schema and Data history; do not retry by bypassing the proxy."}})
		}
	}()
	if len(args) == 0 {
		fmt.Fprint(errout, Help)
		return flag.ErrHelp
	}
	if args[0] == "methods" {
		if len(args) != 1 {
			return errors.New("methods takes no arguments")
		}
		return writeJSON(out, map[string]any{"version": 1, "methods": Methods(), "serverSupport": "unverified; query checks selected method", "sourceKinds": []string{"remote"}})
	}
	if args[0] == "describe" {
		if len(args) != 2 || Method(args[1]) == nil {
			return errors.New("describe requires a supported read METHOD")
		}
		m := Method(args[1])
		return writeJSON(out, map[string]any{"version": 1, "method": args[1], "request": schema(m.Input(), 0), "response": schema(m.Output(), 0)})
	}
	action := args[0]
	args = args[1:]
	method := ""
	if action == "query" {
		if len(args) == 0 || Method(args[0]) == nil {
			return errors.New("query requires a supported read METHOD; run investigate methods")
		}
		method, args = args[0], args[1:]
	} else if action != "context" {
		return errors.New("unknown investigation command; use methods, describe, context or query")
	}
	fs := flag.NewFlagSet("natslens investigate "+action, flag.ContinueOnError)
	fs.SetOutput(errout)
	fs.Usage = func() { fmt.Fprint(errout, Help) }
	profiles := fs.String("profiles", "", "trusted profiles JSON file")
	name := fs.String("profile", "", "configured profile name")
	contextFile := fs.String("context", "", "copied context JSON file, or - for stdin")
	page := fs.String("url", "", "page URL matching the trusted deployment (never fetched)")
	requestFile := fs.String("request", "", "protobuf JSON request file, or - for stdin")
	system := fs.String("system", "", "explicit system ID")
	account := fs.String("account", "", "explicit account ID")
	from := fs.String("from", "", "absolute UTC start")
	to := fs.String("to", "", "absolute UTC end")
	metric := fs.String("metric", "", "metric ID from ListMetrics")
	pageSize := fs.Int("page-size", 100, "one page, at most 500 rows where supported")
	token := fs.String("page-token", "", "explicit continuation token; never followed automatically")
	maxPoints := fs.Int("max-points", 200, "series buckets, at most 2000")
	timeout := fs.Duration("timeout", 45*time.Second, "shared RPC budget, at most 45s")
	if err = fs.Parse(args); err != nil {
		return err
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *profiles == "" || *name == "" {
		return errors.New("-profiles and -profile are required; browser login does not authorize terminal access")
	}
	if (*contextFile != "" && *page != "") || (*contextFile == "-" && *requestFile == "-") {
		return errors.New("choose one context source and one stdin input")
	}
	if *timeout <= 0 || *timeout > 45*time.Second || *pageSize < 1 || *pageSize > 500 || *maxPoints < 1 || *maxPoints > 2000 {
		return errors.New("invalid budget: timeout 0–45s, page-size 1–500, max-points 1–2000")
	}
	b, err := readInput(*profiles, stdin)
	if err != nil {
		return err
	}
	p, err := LoadProfile(strings.NewReader(string(b)), *name)
	if err != nil {
		return err
	}
	var c *Context
	if *contextFile != "" {
		b, err = readInput(*contextFile, stdin)
		if err != nil {
			return err
		}
		c, err = ParseContext(strings.NewReader(string(b)), p.Deployment)
		if err != nil {
			return err
		}
	}
	if *page != "" {
		if err = p.ValidateURL(*page); err != nil {
			return err
		}
	}
	remote, err := NewRemote(p)
	if err != nil {
		return err
	}
	var src Source = remote
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	scope := *system
	if scope == "" && c != nil {
		scope = c.System
	}
	if scope == "" && *page != "" {
		u, _ := url.Parse(*page)
		scope = u.Query().Get("system")
	}
	infoReq, _ := json.Marshal(map[string]string{"system": scope})
	info, err := src.Query(ctx, "GetLensInfo", infoReq)
	if err != nil {
		return err
	}
	// Read only documented GetLensInfo fields, never infer provenance from names.
	var connection struct {
		Version string `json:"version"`
		Now     string `json:"now"`
		System  string `json:"system"`
	}
	if err = json.Unmarshal(info, &connection); err != nil || connection.Now == "" || connection.System == "" {
		return errors.New("server lacks required clock/system contract; upgrade the server")
	}
	now, err := time.Parse(time.RFC3339Nano, connection.Now)
	if err != nil {
		return errors.New("server returned an unsupported clock")
	}
	if c == nil {
		raw := *page
		if raw == "" {
			raw = strings.TrimSuffix(p.Deployment, "/") + "/"
		}
		c, err = ContextFromURL(raw, p.Deployment, now)
		if err != nil {
			return err
		}
	}
	originalJSON, _ := json.Marshal(c)
	var originalContext Context
	_ = json.Unmarshal(originalJSON, &originalContext)
	if *system != "" {
		c.System = *system
	} else if c.System == "" {
		c.System = connection.System
	}
	if *account != "" {
		c.Account = *account
		delete(c.Filters, "account")
	}
	if *from != "" {
		c.Range.Start = *from
	}
	if *to != "" {
		c.Range.End = *to
	}
	if *metric != "" {
		c.Metric = *metric
	}
	c.keepPointWithinRange()
	if err = c.rebaseQuery(*system != "", *from != "" || *to != "", *account != "", *metric != ""); err != nil {
		return err
	}
	if err = c.Validate(p.Deployment); err != nil {
		return err
	}
	if action == "context" {
		return writeJSON(out, c)
	}
	var overrides map[string]json.RawMessage = map[string]json.RawMessage{}
	if *requestFile != "" {
		b, err = readInput(*requestFile, stdin)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &overrides); err != nil || overrides == nil {
			return errors.New("request must be one JSON object")
		}
	}
	if raw := overrides["system"]; raw != nil {
		var supplied string
		if json.Unmarshal(raw, &supplied) != nil || supplied != c.System {
			return errors.New("request system differs from context; use -system for an explicit scope change")
		}
	}
	fields := Method(method).Input().Fields()
	setDefault := func(k string, v any) {
		if fields.ByJSONName(k) != nil && overrides[k] == nil {
			overrides[k], _ = json.Marshal(v)
		}
	}
	// Explicit flags replace imported scope; request-file fields remain strongest.
	if *account != "" {
		setDefault("account", *account)
	}
	bound := c.Query != nil && c.Query.Method == method
	if !bound || visited["page-size"] {
		setDefault(pageSizeField(method), *pageSize)
	}
	if !bound || visited["max-points"] {
		setDefault("maxPoints", *maxPoints)
	}
	if visited["page-token"] {
		setDefault("pageToken", *token)
	}
	b, _ = json.Marshal(overrides)
	req, unapplied, err := Prepare(method, c, b)
	if err != nil {
		return err
	}
	response, err := src.Query(ctx, method, req)
	if err != nil {
		return err
	}
	// Links and context describe effective query scope; original handoff facts
	// remain separately labeled and cannot reintroduce stale request filters.
	linkContext, linkLimitations, err := effectiveContext(method, c, req)
	if err != nil {
		return err
	}
	link, err := linkContext.Link(p.Deployment)
	if err != nil {
		return err
	}
	links := evidenceLinks(method, response, linkContext, p.Deployment, link)
	if originalLink, err := originalContext.Link(p.Deployment); err == nil && originalLink != link {
		links = append(links, EvidenceLink{Kind: "original_source", URL: originalLink})
	}
	linkScope := Availability{Status: "available", Reason: "UI link carries effective representable request scope; original handoff is labeled separately"}
	if len(linkLimitations) > 0 {
		linkScope = Availability{Status: "partial", Reason: strings.Join(linkLimitations, "; ")}
	}

	return writeJSON(out, map[string]any{"version": 1, "source": map[string]any{"kind": "remote", "deployment": p.Deployment, "serverVersion": connection.Version, "readOnlyRouteVerified": true, "selectedMethodVerified": method, "otherServerMethods": "unverified"}, "method": method, "request": json.RawMessage(req), "response": response, "connection": info, "context": linkContext, "originalContext": &originalContext, "links": links, "unappliedContextFilters": unapplied, "limits": map[string]any{"rpcBudget": 2, "responseBytes": MaxResponseBytes, "automaticPagination": false, "page": "one response; nextPageToken remains in response; use unchanged filters to continue", "truncation": "only reported response fields establish totals/truncation; absent metadata is unavailable"}, "availability": map[string]Availability{"uiLinkScope": linkScope, "actualTimes": {"unavailable", "Use documented response range, epoch, point and coverage times where present; no universal actual interval is provided"}, "provenance": {"unavailable", "No universal cross-RPC snapshot pin is supported; list traversal epoch/range and evidence IDs, when supplied, remain in the typed response"}, "completeness": {"unavailable", "Read response partial/missing/truncated fields and GetDataHistory; empty data does not establish completeness"}, "retention": {"available", "GetLensInfo retention and history-start facts are included; historical coverage requires GetDataHistory"}, "freshness": {"unavailable", "GetLensInfo does not report collection freshness; query GetOverview, GetShellStatus or GetDataHistory"}, "aggregation": {"unavailable", "Query ListMetrics for each metric ID’s authoritative aggregation description; series retain their units and bucket steps"}}})
}

// Page limits have two existing RPC spellings. Keep this transport mapping
// explicit and descriptor-checked; the handlers own defaults and tighter caps.
func pageSizeField(method string) string {
	fields := Method(method).Input().Fields()
	if f := fields.ByName("page_size"); f != nil {
		return f.JSONName()
	}
	switch method {
	case "ListStreams", "ListConsumers":
		if f := fields.ByName("limit"); f != nil {
			return f.JSONName()
		}
	}
	return ""
}

func schema(m protoreflect.MessageDescriptor, depth int) any {
	if depth > 5 {
		return map[string]string{"message": string(m.FullName())}
	}
	fields := []any{}
	for i := 0; i < m.Fields().Len(); i++ {
		f := m.Fields().Get(i)
		v := map[string]any{"name": f.JSONName(), "type": f.Kind().String(), "repeated": f.IsList(), "presence": f.HasPresence()}
		if f.Enum() != nil {
			values := []string{}
			for j := 0; j < f.Enum().Values().Len(); j++ {
				values = append(values, string(f.Enum().Values().Get(j).Name()))
			}
			v["values"] = values
		}
		if f.Message() != nil {
			v["fields"] = schema(f.Message(), depth+1)
		}
		fields = append(fields, v)
	}
	return map[string]any{"message": string(m.FullName()), "fields": fields}
}

// Prepare transports context into existing request semantics. It does not
// calculate a metric, compare storage, or reinterpret domain responses.
func Prepare(method string, c *Context, overrides []byte) ([]byte, []string, error) {
	md := Method(method)
	if md == nil {
		return nil, nil, errors.New("unsupported method")
	}
	fields := md.Input().Fields()
	m := map[string]any{}
	put := func(k string, v any) bool {
		if fields.ByJSONName(k) == nil {
			return false
		}
		m[k] = v
		return true
	}
	put("system", c.System)
	if c.Account != "" {
		put("account", c.Account)
	}
	put("range", c.Range)
	if c.Metric != "" {
		put("metric", c.Metric)
	}
	unapplied := []string{}
	if method == "GetTimeSeriesBatch" && len(c.Series) > 0 {
		queries := []map[string]any{}
		for _, ref := range c.Series {
			if ref.Metric == "" {
				unapplied = append(unapplied, "series identity unavailable: "+ref.Label)
				continue
			}
			q := map[string]any{"metric": ref.Metric}
			if ref.Entity != nil {
				q["entity"] = ref.Entity
			}
			queries = append(queries, q)
		}
		if len(queries) > 0 {
			put("queries", queries)
		}
	}
	for k, v := range c.Filters {
		applied := false
		switch k {
		case "account":
			value := c.Account
			if value == "" {
				value = v
			}
			applied = put("account", value)
		case "check":
			applied = put("checkCode", v)
		case "state":
			if method == "ListFindings" {
				states := []string{}
				switch v {
				case "flapping":
					states = []string{"FINDING_STATE_FLAPPING"}
				case "resolved":
					states = []string{"FINDING_STATE_RESOLVED"}
				case "all":
					states = []string{"FINDING_STATE_FIRING", "FINDING_STATE_CHANGED", "FINDING_STATE_FLAPPING", "FINDING_STATE_RESOLVED"}
				case "open":
					states = []string{"FINDING_STATE_FIRING", "FINDING_STATE_CHANGED", "FINDING_STATE_FLAPPING"}
				}
				if len(states) > 0 {
					applied = put("states", states)
				}
			}
		case "sev":
			applied = put("severities", findingSeverities(v))
		case "muted":
			applied = put("includeMuted", v != "0")
		case "src":
			applied = put("sources", eventSources(v))
		case "kv":
			applied = put("includeKv", v == "1")
		case "ephemeral":
			applied = put("includeEphemeral", v == "1")
		case "tooling":
			applied = put("includeTooling", v == "1")
		case "name":
			applied = put("nameContains", v)
		case "process":
			applied = put("observationServerId", v)
		case "epoch":
			applied = put("observationEpoch", v)
		case "subject":
			applied = put("subject", v)
		case "server":
			applied = put("server", v)
			if !applied {
				applied = c.Entity != nil && fields.ByJSONName("entity") != nil
			}
		case "stream":
			if method == "ListConsumers" && c.Entity != nil && (c.Entity.Type == "ENTITY_TYPE_STREAM" || c.Entity.Type == "ENTITY_TYPE_CONSUMER") {
				applied = true
			} else {
				applied = c.Entity != nil && fields.ByJSONName("entity") != nil
			}
		case "consumer":
			applied = c.Entity != nil && fields.ByJSONName("entity") != nil
		case "kind":
			applied = put("kinds", eventKinds(v))
		case "client":
			applied = put("clientName", v)
		case "show":
			applied = put("hideRoutine", v != "all")
			if applied {
				put("foldRepeats", v != "all")
			}
		case "id":
			if method == "GetEvent" {
				applied = put("id", v)
			}
		case "a", "b":
			applied = put(k, v)
		}
		if !applied {
			unapplied = append(unapplied, k)
		}
	}
	if method == "ListConnections" {
		name, _ := m["nameContains"].(string)
		tooling, _ := m["includeTooling"].(bool)
		put("includeTooling", connectionsIncludeTooling(name, tooling))
	}
	// Resolve selected identity as one scope after ordinary page filters, never
	// from their map iteration order. Explicit request fields are applied later.
	if e := c.Entity; e != nil {
		put("entity", e)
		if e.Account != "" {
			put("account", e.Account)
		}
		switch method {
		case "GetServer":
			put("name", e.Server)
		case "GetStream", "GetConsumer", "ListConsumers":
			if e.Type == "ENTITY_TYPE_STREAM" || e.Type == "ENTITY_TYPE_CONSUMER" {
				put("stream", e.Stream)
				if method == "GetConsumer" {
					put("consumer", e.Consumer)
				}
			}
		case "ListConnections", "GetSlowConsumerInvestigation":
			if e.Type == "ENTITY_TYPE_SERVER" {
				put("server", e.Server)
			}
		}
	}
	for _, e := range c.Evidence {
		if method == "GetFinding" && e.Kind == "finding" || method == "GetEvent" && e.Kind == "event" {
			put("id", e.ID)
		}
	}
	if method == "GetReplay" {
		if m["a"] == nil {
			m["a"] = c.Range.Start
		}
		if m["b"] == nil {
			m["b"] = c.Range.End
		}
	}
	if c.Query != nil && c.Query.Method == method {
		// Replace the lossy UI projection, including false/empty values, with
		// the exact prior machine request. Explicit overrides still win last.
		var exact map[string]json.RawMessage
		if err := json.Unmarshal(c.Query.Request, &exact); err != nil || exact == nil {
			return nil, nil, errors.New("invalid bound read request")
		}
		m = map[string]any{}
		for k, v := range exact {
			m[k] = v
		}
	}
	if len(overrides) > 0 {
		var extra map[string]json.RawMessage
		if err := json.Unmarshal(overrides, &extra); err != nil {
			return nil, nil, errors.New("invalid request object")
		}
		for k, v := range extra {
			m[k] = v
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}
	msg := dynamicpb.NewMessage(md.Input())
	if err = protojson.Unmarshal(b, msg); err != nil {
		return nil, nil, errors.New("invalid request fields; use describe METHOD for the protobuf JSON schema")
	}
	// ASVS 2.2.1: client budget checks supplement the authoritative handler.
	// Descriptor access is for a known request contract, never guessed response metadata.
	if f := fields.ByName("range"); f != nil {
		if !msg.Has(f) {
			return nil, nil, errors.New("range-bearing queries require explicit frozen bounds; range cannot be null")
		}
		r := msg.Get(f).Message()
		rb, _ := protojson.Marshal(r.Interface())
		var b Bounds
		_ = json.Unmarshal(rb, &b)
		if _, _, err = bounds(b); err != nil {
			return nil, nil, err
		}
	}
	for _, n := range []string{"page_size", "limit", "max_points"} {
		f := fields.ByName(protoreflect.Name(n))
		if f != nil {
			v := msg.Get(f).Int()
			limit := int64(500)
			if n == "max_points" {
				limit = 2000
			}
			if v < 0 || v > limit {
				return nil, nil, errors.New("request exceeds page/point budget")
			}
		}
	}
	slices.Sort(unapplied)
	b, err = protojson.Marshal(msg)
	return b, unapplied, err
}
