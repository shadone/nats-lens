#!/usr/bin/env bash
# Starts or stops a local 3-node NATS cluster for natslens development.
#
#   scripts/dev-cluster.sh start    start nats-0..nats-2 in the background
#   scripts/dev-cluster.sh stop [N] stop them, or only nats-N
#                                   (start brings a stopped node back)
#   scripts/dev-cluster.sh status   show which are running
#
# Accounts: SYS (system account, user sys) and APP (JetStream, user
# app). The passwords are local development values, not secrets.
# Client ports 4222-4224, cluster ports 6222-6224, monitoring 8222-8224.
# Configuration, logs and JetStream data live in .dev/ (gitignored).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEV="$ROOT/.dev"
NATS_SERVER="${NATS_SERVER:-nats-server}"
CLUSTER_NAME="${CLUSTER_NAME:-example-local}"
NODES=(0 1 2)

conf() {
  local i="$1"
  local routes=""
  for j in "${NODES[@]}"; do
    routes+="${routes:+, }nats-route://127.0.0.1:$((6222 + j))"
  done
  cat <<CONF
server_name: nats-$i
listen: 127.0.0.1:$((4222 + i))
http: 127.0.0.1:$((8222 + i))

jetstream {
  store_dir: "$DEV/nats-$i/jetstream"
  max_memory_store: 256MB
  max_file_store: 2GB
}

accounts {
  SYS { users: [ { user: sys, password: sys } ] }
  APP {
    jetstream: enabled
    users: [ { user: app, password: app } ]
  }
}
system_account: SYS

cluster {
  name: $CLUSTER_NAME
  listen: 127.0.0.1:$((6222 + i))
  routes: [ $routes ]
}
CONF
}

pidfile() { echo "$DEV/nats-$1/nats.pid"; }

running() {
  local pf
  pf="$(pidfile "$1")"
  [[ -f "$pf" ]] && kill -0 "$(cat "$pf")" 2>/dev/null
}

start() {
  command -v "$NATS_SERVER" >/dev/null || { echo "nats-server not found" >&2; exit 1; }
  for i in "${NODES[@]}"; do
    mkdir -p "$DEV/nats-$i"
    conf "$i" > "$DEV/nats-$i/nats.conf"
    if running "$i"; then
      echo "nats-$i already running"
      continue
    fi
    "$NATS_SERVER" -c "$DEV/nats-$i/nats.conf" -P "$(pidfile "$i")" -l "$DEV/nats-$i/nats.log" &
    echo "nats-$i started: nats://127.0.0.1:$((4222 + i)), monitoring http://127.0.0.1:$((8222 + i))"
  done
  # Wait for every monitoring port to report healthy JetStream.
  for i in "${NODES[@]}"; do
    local ok=""
    for _ in $(seq 1 50); do
      if curl -fs "http://127.0.0.1:$((8222 + i))/healthz?js-enabled-only=true" >/dev/null 2>&1; then
        ok=1
        break
      fi
      sleep 0.2
    done
    if [[ -z "$ok" ]]; then
      echo "nats-$i did not become healthy; see $DEV/nats-$i/nats.log" >&2
      exit 1
    fi
  done
  echo "system account: nats://sys:sys@127.0.0.1:4222"
}

stop() {
  local nodes=("${NODES[@]}")
  if [[ -n "${1:-}" ]]; then
    nodes=("$1")
  fi
  for i in "${nodes[@]}"; do
    if running "$i"; then
      kill "$(cat "$(pidfile "$i")")"
      echo "nats-$i stopped"
    else
      echo "nats-$i not running"
    fi
  done
}

status() {
  for i in "${NODES[@]}"; do
    if running "$i"; then echo "nats-$i running"; else echo "nats-$i stopped"; fi
  done
}

case "${1:-}" in
  start) start ;;
  stop) stop "${2:-}" ;;
  status) status ;;
  *) echo "usage: $0 start|stop [N]|status" >&2; exit 2 ;;
esac
