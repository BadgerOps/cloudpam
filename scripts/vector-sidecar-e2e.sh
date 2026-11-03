#!/usr/bin/env bash
# Installs the server chart with vector.enabled=true into a throwaway kind
# cluster, signs in once, and checks that the login audit event comes out of
# the Vector sidecar as parsed CEF. Needs docker, kind, kubectl and helm.
#   IMAGE=ghcr.io/badgerops/cloudpam/server:<tag>  server image (default: chart appVersion)
#   KEEP_CLUSTER=1                                 leave a cluster this run created up (reach it with `kind export kubeconfig --name $CLUSTER`)
#   CLUSTER=<name>                                 kind cluster name; an existing cluster is reused and never deleted
set -euo pipefail

cd "$(dirname "$0")/.."

CLUSTER="${CLUSTER:-cloudpam-vector-e2e}"
RELEASE=cloudpam
NS=cloudpam-vector-e2e
PASSWORD=vector-e2e-password
PORT="${PORT:-18298}"
CHART=deploy/helm/cloudpam-server

image_args=()
if [ -n "${IMAGE:-}" ]; then
	image_args=(--set "image.repository=${IMAGE%:*}" --set "image.tag=${IMAGE##*:}")
fi

# A private kubeconfig keeps the caller's kubeconfig and current context untouched.
KUBECONFIG="$(mktemp)"
export KUBECONFIG

pf_pid=""
created=""
cleanup() {
	[ -n "$pf_pid" ] && kill "$pf_pid" 2>/dev/null || true
	if [ -n "$created" ] && [ "${KEEP_CLUSTER:-}" != 1 ]; then
		kind delete cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG" >/dev/null 2>&1 || true
	fi
	rm -f "$KUBECONFIG"
}
trap cleanup EXIT

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
	echo "reusing existing kind cluster $CLUSTER; it will not be deleted" >&2
	kind get kubeconfig --name "$CLUSTER" >"$KUBECONFIG"
else
	kind create cluster --name "$CLUSTER" --wait 120s --kubeconfig "$KUBECONFIG"
	created=1
fi

helm upgrade --install "$RELEASE" "$CHART" --namespace "$NS" --create-namespace --wait --timeout 5m \
	--set vector.enabled=true \
	--set storage.sqlite.persistence.enabled=false \
	--set config.CLOUDPAM_ADMIN_USERNAME=admin \
	--set secrets.CLOUDPAM_ADMIN_PASSWORD="$PASSWORD" \
	"${image_args[@]}"

pod="$(kubectl -n "$NS" get pod -l "app.kubernetes.io/instance=$RELEASE" -o jsonpath='{.items[0].metadata.name}')"
kubectl -n "$NS" port-forward "pod/$pod" "$PORT:8080" >/dev/null &
pf_pid=$!
for _ in $(seq 50); do curl -fs "http://127.0.0.1:$PORT/healthz" >/dev/null && break; sleep 0.2; done

status="$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
	-d "{\"username\":\"admin\",\"password\":\"$PASSWORD\"}" "http://127.0.0.1:$PORT/api/v1/auth/login")"
[ "$status" = 200 ] || {
	echo "FAIL: login returned $status" >&2
	exit 1
}

for _ in $(seq 30); do
	event="$(kubectl -n "$NS" logs "$pod" -c vector | grep '"deviceProduct":"CloudPAM"' | grep '"act":"login"' | tail -1 || true)"
	[ -n "$event" ] && break
	sleep 1
done
if [ -z "$event" ]; then
	echo "FAIL: no parsed login audit event in the vector sidecar's output" >&2
	kubectl -n "$NS" logs "$pod" -c server | grep -i syslog >&2 || true
	exit 1
fi
echo "$event"
echo "vector-sidecar-e2e: ok (login audit event parsed by the vector sidecar in pod $pod)"
