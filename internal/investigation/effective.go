package investigation

import (
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// effectiveContext describes the request, not the page it originated from.
// UI mappings below are transport mappings only. Unrepresentable RPC options
// are disclosed; no source query or unsupported page filter is carried forward.
func effectiveContext(method string, original *Context, request []byte) (*Context, []string, error) {
	md := Method(method)
	if md == nil {
		return nil, nil, errors.New("unsupported read method")
	}
	msg := dynamicpb.NewMessage(md.Input())
	if err := protojson.Unmarshal(request, msg); err != nil {
		return nil, nil, errors.New("invalid effective request")
	}
	canonical, err := protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(msg)
	if err != nil {
		return nil, nil, err
	}
	var r struct {
		Queries             []SeriesReference `json:"queries"`
		System              string            `json:"system"`
		Account             string            `json:"account"`
		Metric              string            `json:"metric"`
		Name                string            `json:"name"`
		Server              string            `json:"server"`
		FindingID           string            `json:"findingId"`
		ID                  string            `json:"id"`
		Stream              string            `json:"stream"`
		Consumer            string            `json:"consumer"`
		Entity              *Entity           `json:"entity"`
		Range               *Bounds           `json:"range"`
		A                   string            `json:"a"`
		B                   string            `json:"b"`
		CheckCode           string            `json:"checkCode"`
		States              []string          `json:"states"`
		Severities          []string          `json:"severities"`
		Sources             []string          `json:"sources"`
		Kinds               []string          `json:"kinds"`
		ClientName          string            `json:"clientName"`
		IncludeMuted        bool              `json:"includeMuted"`
		HideRoutine         bool              `json:"hideRoutine"`
		FoldRepeats         bool              `json:"foldRepeats"`
		IncludeKv           bool              `json:"includeKv"`
		IncludeEphemeral    bool              `json:"includeEphemeral"`
		Sort                string            `json:"sort"`
		IncludeTooling      bool              `json:"includeTooling"`
		NameContains        string            `json:"nameContains"`
		ObservationServerID string            `json:"observationServerId"`
		ObservationEpoch    string            `json:"observationEpoch"`
	}
	if err := json.Unmarshal(canonical, &r); err != nil {
		return nil, nil, errors.New("unsupported effective request scope")
	}
	c := *original
	c.Filters = map[string]string{}
	c.Series = nil
	c.ChartLabel = ""
	c.Entity = nil
	c.Metric = ""
	c.Evidence = nil
	c.Account = ""
	u, _ := url.Parse(original.SourcePath)
	c.SourcePath = u.EscapedPath()
	fields := md.Input().Fields()
	has := func(n string) bool { return fields.ByJSONName(n) != nil }
	if has("system") {
		c.System = r.System
	} else {
		c.System = ""
	}
	if has("account") {
		c.Account = r.Account
	}
	if r.Range != nil {
		c.Range = *r.Range
	}
	if has("a") && has("b") {
		c.Range = Bounds{r.A, r.B}
	}
	if has("metric") {
		c.Metric = r.Metric
	}
	if r.Entity != nil {
		copy := *r.Entity
		c.Entity = &copy
		if !has("account") {
			c.Account = copy.Account
		}
	}
	if has("server") && r.Server != "" {
		c.Entity = &Entity{Type: "ENTITY_TYPE_SERVER", Key: r.Server, Server: r.Server}
	}
	limitations := []string{}
	switch method {
	case "ListStreams":
		if r.Account != "" {
			limitations = append(limitations, "Streams UI retains account context but does not filter rows by account; inspect exact request and individual resource links")
		}
		c.SourcePath = "/streams"
		c.Filters["kv"] = "0"
		if r.IncludeKv {
			c.Filters["kv"] = "1"
		}
	case "ListConsumers":
		c.SourcePath = "/consumers"
		if r.Stream != "" {
			c.Filters["stream"] = r.Account + "/" + r.Stream
		}
		c.Filters["ephemeral"] = "0"
		if r.IncludeEphemeral {
			c.Filters["ephemeral"] = "1"
		}
		if r.Sort != "CONSUMER_SORT_UNSPECIFIED" {
			limitations = append(limitations, "Consumers API sort is preserved in request; UI order can differ")
		}
	case "ListEvents":
		c.SourcePath = "/events"
		if len(r.Kinds) > 0 {
			c.Filters["kind"] = strings.Join(r.Kinds, ",")
		}
		if r.ClientName != "" {
			c.Filters["client"] = r.ClientName
		}
		sources := []string{}
		for _, v := range r.Sources {
			switch v {
			case "EVENT_SOURCE_NATS":
				sources = append(sources, "nats")
			case "EVENT_SOURCE_KUBERNETES":
				sources = append(sources, "kubernetes")
			case "EVENT_SOURCE_FINDING":
				sources = append(sources, "findings")
			case "EVENT_SOURCE_COLLECTOR":
				sources = append(sources, "collector")
			default:
				limitations = append(limitations, "Event source has no UI equivalent")
			}
		}
		if len(sources) > 0 {
			c.Filters["src"] = strings.Join(sources, ",")
		}
		c.Filters["show"] = "all"
		if r.HideRoutine && r.FoldRepeats {
			c.Filters["show"] = "worth"
		} else if r.HideRoutine != r.FoldRepeats {
			limitations = append(limitations, "Events UI couples hideRoutine and foldRepeats; inspect exact request")
		}
		entityFilter(&c)
	case "ListFindings":
		c.SourcePath = "/findings"
		if r.CheckCode != "" {
			c.Filters["check"] = r.CheckCode
		}
		c.Filters["muted"] = "0"
		if r.IncludeMuted {
			c.Filters["muted"] = "1"
		}
		states := slices.Clone(r.States)
		slices.Sort(states)
		switch strings.Join(states, ",") {
		case "", "FINDING_STATE_CHANGED,FINDING_STATE_FIRING,FINDING_STATE_FLAPPING":
			c.Filters["state"] = "open"
		case "FINDING_STATE_RESOLVED":
			c.Filters["state"] = "resolved"
		case "FINDING_STATE_FLAPPING":
			c.Filters["state"] = "flapping"
		case "FINDING_STATE_CHANGED,FINDING_STATE_FIRING,FINDING_STATE_FLAPPING,FINDING_STATE_RESOLVED":
			c.Filters["state"] = "all"
		default:
			limitations = append(limitations, "Finding-state combination has no exact UI equivalent")
		}
		if len(r.Severities) > 0 {
			values := []string{}
			for _, v := range r.Severities {
				values = append(values, strings.ToLower(strings.TrimPrefix(v, "SEVERITY_")))
			}
			c.Filters["sev"] = strings.Join(values, ",")
		}
		if r.Entity != nil {
			limitations = append(limitations, "Findings UI does not express the API entity filter; inspect request and entity links")
		}
	case "GetStream", "GetConsumer":
		c.Entity = &Entity{Type: "ENTITY_TYPE_STREAM", Key: r.Account + "/" + r.Stream, Account: r.Account, Stream: r.Stream}
		if method == "GetConsumer" {
			c.Entity.Type = "ENTITY_TYPE_CONSUMER"
			c.Entity.Consumer = r.Consumer
			c.Entity.Key += "/" + r.Consumer
		}
		c.SourcePath = entityPath(c.Entity)
	case "GetServer":
		c.Entity = &Entity{Type: "ENTITY_TYPE_SERVER", Key: r.Name, Server: r.Name}
		c.SourcePath = entityPath(c.Entity)
	case "GetOnCall":
		r.ID = r.FindingID
		c.SourcePath = "/findings/" + url.PathEscape(r.ID)
		c.Evidence = []Evidence{{Kind: "finding", ID: r.ID}}
	case "GetFinding":
		c.SourcePath = "/findings/" + url.PathEscape(r.ID)
		c.Evidence = []Evidence{{Kind: "finding", ID: r.ID}}
	case "GetEvent":
		c.SourcePath = "/events"
		c.Filters["id"] = r.ID
		c.Evidence = []Evidence{{Kind: "event", ID: r.ID}}
	case "GetReplay":
		c.SourcePath = "/replay"
		c.Filters["a"] = r.A
		c.Filters["b"] = r.B
	case "GetTimeSeriesBatch", "GetStatusTimeline":
		c.SourcePath = "/"
	case "GetTimeSeries":
		if p := entityPath(c.Entity); p != "" {
			c.SourcePath = p
		} else if original.Entity != nil && original.Entity.Type != "ENTITY_TYPE_SYSTEM" {
			c.SourcePath = "/"
		}
	default:
		paths := map[string]string{"GetOverview": "/", "GetLensInfo": "/data-history", "GetShellStatus": "/", "ListSystems": "/fleet", "ListMetrics": "/", "ListServers": "/servers", "ListAccounts": "/accounts", "ListConnections": "/connections", "ListKeyValueStores": "/kv-stores", "ListObjectStores": "/object-stores", "GetCapacity": "/capacity", "GetDataHistory": "/data-history", "GetPlacement": "/placement", "GetTopology": "/topology", "GetAccountsAndAuth": "/accounts", "GetSubjects": "/subjects", "MatchSubject": "/subjects", "GetMicroServices": "/micro-services", "GetApplications": "/applications", "GetSlowConsumerInvestigation": "/servers/slow-consumers", "ListCheckDefinitions": "/checks"}
		if p := paths[method]; p != "" {
			c.SourcePath = p
		}
	}
	if has("includeTooling") {
		c.Filters["tooling"] = "0"
		if r.IncludeTooling {
			c.Filters["tooling"] = "1"
		}
	}
	if method == "ListConnections" && r.NameContains != "" {
		c.Filters["name"] = r.NameContains
	}
	if method == "GetSlowConsumerInvestigation" {
		if r.ObservationServerID != "" {
			c.Filters["process"] = r.ObservationServerID
		}
		if r.ObservationEpoch != "" && r.ObservationEpoch != "0" {
			c.Filters["epoch"] = r.ObservationEpoch
		}
	}
	if r.Server != "" && (method == "ListConnections" || method == "GetSlowConsumerInvestigation") {
		c.Filters["server"] = r.Server
	}
	// Pagination, point budgets and other options remain exact in request. A
	// page link cannot replay a cursor or reproduce options without UI controls.
	represented := map[string]bool{}
	for _, n := range strings.Fields("system account range a b metric name id finding_id stream consumer page_size limit max_points include_tooling name_contains observation_server_id observation_epoch") {
		represented[n] = true
	}
	switch method {
	case "ListStreams":
		represented["include_kv"] = true
	case "ListConsumers":
		represented["include_ephemeral"] = true
	case "ListEvents":
		for _, n := range strings.Fields("entity sources kinds client_name hide_routine fold_repeats") {
			represented[n] = true
		}
	case "ListFindings":
		for _, n := range strings.Fields("check_code states severities include_muted") {
			represented[n] = true
		}
	case "GetTimeSeries":
		represented["entity"] = true
	case "ListConnections", "GetSlowConsumerInvestigation":
		represented["server"] = true
	}
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if msg.Has(f) && !represented[string(f.Name())] {
			limitations = append(limitations, "UI link does not reproduce request option "+f.JSONName()+"; inspect exact request")
		}
	}

	if method == "GetTimeSeries" || method == "GetTimeSeriesBatch" {
		refs := r.Queries
		if method == "GetTimeSeries" {
			refs = []SeriesReference{{Metric: r.Metric, Entity: r.Entity}}
		}
		matched := 0
		for _, ref := range refs {
			ref.Label = ref.Metric
			for _, before := range original.Series {
				if sameSeries(ref, before) {
					ref.Label = before.Label
					matched++
					break
				}
			}
			c.Series = append(c.Series, ref)
		}
		if matched == len(refs) && matched == len(original.Series) && matched > 0 {
			c.ChartLabel = original.ChartLabel
		}
	}
	c.keepPointWithinRange()
	if err := c.Validate(c.Deployment); err != nil {
		return nil, nil, err
	}
	return &c, limitations, nil
}
func entityPath(e *Entity) string {
	if e == nil {
		return ""
	}
	switch e.Type {
	case "ENTITY_TYPE_SERVER":
		return "/servers/" + url.PathEscape(e.Server)
	case "ENTITY_TYPE_STREAM":
		return "/streams/" + url.PathEscape(e.Account) + "/" + url.PathEscape(e.Stream)
	case "ENTITY_TYPE_CONSUMER":
		return "/consumers/" + url.PathEscape(e.Account) + "/" + url.PathEscape(e.Stream) + "/" + url.PathEscape(e.Consumer)
	}
	return ""
}
func entityFilter(c *Context) {
	if c.Entity == nil {
		return
	}
	switch c.Entity.Type {
	case "ENTITY_TYPE_SERVER":
		c.Filters["server"] = c.Entity.Server
	case "ENTITY_TYPE_STREAM":
		c.Filters["stream"] = c.Entity.Key
	case "ENTITY_TYPE_CONSUMER":
		c.Filters["consumer"] = c.Entity.Key
	}
}

func sameSeries(a, b SeriesReference) bool {
	if a.Metric != b.Metric {
		return false
	}
	if a.Entity == nil || b.Entity == nil {
		return a.Entity == nil && b.Entity == nil
	}
	return a.Entity.Type == b.Entity.Type && a.Entity.Key == b.Entity.Key
}
