#!/usr/bin/env bash
# Model-check the pool overlap guard. One command, no arguments needed.
#
#   .claude/skills/verify-pool-overlap/run.sh [--out DIR]
#
# Runs TLC (pinned through the repo's own nixpkgs rev from flake.lock) on:
#   race.cfg          code as of the sync commit    expected: VIOLATED (finding F1)
#   atomic.cfg        proposed fix                  expected: HOLDS
#   memstore-dup.cfg  in-memory store, same CIDR    expected: VIOLATED (finding F2)
#
# Exit codes:
#   0  every run matched the expectation recorded in SKILL.md (findings still stand)
#   3  race.cfg now HOLDS: the finding is fixed, or the model no longer describes the code.
#      Re-read createPool before celebrating, then update SKILL.md.
#   1  a run did not match any expectation, or TLC itself failed (model error, missing tool)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(git -C "$HERE" rev-parse --show-toplevel)"
OUT="$HERE/out"
if [ "${1:-}" = "--out" ]; then OUT="$2"; fi
mkdir -p "$OUT"

# Same nixpkgs the flake pins, so everyone gets the same TLC (2.19 / tlaplus 1.7.4 at the sync commit).
REV="$(sed -n '/"nixpkgs": {/,/}/p' "$REPO/flake.lock" | grep '"rev"' | head -1 | sed 's/.*"rev": "\([^"]*\)".*/\1/')"
if [ -z "$REV" ]; then echo "could not read nixpkgs rev from $REPO/flake.lock" >&2; exit 1; fi
tlc() { nix shell "github:NixOS/nixpkgs/$REV#tlaplus" --command tlc -workers 1 -metadir "$OUT/states-$1" -config "$HERE/$1.cfg" "$HERE/MC.tla"; }

# --- drift: has the covered code changed since the model was last confirmed? ---
SYNC="$(sed -n 's/^sync commit: *\([0-9a-f]\{7,40\}\).*/\1/p' "$HERE/SKILL.md" | head -1)"
COVERED=(internal/api/pool_handlers.go internal/api/cidr.go internal/storage/store.go internal/storage/sqlite/sqlite.go internal/storage/postgres/postgres.go migrations/0001_init.sql)
if [ -n "$SYNC" ] && git -C "$REPO" cat-file -e "$SYNC^{commit}" 2>/dev/null; then
  if ! git -C "$REPO" diff --quiet "$SYNC" HEAD -- "${COVERED[@]}"; then
    echo "DRIFT: covered files changed since sync commit $SYNC. The model may no longer describe the code:"
    git -C "$REPO" diff --stat "$SYNC" HEAD -- "${COVERED[@]}" | sed 's/^/  /'
  else
    echo "no drift: covered files unchanged since sync commit $SYNC"
  fi
else
  echo "WARN: sync commit '$SYNC' from SKILL.md not found in this repo; skipping drift check"
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
  if [ "${got%% *}" = "${expect%% *}" ]; then return; fi
  if [ "$cfg" = race ] && [ "$got" = HOLDS ]; then
    echo "race.cfg HOLDS: either createPool is now atomic (update SKILL.md, move the sync commit) or the model drifted."
    status=3; return
  fi
  echo "UNEXPECTED: $cfg gave '$got', expected '$expect'. Treat the model as broken until explained."
  status=1
}
check race "VIOLATED (NoOverlappingSiblings)"
check atomic "HOLDS"
check memstore-dup "VIOLATED (NoOverlappingSiblings)"

echo
case $status in
  0) echo "RESULT: as recorded in SKILL.md. Findings F1 and F2 still stand. Read any trace with /pstack:statechart." ;;
  3) echo "RESULT: F1 no longer reproduces in the model. Confirm against the code before updating SKILL.md." ;;
  *) echo "RESULT: verification itself failed; fix the model or the tool, not the code." ;;
esac
exit $status
