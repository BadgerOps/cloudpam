#!/usr/bin/env bash
# Replay the model's interleavings on the real server (one process, in-memory store).
#
#   .claude/skills/verify-pool-overlap/repro.sh [PORT]        default port 18180
#
# Builds cmd/cloudpam inside `nix develop`, starts it with DEV_MODE=1 on a scratch port, creates
# an admin, then 40 times fires eight overlapping sibling creates at once under a fresh parent,
# and finally posts one CIDR twice. Kills only the process it started.
# Exit 0: every attempt stored exactly one sibling and refused seven with 400, and the repeat got
# 409 (one-process.cfg and duplicate.cfg hold on the binary). Exit 1: an attempt got other codes,
# or the repeat was not refused; the codes and the pools stored under that parent are printed.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$HERE" rev-parse --show-toplevel)"
PORT="${1:-18180}"
BASE="http://127.0.0.1:$PORT"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/verify-pool-overlap.XXXXXX")"
JAR="$WORK/cookies"
PID=""
cleanup() { if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then kill "$PID"; wait "$PID" 2>/dev/null || true; fi; rm -rf "$WORK"; }
trap cleanup EXIT

echo "building cmd/cloudpam ..."
( cd "$REPO" && nix develop --command go build -o "$WORK/cloudpam" ./cmd/cloudpam )

if curl -fs "$BASE/healthz" >/dev/null 2>&1; then echo "port $PORT already answers /healthz; pass a free port"; exit 1; fi
DEV_MODE=1 RATE_LIMIT_RPS=0 CLOUDPAM_LOG_LEVEL=error "$WORK/cloudpam" -addr "127.0.0.1:$PORT" >"$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 50); do curl -fs "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.2; done
curl -fs "$BASE/healthz" >/dev/null && kill -0 "$PID" 2>/dev/null || { echo "server did not come up; log:"; cat "$WORK/server.log"; exit 1; }
echo "server pid $PID on $BASE (in-memory store)"

USER=verify-admin; PASS='VerifyOverlap12345!'
curl -fs -c "$JAR" "$BASE/healthz" >/dev/null
curl -fs -o /dev/null -X POST "$BASE/api/v1/auth/setup" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\",\"email\":\"$USER@localhost\"}"
curl -fs -o /dev/null -b "$JAR" -c "$JAR" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}"
CSRF="$(awk '$6=="csrf_token"{print $7}' "$JAR" | tail -1)"
api() { curl -s -b "$JAR" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' "$@"; }

for attempt in $(seq 1 40); do
  parent_json="$(api -X POST "$BASE/api/v1/pools" -d "{\"name\":\"race-parent-$attempt\",\"cidr\":\"10.$attempt.0.0/16\"}")"
  parent_id="$(printf '%s' "$parent_json" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')"
  [ -n "$parent_id" ] || { echo "parent create failed: $parent_json"; exit 1; }
  rm -f "$WORK"/code.*
  curls=()
  for bits in 17 18 19 20 21 22 23 24; do
    api -o /dev/null -w '%{http_code}\n' -X POST "$BASE/api/v1/pools" \
      -d "{\"name\":\"child-$attempt-$bits\",\"cidr\":\"10.$attempt.0.0/$bits\",\"parent_id\":$parent_id}" >"$WORK/code.$bits" &
    curls+=($!)
  done
  wait "${curls[@]}"   # only the curls; a bare `wait` would also wait for the server
  created="$(cat "$WORK"/code.* | grep -c '^201$' || true)"
  refused="$(cat "$WORK"/code.* | grep -c '^400$' || true)"
  echo "attempt $attempt: parent #$parent_id, 8 overlapping sibling creates in parallel -> $created x 201, $refused x 400"
  if [ "$created" -ne 1 ] || [ "$refused" -ne 7 ]; then
    echo
    echo "OVERLAP GUARD BROKEN: $created x 201 and $refused x 400 under parent #$parent_id (expected 1 and 7); codes: $(cat "$WORK"/code.* | tr '\n' ' ')"
    api "$BASE/api/v1/pools" | tr '{' '\n' | grep "\"parent_id\":$parent_id" | sed -n 's/.*"name":"\([^"]*\)","cidr":"\([^"]*\)".*/  \1  \2/p'
    exit 1
  fi
done

first="$(api -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/pools" -d '{"name":"dup-a","cidr":"10.200.0.0/16"}')"
second="$(api -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/pools" -d '{"name":"dup-b","cidr":"10.200.0.0/16"}')"
echo "same top-level CIDR twice: $first then $second"
[ "$first" = 201 ] && [ "$second" = 409 ] || { echo "DUPLICATE NOT REFUSED: expected 201 then 409"; exit 1; }
echo "guard held: 40 x exactly one 201 among eight overlapping siblings, and the repeat got 409"
