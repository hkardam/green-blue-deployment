#!/usr/bin/env bash
# Blue-green deploy helper.
#   ./deploy.sh start            first-time start (db + active green server + nginx + blue client)
#   ./deploy.sh deploy [version] build + start the IDLE server, health-check it,
#                                switch nginx to it, then stop the old server
#   ./deploy.sh rollback         switch back to the previous server
#   ./deploy.sh status           show which server is live
set -euo pipefail
cd "$(dirname "$0")"

CONF=nginx/active.conf
HEALTH_TIMEOUT=${HEALTH_TIMEOUT:-60}   # seconds to wait for the new server to turn healthy
DRAIN_SECONDS=${DRAIN_SECONDS:-5}      # let nginx's old workers finish before stopping the old server

active() { sed -n 's/.*server \([a-z0-9-]*\):8080;.*/\1/p' "$CONF"; }
other()  { if [ "$1" = green-1 ]; then echo green-2; else echo green-1; fi; }
log()    { printf '\n==> %s\n' "$*"; }

wait_healthy() {
  local svc=$1 cid status i
  cid=$(docker compose ps -q "$svc")
  [ -n "$cid" ] || return 1
  for ((i = 0; i < HEALTH_TIMEOUT; i++)); do
    status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$cid")
    [ "$status" = healthy ] && return 0
    sleep 1
  done
  return 1
}

switch_to() {
  local target=$1 backup
  backup=$(cat "$CONF")
  printf '# Managed by deploy.sh - points at the server that currently receives traffic.\nupstream app {\n    server %s:8080;\n}\n' "$target" > "$CONF"
  if ! docker compose exec -T nginx nginx -t; then
    printf '%s\n' "$backup" > "$CONF"
    echo "nginx rejected the new config; traffic left on $(active)" >&2
    return 1
  fi
  docker compose exec -T nginx nginx -s reload   # graceful: in-flight requests finish on old workers
}

# $1 = server to bring live, $2 = "build" or "nobuild", $3 = version label (build only)
cutover() {
  local target=$1 mode=$2 version=${3:-} current upper
  current=$(active)
  upper=$(echo "$target" | tr '[:lower:]-' '[:upper:]_')   # green-2 -> GREEN_2

  log "live: $current   idle: $target"
  if [ "$mode" = build ]; then
    log "building and starting $target (version $version)"
    env "${upper}_VERSION=$version" docker compose up -d --build "$target"
  else
    log "starting $target"
    # `start` revives the existing (stopped) container as-is, keeping its version label
    docker compose start "$target" || docker compose up -d "$target"
  fi

  log "waiting for $target to be healthy"
  if ! wait_healthy "$target"; then
    echo "$target never became healthy - leaving traffic on $current" >&2
    docker compose logs --tail=30 "$target" >&2 || true
    docker compose stop "$target" >/dev/null
    exit 1
  fi

  log "switching traffic: $current -> $target"
  switch_to "$target"

  log "draining $current for ${DRAIN_SECONDS}s, then stopping it (kept for rollback)"
  sleep "$DRAIN_SECONDS"
  docker compose stop "$current" >/dev/null

  log "done - $target is live"
}

case "${1:-}" in
  start)
    docker compose up -d --build --no-recreate db "$(active)" nginx blue
    wait_healthy "$(active)"
    echo "live: $(active)   api: http://localhost:8080   blue client: http://localhost:3001"
    ;;
  deploy)
    cutover "$(other "$(active)")" build "${2:-v$(date +%H%M%S)}"
    ;;
  rollback)
    cutover "$(other "$(active)")" nobuild
    ;;
  status)
    echo "live: $(active)"
    docker compose ps
    ;;
  *)
    sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
