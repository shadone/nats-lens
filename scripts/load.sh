#!/usr/bin/env bash
# Creates shop-shaped streams, consumers and KV buckets on the local dev
# cluster (scripts/dev-cluster.sh) and publishes some traffic.
#
#   scripts/load.sh           create assets (idempotent) and publish once
#   scripts/load.sh traffic   only publish and consume
#   scripts/load.sh churn     delete and recreate a consumer from a named
#                             connection, then restart nats-2: produces
#                             consumer, leader election, lame duck and
#                             disconnect events for the collector
#
# Names follow an online shop in the APP account: the order-jobs work
# stream with its worker consumers, the order-archive mirror, catalog-changes, the
# stock-levels KV bucket, and an R1 worker-presence KV bucket.
set -euo pipefail

SERVER="${NATS_URL:-nats://127.0.0.1:4222}"
NATS=(nats --server "$SERVER" --user app --password app)

exists() { "${NATS[@]}" "$1" info "$2" >/dev/null 2>&1; }

stream() {
  local name="$1"; shift
  if exists stream "$name"; then
    echo "stream $name exists"
  else
    "${NATS[@]}" stream add "$name" --defaults "$@" >/dev/null
    echo "stream $name created"
  fi
}

consumer() {
  local stream="$1" name="$2"; shift 2
  if "${NATS[@]}" consumer info "$stream" "$name" >/dev/null 2>&1; then
    echo "consumer $stream > $name exists"
  else
    "${NATS[@]}" consumer add "$stream" "$name" --pull --ack explicit --defaults "$@" >/dev/null
    echo "consumer $stream > $name created"
  fi
}

kv() {
  local name="$1"; shift
  if "${NATS[@]}" kv info "$name" >/dev/null 2>&1; then
    echo "kv $name exists"
  else
    "${NATS[@]}" kv add "$name" "$@" >/dev/null
    echo "kv $name created"
  fi
}

assets() {
  stream order-jobs --subjects 'orders.*.*.*.*' --replicas 3 --retention limits --storage file \
    --max-age 72h --description 'All order jobs'
  consumer order-jobs resize-worker --filter 'orders.*.*.resize.created' --max-deliver 3 \
    --max-pending 20 --wait 60s
  consumer order-jobs index-worker --filter 'orders.*.*.index.created' --max-deliver 3 \
    --max-pending 20 --wait 60s
  consumer order-jobs order-processed --filter 'orders.*.*.*.completed' --max-deliver=-1

  stream order-archive --mirror order-jobs --replicas 3 --storage file \
    --description 'Permanent archive of all order messages'
  consumer order-archive analytics-aggregator --filter 'orders.*.*.*.completed' --max-deliver=-1

  stream catalog-changes --subjects 'catalog.changes.>' --replicas 3 --retention limits --storage file \
    --description 'Catalog change events'
  consumer catalog-changes catalog-writer --filter 'catalog.changes.>' --max-deliver=-1

  stream order-callbacks --subjects 'orders.callbacks.*' --replicas 3 --retention work \
    --storage file --description 'Order callback deliveries'
  consumer order-callbacks callback-delivery --max-deliver 5

  kv stock-levels --replicas 3 --history 1
  kv worker-presence --replicas 1 --history 1 --ttl 1m
}

traffic() {
  "${NATS[@]}" pub 'orders.p1.job{{Count}}.resize.created' 'resize job {{Count}}' --count 200 >/dev/null
  "${NATS[@]}" pub 'orders.p2.job{{Count}}.index.created' 'index job {{Count}}' --count 120 >/dev/null
  "${NATS[@]}" pub 'orders.p1.job{{Count}}.resize.completed' 'done {{Count}}' --count 150 >/dev/null
  "${NATS[@]}" pub 'catalog.changes.c1.r{{Count}}' 'mutation {{Count}}' --count 80 >/dev/null
  "${NATS[@]}" pub 'orders.callbacks.p1' 'callback {{Count}}' --count 30 >/dev/null
  for i in 1 2 3; do
    "${NATS[@]}" kv put stock-levels "product-$i" "{\"product\":$i}" >/dev/null
  done
  "${NATS[@]}" kv put worker-presence "worker-1" '{"host":"local"}' >/dev/null
  # Drain part of the backlog so consumers show both delivered and pending work.
  "${NATS[@]}" consumer next order-jobs resize-worker --count 120 --ack >/dev/null
  "${NATS[@]}" consumer next order-jobs index-worker --count 40 --ack >/dev/null
  "${NATS[@]}" consumer next order-jobs index-worker --count 10 --no-ack >/dev/null
  "${NATS[@]}" consumer next catalog-changes catalog-writer --count 80 --ack >/dev/null
  echo "traffic published"
}

churn() {
  local root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  "${NATS[@]}" --connection-name load-churn consumer rm order-jobs index-worker --force >/dev/null
  echo "consumer order-jobs > index-worker deleted by connection load-churn"
  # Wait two collect intervals (scripts/natslens.dev.yaml: 10s) so that at
  # least one epoch sees the consumer missing and consumer-010 fires.
  local wait="${CHURN_WAIT:-20}"
  echo "waiting ${wait}s before recreating it"
  sleep "$wait"
  "${NATS[@]}" --connection-name load-churn consumer add order-jobs index-worker --pull --ack explicit \
    --defaults --filter 'orders.*.*.index.created' --max-deliver 3 --max-pending 20 --wait 60s >/dev/null
  echo "consumer order-jobs > index-worker created again"
  "$root/scripts/dev-cluster.sh" stop 2
  sleep 3
  "$root/scripts/dev-cluster.sh" start >/dev/null
  echo "nats-2 restarted"
}

case "${1:-all}" in
  all) assets; traffic ;;
  traffic) traffic ;;
  churn) churn ;;
  *) echo "usage: $0 [traffic|churn]" >&2; exit 2 ;;
esac
