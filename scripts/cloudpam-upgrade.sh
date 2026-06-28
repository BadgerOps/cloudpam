#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="${DATA_DIR:-/var/lib/cloudpam/control}"
REQUEST_FILE="${DATA_DIR}/upgrade-requested"
STATUS_FILE="${DATA_DIR}/upgrade-status.json"
HEALTH_URL="${HEALTH_URL:-http://localhost:8080/healthz}"
PULL_TIMEOUT="${PULL_TIMEOUT:-300}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-30}"
SERVER_IMAGE="${SERVER_IMAGE:-ghcr.io/badgerops/cloudpam/server}"
SERVICE_NAME="${SERVICE_NAME:-cloudpam.service}"
TOTAL_STEPS=4

read_request_field() {
  local field="$1"
  python3 - "$REQUEST_FILE" "$field" <<'PY' 2>/dev/null || true
import json
import sys

path, field = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as handle:
    value = json.load(handle).get(field, "")
if value is None:
    value = ""
print(value)
PY
}

write_status() {
  local status="$1"
  local step="$2"
  local message="$3"
  local progress=0
  if [[ "$step" -gt 0 ]]; then
    progress=$(( (step * 100) / TOTAL_STEPS ))
  fi
  if [[ "$status" == "completed" ]]; then
    progress=100
  fi

  mkdir -p "${DATA_DIR}"
  STATUS_VALUE="${status}" \
    MESSAGE_VALUE="${message}" \
    STEP_VALUE="${step}" \
    TOTAL_STEPS_VALUE="${TOTAL_STEPS}" \
    PROGRESS_VALUE="${progress}" \
    UPDATED_AT_VALUE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    UPGRADE_ID_VALUE="${UPGRADE_ID:-}" \
    CURRENT_VERSION_VALUE="${CURRENT_VERSION:-}" \
    TARGET_VERSION_VALUE="${TARGET_VERSION:-}" \
    TARGET_IMAGE_TAG_VALUE="${TARGET_IMAGE_TAG:-}" \
    TARGET_RELEASE_TAG_VALUE="${TARGET_RELEASE_TAG:-}" \
    REQUESTED_AT_VALUE="${REQUESTED_AT:-}" \
    RELEASE_URL_VALUE="${RELEASE_URL:-}" \
    REQUESTED_BY_VALUE="${REQUESTED_BY:-}" \
    python3 - "${STATUS_FILE}" <<'PY'
import json
import os
import sys

payload = {
    "status": os.environ["STATUS_VALUE"],
    "message": os.environ["MESSAGE_VALUE"],
    "step": int(os.environ["STEP_VALUE"]),
    "total_steps": int(os.environ["TOTAL_STEPS_VALUE"]),
    "progress": int(os.environ["PROGRESS_VALUE"]),
    "updated_at": os.environ["UPDATED_AT_VALUE"],
}
for key, env_name in (
    ("upgrade_id", "UPGRADE_ID_VALUE"),
    ("current_version", "CURRENT_VERSION_VALUE"),
    ("target_version", "TARGET_VERSION_VALUE"),
    ("target_image_tag", "TARGET_IMAGE_TAG_VALUE"),
    ("target_release_tag", "TARGET_RELEASE_TAG_VALUE"),
    ("requested_at", "REQUESTED_AT_VALUE"),
    ("release_url", "RELEASE_URL_VALUE"),
    ("requested_by", "REQUESTED_BY_VALUE"),
):
    value = os.environ.get(env_name, "").strip()
    if value:
        payload[key] = value

with open(sys.argv[1], "w", encoding="utf-8") as handle:
    json.dump(payload, handle, indent=2)
    handle.write("\n")
PY
}

cleanup() {
  rm -f "${REQUEST_FILE}"
}

if [[ ! -f "${REQUEST_FILE}" ]]; then
  exit 0
fi

UPGRADE_ID="$(read_request_field "upgrade_id")"
REQUESTED_AT="$(read_request_field "requested_at")"
CURRENT_VERSION="$(read_request_field "current_version")"
TARGET_VERSION="$(read_request_field "target_version")"
TARGET_IMAGE_TAG="$(read_request_field "target_image_tag")"
TARGET_RELEASE_TAG="$(read_request_field "target_release_tag")"
RELEASE_URL="$(read_request_field "release_url")"
REQUESTED_BY="$(read_request_field "requested_by")"

if [[ -z "${TARGET_VERSION}" ]]; then
  write_status "failed" 0 "Could not read target version from upgrade request"
  cleanup
  exit 1
fi

write_status "running" 1 "Pulling cloudpam server image v${TARGET_VERSION}..."
if ! timeout "${PULL_TIMEOUT}" podman pull "${SERVER_IMAGE}:v${TARGET_VERSION}"; then
  write_status "failed" 1 "Failed to pull ${SERVER_IMAGE}:v${TARGET_VERSION}"
  cleanup
  exit 1
fi

write_status "running" 2 "Restarting ${SERVICE_NAME}..."
if ! systemctl restart "${SERVICE_NAME}"; then
  write_status "failed" 2 "Failed to restart ${SERVICE_NAME}"
  cleanup
  exit 1
fi

write_status "running" 3 "Waiting for health check..."
HEALTH_OK=false
for _ in $(seq 1 "${HEALTH_TIMEOUT}"); do
  if curl -sf "${HEALTH_URL}" >/dev/null 2>&1; then
    HEALTH_OK=true
    break
  fi
  sleep 1
done

if [[ "${HEALTH_OK}" != "true" ]]; then
  write_status "failed" 3 "Health check failed after ${HEALTH_TIMEOUT}s"
  cleanup
  exit 1
fi

write_status "completed" 4 "Upgrade completed to v${TARGET_VERSION}"
cleanup
