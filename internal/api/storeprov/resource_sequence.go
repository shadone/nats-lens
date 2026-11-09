package storeprov

import "fmt"

// resourceSequenceCTEs preserves observed adjacency in epoch order. Creation
// must be known and agree across responding rows. Consumer delivery additionally
// needs exactly one observed leader; leader changes start a new run. Gaps with
// no resource rows may bridge only within the elapsed-time bound. Identifiers
// and predicates here are compile-time constants; caller values stay parameters.
func resourceSequenceCTEs(consumer bool, where string) string {
	table, identity, reporter, extra := "stream_stats", "''::VARCHAR", "NULL::VARCHAR", ""
	value := "coalesce(max(x.last_seq) FILTER(WHERE x.is_leader),max(x.last_seq))"
	if consumer {
		table, identity = "consumer_stats", "x.consumer"
		reporter = "max(x.server_id) FILTER(WHERE x.is_leader)"
		extra = " AND count(*) FILTER(WHERE x.is_leader)=1"
		value = "max(x.delivered_consumer_seq) FILTER(WHERE x.is_leader)"
	}
	sameLeader := ""
	if consumer {
		sameLeader = " AND server_id=previous_server_id"
	}
	return fmt.Sprintf(`sequence_epochs AS (
 SELECT epoch FROM epochs WHERE started_at>=? AND started_at<?),
sequence_samples AS (
 SELECT x.account,x.stream,%[3]s AS consumer,e.epoch,e.started_at AS t,%[4]s AS server_id,
 CASE WHEN count(x.created)=count(*) AND count(DISTINCT x.created)=1%[5]s THEN max(x.created) END AS created,
 %[6]s AS seq
 FROM %[1]s x JOIN epochs e USING(epoch)
 WHERE e.epoch BETWEEN (SELECT min(epoch) FROM sequence_epochs) AND (SELECT max(epoch) FROM sequence_epochs) %[2]s
 GROUP BY x.account,x.stream,consumer,e.epoch,e.started_at),
sequence_previous AS (
 SELECT *,lag(seq) OVER w AS previous_seq,lag(created) OVER w AS previous_created,
 lag(server_id) OVER w AS previous_server_id,lag(t) OVER w AS previous_at
 FROM sequence_samples WINDOW w AS(PARTITION BY account,stream,consumer ORDER BY epoch)),
sequence_steps AS (
 SELECT *,date_diff('millisecond',previous_at,t) AS elapsed_ms,
 coalesce(created IS NOT NULL AND previous_created=created AND seq>=0 AND previous_seq>=0 AND seq>=previous_seq
 AND date_diff('millisecond',previous_at,t)>0 AND date_diff('millisecond',previous_at,t)<=%[7]s%[8]s,false) AS comparable
 FROM sequence_previous)`, table, where, identity, reporter, extra, value, maxGapToken, sameLeader)
}
func resourceSequenceRate(consumer bool, where string) string {
	return `WITH ` + resourceSequenceCTEs(consumer, where) + `
 SELECT t,(seq::HUGEINT-previous_seq::HUGEINT)*1000.0/elapsed_ms AS v
 FROM sequence_steps WHERE comparable AND epoch IN(SELECT epoch FROM sequence_epochs)`
}
