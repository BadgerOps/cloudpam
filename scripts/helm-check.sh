#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

SERVER=deploy/helm/cloudpam-server
AGENT=deploy/helm/cloudpam-agent
OUT="${HELM_CHECK_OUT:-$(mktemp -d)}"
mkdir -p "$OUT"
status=0

fail() {
	echo "FAIL: $*" >&2
	status=1
}

count_rendered_env() {
	cat "${@:2}" | grep -cE "^  ${1}:|- name: ${1}\$" || true
}

want_once() {
	local n
	n="$(count_rendered_env "$@")"
	[ "$n" = 1 ] || fail "$1 rendered $n times in ${*:2} (want 1)"
}

want_absent() {
	local n
	n="$(count_rendered_env "$@")"
	[ "$n" = 0 ] || fail "$1 rendered $n times in ${*:2} (want 0)"
}

render() {
	local name=$1 chart=$2 tag=$3
	shift 3
	helm template "$name" "$chart" "$@" -s templates/configmap.yaml >"$OUT/$tag-configmap.yaml"
	helm template "$name" "$chart" "$@" -s templates/deployment.yaml >"$OUT/$tag-deployment.yaml"
	helm template "$name" "$chart" "$@" >"$OUT/$tag-all.yaml"
}

helm lint "$SERVER"
helm lint "$SERVER" -f "$SERVER/ci/full-values.yaml"
helm lint "$AGENT" -f "$AGENT/ci/full-values.yaml"

render cloudpam "$SERVER" server-default
render cloudpam "$SERVER" server-full -f "$SERVER/ci/full-values.yaml"
render cloudpam-agent "$AGENT" agent-full -f "$AGENT/ci/full-values.yaml"
render cloudpam-agent "$AGENT" agent-default --namespace discovery --set config.accountId=1 --set apiKey=x

release="$(grep -m1 -oE '^## \[[0-9]+\.[0-9]+\.[0-9]+' docs/CHANGELOG.md | sed 's/^## \[//')"
for chart in "$SERVER" "$AGENT"; do
	grep -qx "appVersion: \"${release}\"" "$chart/Chart.yaml" ||
		fail "$chart/Chart.yaml appVersion is not the CHANGELOG release ${release}"
done

sd=("$OUT/server-default-configmap.yaml" "$OUT/server-default-deployment.yaml")
sf=("$OUT/server-full-configmap.yaml" "$OUT/server-full-deployment.yaml")
af=("$OUT/agent-full-configmap.yaml" "$OUT/agent-full-deployment.yaml")

for rendered in "$OUT"/server-default-all.yaml "$OUT"/server-full-all.yaml "$OUT"/agent-full-all.yaml; do
	want_absent CLOUDPAM_AUTH_ENABLED "$rendered"
done

grep -qE "^  APP_VERSION: \"?${release}\"?\$" "${sd[0]}" || fail "default APP_VERSION is not ${release}"
want_once SQLITE_DSN "${sd[@]}"
want_absent DATABASE_URL "${sd[@]}"
grep -q 'kind: PersistentVolumeClaim' "$OUT/server-default-all.yaml" || fail "default render has no PVC"
grep -q 'type: Recreate' "${sd[1]}" || fail "default render is not strategy Recreate"
grep -q 'rollingUpdate' "${sd[1]}" && fail "default render sets rollingUpdate, which the API server rejects with Recreate"

SERVER_FULL_ENV=(
	APP_VERSION
	DATABASE_URL
	CLOUDPAM_ADMIN_USERNAME
	CLOUDPAM_SERVER_URL
	CLOUDPAM_OIDC_CALLBACK_URL
	CLOUDPAM_TRUSTED_PROXIES
	CLOUDPAM_LLM_ENDPOINT
	CLOUDPAM_LLM_MODEL
	CLOUDPAM_TRACING_ENABLED
	CLOUDPAM_TRACING_ENDPOINT
	CLOUDPAM_TRACING_SAMPLE_RATE
	CLOUDPAM_AUDIT_SYSLOG_ADDR
	CLOUDPAM_AUDIT_SYSLOG_NETWORK
	CLOUDPAM_AUDIT_SYSLOG_APP_NAME
)
for name in "${SERVER_FULL_ENV[@]}"; do
	want_once "$name" "${sf[@]}"
done
SERVER_SECRETS=(
	DATABASE_URL
	CLOUDPAM_ADMIN_PASSWORD
	CLOUDPAM_OIDC_ENCRYPTION_KEY
	CLOUDPAM_LLM_API_KEY
	SENTRY_DSN
)
for name in "${SERVER_SECRETS[@]}"; do
	want_absent "$name" "${sf[0]}"
	n="$(grep -c "^  ${name}:" "$OUT/server-full-all.yaml" || true)"
	[ "$n" = 1 ] || fail "$name appears $n times in the postgres render Secret (want 1)"
done
want_absent SQLITE_DSN "${sf[@]}"
grep -q 'kind: PersistentVolumeClaim' "$OUT/server-full-all.yaml" && fail "postgres render still has a PVC"
grep -q 'mountPath: /data' "${sf[1]}" && fail "postgres render still mounts /data"
grep -q 'type: RollingUpdate' "${sf[1]}" || fail "postgres render is not strategy RollingUpdate"

want_once CLOUDPAM_API_KEY "${af[@]}"
want_absent CLOUDPAM_AGENT_NAME "${af[@]}"
grep -q '^    agent_name: ' "${af[0]}" || fail "agent.yaml has no agent_name"
grep -q '^    aws_org:' "${af[0]}" || fail "agent.yaml has no aws_org block"
grep -q '^    agent_name: "discovery-cloudpam-agent"$' "$OUT/agent-default-configmap.yaml" ||
	fail "default agent_name is not <namespace>-<fullname>"

check_values_catalog_lists_every_server_env() {
	local not_for_pod=(ADDR PORT APP_VERSION SQLITE_DSN CLOUDPAM_STORAGE DEV_MODE CLOUDPAM_DEV_MODE
		CLOUDPAM_CONTROL_DIR CLOUDPAM_UPGRADE_DATA_DIR CLOUDPAM_RESET_PASSWORD)
	local read_by_server in_chart name
	read_by_server="$(grep -rhoE '(os\.Getenv|envOr|envIntOrDefault|getenvDefault)\("[A-Z][A-Z0-9_]+"' \
		cmd/cloudpam internal --include='*.go' --exclude='*_test.go' | sed -E 's/.*"([A-Z0-9_]+)"/\1/' | sort -u)"
	in_chart="$(grep -oE '^  [A-Z][A-Z0-9_]+:' "$SERVER/values.yaml" | tr -d ' :' | sort -u)"
	for name in $read_by_server; do
		grep -qx "$name" <<<"$in_chart" && continue
		printf '%s\n' "${not_for_pod[@]}" | grep -qx "$name" && continue
		fail "the server reads $name but $SERVER/values.yaml does not list it"
	done
}
check_values_catalog_lists_every_server_env

if [ "$status" = 0 ]; then
	echo "helm-check: ok (rendered manifests in $OUT)"
fi
exit "$status"
