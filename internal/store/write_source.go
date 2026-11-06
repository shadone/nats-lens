package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/nats-io/nats-server/v2/server"
)

type sourceStatsRow struct {
	Account       string `json:"account"`
	Stream        string `json:"stream"`
	SourceID      string `json:"source_id"`
	Name          string `json:"name"`
	FilterSubject string `json:"filter_subject"`
	Config        any    `json:"config"`
	Reported      bool   `json:"reported"`
	Lag           any    `json:"lag"`
	ActiveNS      any    `json:"active_ns"`
	ErrorCode     any    `json:"error_code"`
	ErrorID       any    `json:"error_id"`
	Error         any    `json:"error"`
}

// sourceReportKey uses only identity fields JSZ reports, not list order.
// nats-server 2.15.0 stream.go composeIName/sourceInfo normalize empty
// transform subjects to >. Consumer and starting position are absent in
// runtime reports; if these make a match ambiguous, leave it unknown.
func sourceReportKey(name string, ext *server.ExternalStream, filter string, transforms []server.SubjectTransformConfig) string {
	normalized := make([]server.SubjectTransformConfig, 0, len(transforms))
	for _, tr := range transforms {
		if tr.Source == "" {
			tr.Source = ">"
		}
		if tr.Destination == "" {
			tr.Destination = ">"
		}
		normalized = append(normalized, tr)
	}
	if len(normalized) == 0 {
		if filter == "" {
			filter = ">"
		}
		normalized = append(normalized, server.SubjectTransformConfig{Source: filter, Destination: ">"})
	}
	api, deliver := "", ""
	if ext != nil {
		api, deliver = ext.ApiPrefix, ext.DeliverPrefix
	}
	b, _ := json.Marshal([]any{name, api, deliver, normalized})
	return string(b)
}

func gatherSources(b *jsRows, account string, s *server.StreamDetail, leader bool) {
	if s.Config == nil || s.Config.Mirror != nil {
		return
	}
	counts := map[string]int{}
	for _, cfg := range s.Config.Sources {
		if cfg != nil {
			counts[sourceReportKey(cfg.Name, cfg.External, cfg.FilterSubject, cfg.SubjectTransforms)]++
		}
	}
	reports := map[string]*server.StreamSourceInfo{}
	duplicate := map[string]bool{}
	if leader {
		for _, src := range s.Sources {
			if src != nil {
				key := sourceReportKey(src.Name, src.External, src.FilterSubject, src.SubjectTransforms)
				if _, ok := reports[key]; ok {
					duplicate[key] = true
				}
				reports[key] = src
			}
		}
	}
	seen := map[string]bool{}
	for _, cfg := range s.Config.Sources {
		if cfg == nil {
			continue
		}
		encoded, err := json.Marshal(cfg)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(encoded)
		id := hex.EncodeToString(sum[:])
		if seen[id] {
			continue
		}
		seen[id] = true
		row := sourceStatsRow{Account: account, Stream: s.Name, SourceID: id, Name: cfg.Name, FilterSubject: cfg.FilterSubject, Config: jsonText(cfg)}
		key := sourceReportKey(cfg.Name, cfg.External, cfg.FilterSubject, cfg.SubjectTransforms)
		if src := reports[key]; src != nil && counts[key] == 1 && !duplicate[key] {
			row.Reported = true
			row.Lag = src.Lag
			row.ActiveNS = src.Active.Nanoseconds()
			if src.Error != nil {
				row.ErrorCode = src.Error.Code
				row.ErrorID = src.Error.ErrCode
				row.Error = src.Error.Description
			}
		}
		b.sources = append(b.sources, row)
	}
}
