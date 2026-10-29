#!/usr/bin/env bash
# Replay the TLC counterexample for F1 against the real server.
#
#   .claude/skills/verify-pool-overlap/repro.sh [PORT]        default port 18180
#
# Builds cmd/cloudpam inside `nix develop`, starts it on the in-memory store on a scratch
# port, creates an admin, then fires overlapping sibling creates concurrently under a
# fresh parent until two of them both return 201. Kills only the process it started.
# Prints the pools the server stored; a parent with two overlapping children is the bug.
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

DEV_MODE=1 RATE_LIMIT_RPS=0 CLOUDPAM_LOG_LEVEL=error "$WORK/cloudpam" -addr "127.0.0.1:$PORT" >"$WORK/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 50); do curl -fs "$BASE/healthz" >/dev/null 2>&1 && break; sleep 0.2; done
curl -fs "$BASE/healthz" >/dev/null || { echo "server did not come up; log:"; cat "$WORK/server.log"; exit 1; }
echo "server pid $PID on $BASE (in-memory store)"

USER=verify-admin; PASS='VerifyOverlap12345!'
curl -fs -c "$JAR" "$BASE/healthz" >/dev/null                                   # get csrf_token cookie
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
  # Eight siblings that all overlap each other (nested prefixes of the same base).
  rm -f "$WORK"/code.*
  curls=()
  for bits in 17 18 19 20 21 22 23 24; do
    api -o /dev/null -w '%{http_code}\n' -X POST "$BASE/api/v1/pools" \
      -d "{\"name\":\"child-$attempt-$bits\",\"cidr\":\"10.$attempt.0.0/$bits\",\"parent_id\":$parent_id}" >"$WORK/code.$bits" &
    curls+=($!)
  done
  wait "${curls[@]}"   # only the curls; a bare `wait` would also wait for the server
  created="$(cat "$WORK"/code.* | grep -c '^201$' || true)"
  echo "attempt $attempt: parent #$parent_id, 8 overlapping sibling creates in parallel -> $created x 201"
  if [ "$created" -ge 2 ]; then
    echo
    echo "RACE REPRODUCED: $created overlapping siblings stored under parent #$parent_id:"
    api "$BASE/api/v1/pools" | tr '{' '\n' | grep "\"parent_id\":$parent_id" | sed -n 's/.*"name":"\([^"]*\)","cidr":"\([^"]*\)".*/  \1  \2/p'
    exit 0
  fi
done
echo "no double create in 40 attempts; the window is small. Re-run, or widen it by adding load."
exit 2
