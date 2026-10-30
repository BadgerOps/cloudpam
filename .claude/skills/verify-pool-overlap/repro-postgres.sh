#!/usr/bin/env bash
# Replay multi-process.cfg on the real server: two postgres-tagged CloudPAM processes on one database.
#
#   DATABASE_URL=postgres://user:pass@host:port/db?sslmode=disable \
#     .claude/skills/verify-pool-overlap/repro-postgres.sh [PORT_A [PORT_B]]   default ports 18181 18182
#
# DATABASE_URL names a PostgreSQL server and a database to connect to; its user needs CREATEDB.
# The script creates a scratch database next to it, builds cmd/cloudpam with -tags postgres inside
# `nix develop`, starts two processes on that database one after the other, and checks that each
# logs "placement guard: postgres advisory lock". Then, 40 times, it fires eight overlapping sibling
# creates at once under a fresh parent, four on each process, and finally posts one top-level CIDR on
# both processes at once. It stops only the processes it started and drops only the database it
# created.
# Exit 0: every attempt stored exactly one sibling and refused seven with 400, the CIDR posted twice
# got one 201 and one 409, and GET /api/v1/pools shows no overlapping siblings (multi-process.cfg and
# multi-process-duplicate.cfg hold on the binary). Exit 1: any other result; the codes and the
# overlapping pools are printed.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$HERE" rev-parse --show-toplevel)"
PA="${1:-18181}"; PB="${2:-18182}"
: "${DATABASE_URL:?set DATABASE_URL to a PostgreSQL database whose user may CREATE DATABASE}"
case "$DATABASE_URL" in postgres://*/*|postgresql://*/*) ;; *) echo "DATABASE_URL must look like postgres://user:pass@host:port/db"; exit 1 ;; esac

REV="$(sed -n '/"nixpkgs": {/,/}/p' "$REPO/flake.lock" | grep '"rev"' | head -1 | sed 's/.*"rev": "\([^"]*\)".*/\1/')"
pg() { if command -v psql >/dev/null; then psql "$@"; else nix shell "github:NixOS/nixpkgs/$REV#postgresql" --command psql "$@"; fi; }

DB="cloudpam_overlap_$(date +%s)_$$"
BASE_URL="${DATABASE_URL%%\?*}"; QUERY=""; [ "$BASE_URL" = "$DATABASE_URL" ] || QUERY="?${DATABASE_URL#*\?}"
SCRATCH_URL="${BASE_URL%/*}/$DB$QUERY"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/verify-pool-overlap-pg.XXXXXX")"
PASS='VerifyOverlapPg12345!'
PIDS=(); CREATED=""
cleanup() {
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  for p in "${PIDS[@]}"; do wait "$p" 2>/dev/null || true; done
  if [ -n "$CREATED" ]; then pg "$DATABASE_URL" -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null || echo "could not drop scratch database $DB"; fi
  rm -rf "$WORK"
}
trap cleanup EXIT

for port in "$PA" "$PB"; do
  if curl -fs "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then echo "port $port already answers /healthz; pass free ports"; exit 1; fi
done

echo "building cmd/cloudpam -tags postgres ..."
( cd "$REPO" && nix develop --command go build -tags postgres -o "$WORK/cloudpam" ./cmd/cloudpam )

pg "$DATABASE_URL" -qc "CREATE DATABASE $DB" >/dev/null
CREATED=1
echo "scratch database $DB"

start() {
  local port="$1" name="$2"
  ADDR="127.0.0.1:$port" DATABASE_URL="$SCRATCH_URL" RATE_LIMIT_RPS=0 CLOUDPAM_LOG_FORMAT=text \
    CLOUDPAM_ADMIN_USERNAME=verify-admin CLOUDPAM_ADMIN_PASSWORD="$PASS" CLOUDPAM_ADMIN_EMAIL=verify-admin@localhost \
    "$WORK/cloudpam" >"$WORK/$name.log" 2>&1 &
  PIDS+=("$!")
  for _ in $(seq 1 100); do curl -fs "http://127.0.0.1:$port/healthz" >/dev/null 2>&1 && break; sleep 0.2; done
  curl -fs "http://127.0.0.1:$port/healthz" >/dev/null || { echo "process $name did not come up; log:"; tail -20 "$WORK/$name.log"; exit 1; }
  grep -q 'placement guard: postgres advisory lock' "$WORK/$name.log" || { echo "process $name does not log the postgres placement guard; log:"; tail -20 "$WORK/$name.log"; exit 1; }
  echo "process $name pid ${PIDS[-1]} on 127.0.0.1:$port, placement guard: postgres advisory lock"
}
# One after the other: two processes migrating a fresh database at once is a separate race.
start "$PA" a
start "$PB" b

login() {
  local port="$1" jar="$WORK/jar-$1"
  curl -fs -o /dev/null -c "$jar" -b "$jar" -H 'Content-Type: application/json' \
    -d "{\"username\":\"verify-admin\",\"password\":\"$PASS\"}" "http://127.0.0.1:$port/api/v1/auth/login"
  curl -fs -o /dev/null -c "$jar" -b "$jar" "http://127.0.0.1:$port/api/v1/auth/me"
  awk '$6 == "csrf_token" {print $7}' "$jar" | tail -1
}
CA="$(login "$PA")"; CB="$(login "$PB")"
[ -n "$CA" ] && [ -n "$CB" ] || { echo "login failed"; exit 1; }
api() { local port="$1" csrf="$2"; shift 2; curl -s -b "$WORK/jar-$port" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' "$@"; }
post_code() { api "$1" "$2" -o /dev/null -w '%{http_code}\n' -X POST "http://127.0.0.1:$1/api/v1/pools" -d "$3"; }

bad=0
for attempt in $(seq 1 40); do
  parent_json="$(api "$PA" "$CA" -X POST "http://127.0.0.1:$PA/api/v1/pools" -d "{\"name\":\"race-parent-$attempt\",\"cidr\":\"10.$attempt.0.0/16\"}")"
  parent_id="$(printf '%s' "$parent_json" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')"
  [ -n "$parent_id" ] || { echo "parent create failed: $parent_json"; exit 1; }
  rm -f "$WORK"/code.*
  curls=()
  for bits in 17 18 19 20 21 22 23 24; do
    if (( bits % 2 )); then port="$PA"; csrf="$CA"; else port="$PB"; csrf="$CB"; fi
    post_code "$port" "$csrf" "{\"name\":\"child-$attempt-$bits\",\"cidr\":\"10.$attempt.0.0/$bits\",\"parent_id\":$parent_id}" >"$WORK/code.$bits" &
    curls+=($!)
  done
  wait "${curls[@]}"   # only the curls; a bare `wait` would also wait for the servers
  created="$(cat "$WORK"/code.* | grep -c '^201$' || true)"
  refused="$(cat "$WORK"/code.* | grep -c '^400$' || true)"
  echo "attempt $attempt: parent #$parent_id, 8 overlapping sibling creates across both processes -> $created x 201, $refused x 400"
  if [ "$created" -ne 1 ] || [ "$refused" -ne 7 ]; then
    echo
    echo "OVERLAP GUARD BROKEN ACROSS PROCESSES: $created x 201 and $refused x 400 under parent #$parent_id (expected 1 and 7); codes: $(cat "$WORK"/code.* | tr '\n' ' ')"
    bad=1; break
  fi
done

if [ "$bad" = 0 ]; then
  post_code "$PA" "$CA" '{"name":"dup-a","cidr":"10.200.0.0/16"}' >"$WORK/dup.a" &
  da=$!
  post_code "$PB" "$CB" '{"name":"dup-b","cidr":"10.200.0.0/16"}' >"$WORK/dup.b" &
  db=$!
  wait "$da" "$db"
  dup="$(cat "$WORK/dup.a" "$WORK/dup.b" | sort | tr '\n' ' ')"
  echo "same top-level CIDR on both processes at once: $dup"
  [ "$dup" = "201 409 " ] || { echo "DUPLICATE NOT REFUSED ACROSS PROCESSES: expected one 201 and one 409"; bad=1; }
fi

api "$PB" "$CB" "http://127.0.0.1:$PB/api/v1/pools" >"$WORK/pools.json"
python3 -I - "$WORK/pools.json" <<'EOF' || bad=1
import ipaddress, itertools, json, sys
pools = json.load(open(sys.argv[1]))
bad = [(a, b) for a, b in itertools.combinations(pools, 2)
       if a.get("parent_id") == b.get("parent_id")
       and ipaddress.ip_network(a["cidr"]).overlaps(ipaddress.ip_network(b["cidr"]))]
print(f"GET /api/v1/pools on process b: {len(pools)} pools, {len(bad)} overlapping sibling pairs")
for a, b in bad[:10]:
    print(f"  #{a['id']} {a['cidr']} overlaps #{b['id']} {b['cidr']} (parent {a.get('parent_id')})")
sys.exit(1 if bad else 0)
EOF
[ "$bad" = 0 ] || exit 1
echo "guard held across processes: 40 x exactly one 201 among eight overlapping siblings, and the CIDR posted on both got 201 and 409"
