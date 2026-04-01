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
  cat > "${STATUS_FILE}" <<EOF
{
  "status": "${status}",
  "message": "${message}",
  "step": ${step},
  "total_steps": ${TOTAL_STEPS},
  "progress": ${progress},
  "updated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
}

cleanup() {
  rm -f "${REQUEST_FILE}"
}

if [[ ! -f "${REQUEST_FILE}" ]]; then
  exit 0
fi

TARGET_VERSION="$(python3 -c "import json; print(json.load(open('${REQUEST_FILE}')).get('target_version', ''))" 2>/dev/null || true)"
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
