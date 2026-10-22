package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/micro"
)

// microEndpointRow is one row of micro_endpoints.
type microEndpointRow struct {
	Account     string          `json:"account"`
	Service     string          `json:"service"`
	Instance    string          `json:"instance_id"`
	Endpoint    string          `json:"endpoint"`
	Subject     *string         `json:"subject"`
	QueueGroup  *string         `json:"queue_group"`
	NumRequests int64           `json:"num_requests"`
	NumErrors   int64           `json:"num_errors"`
	Processing  int64           `json:"processing_time_ns"`
	Average     int64           `json:"average_processing_time_ns"`
	LastError   *string         `json:"last_error"`
	Data        json.RawMessage `json:"data"`
}

const microEndpointShape = `[{"account":"VARCHAR","service":"VARCHAR","instance_id":"VARCHAR","endpoint":"VARCHAR",` +
	`"subject":"VARCHAR","queue_group":"VARCHAR","num_requests":"BIGINT","num_errors":"BIGINT",` +
	`"processing_time_ns":"BIGINT","average_processing_time_ns":"BIGINT","last_error":"VARCHAR","data":"JSON"}]`

// microEndpointBatch is the most rows one INSERT carries.
const microEndpointBatch = 5000

var (
	microInstanceCols = []string{"epoch", "account", "service", "instance_id", "version", "description", "metadata", "endpoints"}
	microStatsCols    = []string{"epoch", "account", "service", "instance_id", "version", "started"}
)

// maxEndpointData is the most bytes of an endpoint's custom stats stored;
// nothing shows them yet, and a service may put anything there.
const maxEndpointData = 1024

// endpointData is an endpoint's custom stats as stored: nil for none, and
// for more than maxEndpointData bytes an object that says how many there
// were.
func endpointData(d json.RawMessage) json.RawMessage {
	switch {
	case len(d) == 0 || string(d) == "null":
		return nil
	case len(d) > maxEndpointData:
		return json.RawMessage(fmt.Sprintf(`{"truncated":true,"bytes":%d}`, len(d)))
	}
	return d
}

// microRequestInsert stores one micro_requests row; its times come as
// Unix microseconds of UTC and are made into TIMESTAMP in SQL, so that no
// session time zone shifts them.
const microRequestInsert = `INSERT INTO micro_requests
  (epoch, label, account, request, state, replies, error, refused_at, retry_after)
VALUES (?, ?, ?, ?, ?, ?, ?, make_timestamp(?::BIGINT), make_timestamp(?::BIGINT))`

// unixMicro is t as Unix microseconds, or NULL for the zero time.
func unixMicro(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().UnixMicro()
}

// optString is s, or nil for an empty string: a value the service did
// not report.
func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// optText is optString as a statement argument: s, or NULL when empty.
func optText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// writeMicro stores what the account connections' micro services
// answered: one micro_requests row per request sent, the INFO and STATS
// replies per instance, and STATS endpoints in JSON batches, as an
// account may have many instances with many endpoints.
func (w *writer) writeMicro() {
	var rows []microEndpointRow
	for _, acc := range w.snap.Micro {
		for _, a := range acc.Asked {
			w.exec(microRequestInsert, w.epoch, acc.Label, acc.Account, string(a.Request),
				string(a.State), a.Replies, optText(a.Error), unixMicro(a.RefusedAt), unixMicro(a.RetryAfter))
		}
		for _, in := range acc.Infos {
			eps := in.Endpoints
			if eps == nil {
				eps = []micro.EndpointInfo{}
			}
			var md any
			if len(in.Metadata) > 0 {
				md = in.Metadata
			}
			w.insert("micro_instances", microInstanceCols, w.epoch, acc.Account, in.Name, in.ID, optText(in.Version),
				optText(in.Description), jsonText(md), jsonText(eps))
		}
		for _, s := range acc.Stats {
			w.insert("micro_stats", microStatsCols, w.epoch, acc.Account, s.Name, s.ID, optText(s.Version), ts(s.Started))
			for _, e := range s.Endpoints {
				if e == nil {
					continue
				}
				data := endpointData(e.Data)
				rows = append(rows, microEndpointRow{
					Account: acc.Account, Service: s.Name, Instance: s.ID, Endpoint: e.Name,
					Subject: optString(e.Subject), QueueGroup: optString(e.QueueGroup),
					NumRequests: int64(e.NumRequests), NumErrors: int64(e.NumErrors),
					Processing: e.ProcessingTime.Nanoseconds(), Average: e.AverageProcessingTime.Nanoseconds(),
					LastError: optString(e.LastError), Data: data,
				})
			}
		}
	}
	q := fmt.Sprintf(`INSERT INTO micro_endpoints (epoch, account, service, instance_id, endpoint, subject, queue_group,
  num_requests, num_errors, processing_time_ns, average_processing_time_ns, last_error, data)
SELECT %d, account, service, instance_id, endpoint, subject, queue_group,
  num_requests, num_errors, processing_time_ns, average_processing_time_ns, last_error, data
FROM (SELECT unnest(from_json(?::VARCHAR, '%s'), recursive := true))`, w.epoch, microEndpointShape)
	for len(rows) > 0 && w.err == nil {
		n := min(len(rows), microEndpointBatch)
		b, err := json.Marshal(rows[:n])
		if err != nil {
			w.fail(fmt.Errorf("micro_endpoints: %w", err))
			return
		}
		w.exec(q, string(b))
		rows = rows[n:]
	}
}
