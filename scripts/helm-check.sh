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
helm lint "$AGENT" -f "$AGENT/ci/gcp-org-values.yaml"
render cloudpam-agent "$AGENT" agent-gcp-org -f "$AGENT/ci/gcp-org-values.yaml"
grep -q 'provider: "gcp"' "$OUT/agent-gcp-org-configmap.yaml" || fail "gcp chart does not render provider gcp"
grep -q 'parent: "organizations/123456789"' "$OUT/agent-gcp-org-configmap.yaml" || fail "gcp chart does not render gcp_org.parent"
grep -q 'impersonate_service_account: "cloudpam-discovery@{project}.iam.gserviceaccount.com"' "$OUT/agent-gcp-org-configmap.yaml" ||
	fail "gcp chart does not render the impersonation template"
want_once GOOGLE_APPLICATION_CREDENTIALS "$OUT/agent-gcp-org-deployment.yaml"
want_absent GOOGLE_APPLICATION_CREDENTIALS "$OUT/agent-full-deployment.yaml"
render cloudpam-agent "$AGENT" agent-explicit --set config.accountId=1 --set config.agentName=cluster-a-discovery --set apiKey=x --set config.provider=aws

render cloudpam "$SERVER" server-vector --set vector.enabled=true
helm template cloudpam "$SERVER" --set vector.enabled=true -s templates/vector-configmap.yaml |
	sed -n '/^  vector.toml: |$/,$p' | tail -n +2 | sed 's/^    //' >"$OUT/server-vector.toml"

want_fail() {
	local tag=$1 message=$2
	shift 2
	if helm template cloudpam "$SERVER" "$@" >"$OUT/$tag.txt" 2>&1; then
		fail "server chart renders with $* (want failure)"
	elif ! grep -q "$message" "$OUT/$tag.txt"; then
		fail "server chart with $* does not explain: $message"
	fi
}
want_fail vector-own-addr 'config.CLOUDPAM_AUDIT_SYSLOG_ADDR is set by the chart while vector.enabled' \
	--set vector.enabled=true --set config.CLOUDPAM_AUDIT_SYSLOG_ADDR=syslog:514
want_fail vector-own-network 'config.CLOUDPAM_AUDIT_SYSLOG_NETWORK is set by the chart while vector.enabled' \
	--set vector.enabled=true --set config.CLOUDPAM_AUDIT_SYSLOG_NETWORK=udp
want_fail vector-no-listener 'vector.config must keep a syslog source' \
	--set vector.enabled=true --set-string 'vector.config=data_dir = "/var/lib/vector"'

if helm template cloudpam-agent "$AGENT" --set apiKey=x --set config.provider=aws >"$OUT/agent-missing-name.txt" 2>&1; then
	fail "agent chart renders without required config.agentName"
elif ! grep -q 'config.agentName is required' "$OUT/agent-missing-name.txt"; then
	fail "agent chart does not explain that config.agentName is required"
fi
if helm template cloudpam-agent "$AGENT" --set apiKey=x --set config.agentName=a >"$OUT/agent-missing-provider.txt" 2>&1; then
	fail "agent chart renders without required config.provider"
elif ! grep -q 'config.provider is required (aws or gcp)' "$OUT/agent-missing-provider.txt"; then
	fail "agent chart does not explain that config.provider is required"
fi
agent_want_fail() {
	local tag=$1 message=$2
	shift 2
	if helm template cloudpam-agent "$AGENT" --set apiKey=x --set config.agentName=a "$@" >"$OUT/$tag.txt" 2>&1; then
		fail "agent chart renders with $* (want failure)"
	elif ! grep -qF "$message" "$OUT/$tag.txt"; then
		fail "agent chart with $* does not explain: $message"
	fi
}
agent_want_fail agent-bad-provider 'config.provider is required (aws or gcp), got "azure"' \
	--set config.provider=azure
agent_want_fail agent-gcp-no-project 'config.gcpProject is required when config.provider is gcp and config.gcpOrg.enabled is false' \
	--set config.provider=gcp
agent_want_fail agent-gcp-org-no-parent 'config.gcpOrg.parent must be organizations/<id> or folders/<id>' \
	--set config.provider=gcp --set config.gcpOrg.enabled=true
agent_want_fail agent-gcp-org-bad-parent 'config.gcpOrg.parent must be organizations/<id> or folders/<id>' \
	--set config.provider=gcp --set config.gcpOrg.enabled=true --set config.gcpOrg.parent=projects/123
agent_want_fail agent-aws-org-on-gcp 'config.awsOrg.enabled requires config.provider aws' \
	--set config.provider=gcp --set config.gcpProject=p --set config.awsOrg.enabled=true
agent_want_fail agent-gcp-org-on-aws 'config.gcpOrg.enabled requires config.provider gcp' \
	--set config.provider=aws --set config.gcpOrg.enabled=true --set config.gcpOrg.parent=organizations/1
render cloudpam-agent "$AGENT" agent-gcp-project --set config.accountId=1 --set config.agentName=a --set apiKey=x \
	--set config.provider=gcp --set config.gcpProject=net-prod
grep -q '^    gcp_project: "net-prod"$' "$OUT/agent-gcp-project-configmap.yaml" || fail "gcp single-project chart does not render gcp_project"

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

for rendered in "$OUT"/server-default-all.yaml "$OUT"/server-full-all.yaml; do
	grep -q 'name: vector$' "$rendered" && fail "$rendered has a vector sidecar while vector.enabled is false"
done
sv=("$OUT/server-vector-configmap.yaml" "$OUT/server-vector-deployment.yaml")
want_once CLOUDPAM_AUDIT_SYSLOG_ADDR "${sv[@]}"
want_once CLOUDPAM_AUDIT_SYSLOG_NETWORK "${sv[@]}"
grep -q '^  CLOUDPAM_AUDIT_SYSLOG_ADDR: 127.0.0.1:5514$' "${sv[0]}" || fail "vector render does not send audit events to 127.0.0.1:5514"
grep -q '^  CLOUDPAM_AUDIT_SYSLOG_NETWORK: tcp$' "${sv[0]}" || fail "vector render does not send audit events over tcp"
grep -q '^address = "127.0.0.1:5514"$' "$OUT/server-vector.toml" || fail "default vector config does not listen on 127.0.0.1:5514"
grep -q '^      initContainers:$' "${sv[1]}" || fail "vector render has no initContainers"
grep -q '^        restartPolicy: Always$' "${sv[1]}" || fail "vector sidecar is not a native sidecar (restartPolicy: Always)"
grep -q 'name: cloudpam-cloudpam-server-vector$' "${sv[1]}" || fail "vector render does not mount the vector ConfigMap"
grep -q 'checksum/vector:' "${sv[1]}" || fail "vector render does not roll the pod on a config change"
grep -q '^        startupProbe:$' "${sv[1]}" || fail "vector sidecar has no startupProbe to hold the server until it listens"

want_once CLOUDPAM_API_KEY "${af[@]}"
want_absent CLOUDPAM_AGENT_NAME "${af[@]}"
grep -q '^    agent_name: ' "${af[0]}" || fail "agent.yaml has no agent_name"
grep -q '^    aws_org:' "${af[0]}" || fail "agent.yaml has no aws_org block"
grep -q '^    agent_name: "cluster-a-discovery"$' "$OUT/agent-explicit-configmap.yaml" ||
	fail "configured agent_name did not render"

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
