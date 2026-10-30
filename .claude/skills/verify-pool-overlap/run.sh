#!/usr/bin/env bash
# Model-check the pool overlap guard. One command, no arguments needed.
#
#   .claude/skills/verify-pool-overlap/run.sh [--out DIR]
#
# Runs TLC (pinned through the repo's own nixpkgs rev from flake.lock) on:
#   one-process.cfg              one process, MemoryStore or SQLite          expected: HOLDS
#   duplicate.cfg                the same, same CIDR twice                   expected: HOLDS
#   multi-process.cfg            three replicas on one PostgreSQL database   expected: HOLDS (#267)
#   multi-process-duplicate.cfg  the same, same CIDR twice                   expected: HOLDS
#   multi-process-local.cfg      three processes on one SQLite file          expected: VIOLATED (L1, unsupported)
#
# Exit codes:
#   0  every run matched SKILL.md and the covered code is unchanged since the sync commit
#   4  every run matched, but covered code in the working tree differs from the sync commit, or that
#      commit is not in this clone: the pass may describe code that no longer exists. Re-read the
#      changed code, update the model, then the sync commit.
#   3  multi-process-local.cfg now HOLDS: the model lost the ability to fail, or stores without
#      PlacementLocker now serialize across processes.
#   1  any other mismatch (a rule broken in a config expected to hold is a bug candidate), or TLC itself failed
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$HERE" rev-parse --show-toplevel)"
OUT="$HERE/out"
if [ "${1:-}" = "--out" ]; then OUT="$2"; fi
mkdir -p "$OUT"

REV="$(sed -n '/"nixpkgs": {/,/}/p' "$REPO/flake.lock" | grep '"rev"' | head -1 | sed 's/.*"rev": "\([^"]*\)".*/\1/')"
if [ -z "$REV" ]; then echo "could not read nixpkgs rev from $REPO/flake.lock" >&2; exit 1; fi
tlc() { nix shell "github:NixOS/nixpkgs/$REV#tlaplus" --command tlc -workers 1 -metadir "$OUT/states-$1" -config "$HERE/$1.cfg" "$HERE/MC.tla"; }

drift=0
SYNC="$(sed -n 's/^sync commit: *\([0-9a-f]\{7,40\}\).*/\1/p' "$HERE/SKILL.md" | head -1)"
COVERED=(
  internal/api/pool_handlers.go internal/api/pool_placement.go internal/api/server.go internal/api/schema_handlers.go internal/api/cidr.go
  internal/storage/store.go internal/storage/sqlite/sqlite.go internal/storage/postgres/postgres.go
  internal/storage/placement.go internal/storage/postgres/placement.go
  migrations/0001_init.sql migrations/postgres/001_core_schema.up.sql
  internal/api/ai_handlers.go internal/api/export_handlers.go internal/api/discovery_handlers.go internal/planning/recommendations.go
)
if [ -n "$SYNC" ] && git -C "$REPO" cat-file -e "$SYNC^{commit}" 2>/dev/null; then
  if ! git -C "$REPO" diff --quiet "$SYNC" -- "${COVERED[@]}"; then
    drift=1
    echo "DRIFT: covered files changed since sync commit $SYNC. The model may no longer describe the code:"
    git -C "$REPO" diff --stat "$SYNC" -- "${COVERED[@]}" | sed 's/^/  /'
  else
    echo "no drift: covered files unchanged since sync commit $SYNC"
  fi
else
  drift=1
  echo "DRIFT: sync commit '$SYNC' from SKILL.md not found in this repo; cannot tell whether the model still describes the code (shallow clone? git fetch --unshallow)"
fi

status=0
check() {
  local cfg="$1" expect="$2" log="$OUT/$1.out"
  echo "----- $cfg (expected: $expect) -----"
  if ! tlc "$cfg" >"$log" 2>&1; then
    if ! grep -q "is violated" "$log"; then
      echo "TLC FAILED on $cfg; see $log"; tail -5 "$log"; status=1; return
    fi
  fi
  local got
  if grep -q "No error has been found" "$log"; then got=HOLDS
  elif grep -q "Invariant .* is violated" "$log"; then got="VIOLATED ($(grep -o 'Invariant [A-Za-z]* is violated' "$log" | head -1 | awk '{print $2}'))"
  else echo "could not read result for $cfg; see $log"; status=1; return; fi
  echo "result: $got   (full TLC output: $log)"
  if [ "$got" = "$expect" ]; then return; fi
  if [ "$cfg" = multi-process-local ] && [ "$got" = HOLDS ]; then
    echo "multi-process-local.cfg HOLDS: either the model can no longer fail, or stores without PlacementLocker now serialize across processes (update SKILL.md)."
    [ "$status" = 1 ] || status=3; return
  fi
  echo "UNEXPECTED: $cfg gave '$got', expected '$expect'. Replay the trace against the code: a broken rule in a config expected to hold is a bug candidate."
  status=1
}
check one-process "HOLDS"
check duplicate "HOLDS"
check multi-process "HOLDS"
check multi-process-duplicate "HOLDS"
check multi-process-local "VIOLATED (NoOverlappingSiblings)"
if [ "$status" = 0 ] && [ "$drift" = 1 ]; then status=4; fi

echo
case $status in
  0) echo "RESULT: matches SKILL.md at the sync commit. One process, and replicas on PostgreSQL, keep siblings apart; L1 still stands for several processes on one SQLite file." ;;
  4) echo "RESULT: TLC matched SKILL.md, but the covered code drifted, so this pass may be on a stale description. Re-read the diff above before trusting it." ;;
  3) echo "RESULT: the negative control multi-process-local.cfg no longer fails. Confirm against the code before updating SKILL.md." ;;
  *) echo "RESULT: a check did not match. Read the trace with /pstack:statechart and decide: model error or bug in the code." ;;
esac
exit $status
