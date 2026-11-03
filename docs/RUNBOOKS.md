# CloudPAM Operations Runbooks

Day-two procedures for a running CloudPAM server. [DEPLOYMENT.md](DEPLOYMENT.md) covers installation. Each runbook names the files, endpoints, and log lines it relies on, so you can check the behaviour against the release you run.

| Runbook | Use it when |
|---------|-------------|
| [1. Backup and restore](#1-backup-and-restore) | Before any upgrade, and on a schedule |
| [2. Upgrade and rollback](#2-upgrade-and-rollback) | A new release is out |
| [3. Rotate the encryption key](#3-rotate-the-encryption-key) | `CLOUDPAM_OIDC_ENCRYPTION_KEY` is due for rotation or was exposed |
| [4. OIDC troubleshooting](#4-oidc-troubleshooting) | SSO login fails or gives everyone the wrong role |
| [5. Audit forwarding](#5-audit-forwarding) | Events are not reaching the SIEM, or arrive without a source address |
| [6. Lockout recovery for the last admin](#6-lockout-recovery-for-the-last-admin) | Nobody can sign in as an administrator |

Conventions used below:

- `cloudpam` is the server binary. In the release image (`ghcr.io/badgerops/cloudpam/server`) it is the entrypoint and the only executable. The image is built on `cgr.dev/chainguard/static`, so it has no shell and no `env`, and `kubectl exec` or `podman exec` can run `cloudpam` only.
- `<control>` is the upgrade control directory, `<data>` is the SQLite directory, `$DATABASE_URL` is the PostgreSQL connection string.
- API calls need an admin session cookie or an API key (`Authorization: Bearer cpam_...`). Permissions are named per endpoint.

## 1. Backup and restore

Take a backup before every upgrade and before runbooks 2, 3, and 6. Migrations are forward-only, so a backup is the only way back across a schema change.

The backup contains ciphertexts that only the current `CLOUDPAM_OIDC_ENCRYPTION_KEY` opens (OIDC client secrets, the AI provider API key). Keep the key for as long as you keep the backups it protects, in your secret store, not next to the backup files.

### 1.1 SQLite

The server opens the database with `PRAGMA journal_mode=WAL` (`internal/storage/sqlite/sqlite.go`). A WAL database is three files: `cloudpam.db`, `cloudpam.db-wal`, and `cloudpam.db-shm`. Committed transactions sit in the `-wal` file until a checkpoint folds them into the main file, so copying `cloudpam.db` alone while the server runs loses recent writes and can copy a torn page. Use SQLite's own backup, which produces one consistent file with the WAL applied.

Find the file from the DSN. The default is `file:cloudpam.db?cache=shared&_fk=1` (relative to the working directory). The Helm chart sets `file:/data/cloudpam.db?cache=shared&_fk=1` on a persistent volume mounted at `/data`.

Online backup, server running:

```bash
ts="$(date -u +%Y%m%dT%H%M%SZ)"
sqlite3 <data>/cloudpam.db ".backup '<backups>/cloudpam-${ts}.db'"
sqlite3 "<backups>/cloudpam-${ts}.db" "PRAGMA integrity_check; SELECT schema_version FROM schema_info; SELECT COUNT(*) FROM pools;"
```

`.backup` reads a snapshot and does not block the server's writer. The second command must print `ok`, the schema version, and a plausible pool count. `VACUUM INTO '<path>'` is an equivalent single-file backup on SQLite 3.27 or later.

Offline backup with the server stopped. Copy all three files together, or checkpoint first and copy the main file:

```bash
sqlite3 <data>/cloudpam.db "PRAGMA wal_checkpoint(TRUNCATE);"
cp <data>/cloudpam.db "<backups>/cloudpam-${ts}.db"
```

On Kubernetes the server pod cannot run `sqlite3`. Run a helper pod that mounts the same claim on the same node, then run the online backup inside it. A `ReadWriteOnce` volume can be mounted by a second pod on the node where the server pod runs; `nodeName` pins it there. The pod has no readiness probe, so `Ready` only means the container started while `apk add` is still running; the loop below waits until `sqlite3` answers instead.

```bash
node="$(kubectl -n <ns> get pod -l app.kubernetes.io/instance=<release> -o jsonpath='{.items[0].spec.nodeName}')"
claim="$(kubectl -n <ns> get pod -l app.kubernetes.io/instance=<release> -o jsonpath='{.items[0].spec.volumes[?(@.persistentVolumeClaim)].persistentVolumeClaim.claimName}')"
kubectl -n <ns> apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata: {name: cloudpam-backup}
spec:
  nodeName: ${node}
  restartPolicy: Never
  containers:
  - name: sqlite
    image: alpine:3
    command: [sh, -c, "apk add --no-cache sqlite >/dev/null && sleep 3600"]
    volumeMounts: [{name: data, mountPath: /data}]
  volumes:
  - name: data
    persistentVolumeClaim: {claimName: ${claim}}
EOF
until kubectl -n <ns> exec cloudpam-backup -- sqlite3 -version >/dev/null 2>&1; do sleep 2; done
kubectl -n <ns> exec cloudpam-backup -- sqlite3 /data/cloudpam.db ".backup '/data/cloudpam-backup.db'"
kubectl -n <ns> cp cloudpam-backup:/data/cloudpam-backup.db "./cloudpam-${ts}.db"
kubectl -n <ns> exec cloudpam-backup -- rm /data/cloudpam-backup.db
kubectl -n <ns> delete pod cloudpam-backup
```

Restore:

1. Stop the server (`systemctl stop cloudpam.service`, or `kubectl scale deploy/<name> --replicas=0`; the chart runs one replica with `Recreate`).
2. Move the live files aside. Remove or rename `cloudpam.db`, `cloudpam.db-wal`, and `cloudpam.db-shm` together. A leftover `-wal` from the old database is not valid for the restored file.
3. Copy the backup to the DSN path and give it the server's ownership (the image runs as `nonroot`, uid 65532).
4. Start the server. Migrations newer than the backup apply at startup; check `GET /readyz` and the migration status:

```bash
SQLITE_DSN='file:<data>/cloudpam.db?cache=shared&_fk=1' cloudpam -migrate status
```

Restoring a backup taken by a newer release into an older binary is not supported. The older binary does not refuse the newer schema, it just runs against columns it does not know. Match the binary to the backup's `app_version` from `schema_info`, or upgrade the binary first.

### 1.2 PostgreSQL

Use `pg_dump` in custom format, which `pg_restore` can list and restore selectively:

```bash
ts="$(date -u +%Y%m%dT%H%M%SZ)"
pg_dump --format=custom --file="cloudpam-${ts}.dump" "$DATABASE_URL"
pg_restore --list "cloudpam-${ts}.dump" | grep -c 'TABLE DATA'
```

The dump holds every table CloudPAM owns, `schema_migrations` and `schema_info` included, so a restore carries the schema version with it. Managed services (RDS, Cloud SQL) add snapshots and point-in-time recovery on top; keep both, because a logical dump restores into a fresh database on any provider.

Restore into the database the server uses:

1. Stop every replica. `pg_restore --clean` drops and recreates tables, and a running server would fail requests or reapply migrations against a half-restored schema.
2. Restore, then compare the version with the dump:

```bash
pg_restore --clean --if-exists --no-owner --dbname="$DATABASE_URL" "cloudpam-${ts}.dump"
psql "$DATABASE_URL" -c "SELECT MAX(version) FROM schema_migrations; SELECT schema_version, app_version FROM schema_info;"
```

3. Start the server and check `GET /readyz`. A binary built with `-tags postgres` prints the same status as `cloudpam -migrate status` with `DATABASE_URL` set.

### 1.3 What to test

Restore a backup into a scratch server quarterly and sign in. Test the OIDC provider (`POST /api/v1/settings/oidc/providers/{id}/test`) and the AI provider (`POST /api/v1/settings/ai/test`) on the restored copy, because those prove the encryption key you kept matches the ciphertexts you restored.

## 2. Upgrade and rollback

Two upgrade paths exist. Both start with a backup (runbook 1), because migrations apply automatically at startup and are forward-only.

Release image tags carry no `v`: `0.23.11`, `0.23`, and `latest`. `latest` moves on every published release, pre-releases included. The in-app update check ignores drafts and pre-releases and uses the GitHub release list (`CLOUDPAM_RELEASE_REPO`, default `BadgerOps/cloudpam`; `CLOUDPAM_GITHUB_API_ROOT` for a mirror).

Whichever path you use, set `APP_VERSION` to the running version. The Helm chart passes `image.tag` as `APP_VERSION`. A host unit must pass it explicitly. Without it the server reports `current_version: dev` and `update_available: true` forever, because `dev` compares as `0.0.0`.

### 2.1 Plain image bump

Helm:

```bash
helm upgrade <release> deploy/helm/cloudpam-server --reuse-values --set image.tag=0.23.11
kubectl -n <ns> rollout status deploy/<name>
```

Or bump the chart, whose `appVersion` is the default tag. A SQLite install rolls out with `Recreate`, so expect a short outage; PostgreSQL installs roll.

On a Podman or Docker host, change the tag in the unit or compose file, pull, and restart.

Verify after either:

```bash
curl -s http://<host>/healthz
curl -s http://<host>/readyz
curl -s -H "Authorization: Bearer cpam_..." http://<host>/api/v1/updates | jq '{current_version, latest_version, update_available}'
```

The startup log reports the migration result; `cloudpam -migrate status` prints `schema_version=<n> applied=<n> latest=<n> app_version=<version>`.

To roll back, first check whether the release you are leaving added a migration (compare `migrations/` between the two tags, or the release notes). When it did not, redeploy the previous tag the same way. When it did, the older binary starts against the newer schema without complaint and misbehaves later, so stop the server, restore the pre-upgrade backup (runbook 1, steps 1 to 3) while it is stopped, and only then redeploy the previous tag. Do not start the old image first and restore afterwards; every request served in between runs on the incompatible schema.

### 2.2 Host-managed upgrade through the control directory

This path lets an admin trigger the upgrade from the Updates page on a host that runs the server as a Podman container under systemd. The server writes a request file; a systemd path unit on the host runs the upgrade script; the script reports progress through a status file that the UI polls.

The pieces, all in this repository:

| Piece | Path | Role |
|-------|------|------|
| Request and status handlers | `internal/api/update_handlers.go` | `GET /api/v1/updates` (release check, 1 h cache, `?force=true` refreshes), `POST /api/v1/updates/upgrade` (writes the request), `GET /api/v1/updates/status`, `POST /api/v1/updates/status/ack`. `settings:read` for the GETs, `settings:write` for the POSTs. |
| Path unit | `deploy/cloudpam-upgrade.path` | Watches `/var/lib/cloudpam/control/upgrade-requested` |
| Service unit | `deploy/cloudpam-upgrade.service` | Runs the script as root with `DATA_DIR=/var/lib/cloudpam/control` |
| Script | `scripts/cloudpam-upgrade.sh` | Pulls the image, records it in `image.env`, restarts `cloudpam.service`, waits for `/healthz`, writes the status file, removes the request |

Files in the control directory:

- `upgrade-requested`: JSON with `upgrade_id`, `target_version`, `target_image_tag`, `target_release_tag`, `release_url`, `requested_at`, `requested_by`. Written by the server, removed by the script.
- `upgrade-status.json`: `status` (`running`, `completed`, `failed`), `step` of `total_steps`, `progress`, `message`, `updated_at`, plus the request fields. Written by the script.
- `upgrade-status-ack.json`: written by the server when an admin dismisses a completed upgrade. A completed status also expires from the UI ten minutes after `updated_at`.
- `image.env`: one line, `CLOUDPAM_IMAGE=<repository>:<tag>`, the reference the script last pulled. Written by the script after a successful pull, before the restart.

Set-up requirements the repository does not enforce for you:

1. **One directory, two defaults.** The server reads `CLOUDPAM_CONTROL_DIR`, then `CLOUDPAM_UPGRADE_DATA_DIR`, then falls back to `/var/lib/cloudpam-control`. The units and the script use `/var/lib/cloudpam/control`. Those are different paths. Set `CLOUDPAM_CONTROL_DIR=/var/lib/cloudpam/control` on the server and bind-mount that host directory into the container at the same path. The directory must be writable by the container user (uid 65532 in the release image).
2. **The service must start what the script pulled.** The script pulls `ghcr.io/badgerops/cloudpam/server:<target_image_tag>` (the `SERVER_IMAGE` variable overrides the repository), writes that reference to `image.env`, and restarts `cloudpam.service`. It does not edit the unit, so the unit must read `image.env` and start the image it names:

   ```ini
   [Service]
   Environment=CLOUDPAM_IMAGE=ghcr.io/badgerops/cloudpam/server:0.23.11
   EnvironmentFile=-/var/lib/cloudpam/control/image.env
   ExecStart=/usr/bin/podman run --rm --name cloudpam ... ${CLOUDPAM_IMAGE}
   ```

   `Environment=` is the tag for a host that has never upgraded; once `image.env` exists its value wins. Do not run `:latest` with a pull on start instead. `latest` moves on every published release, pre-releases included, while the update check offers stable releases only, so a `:latest` unit can start a pre-release and still report the stable target as `completed`. With a pinned tag and no `EnvironmentFile`, the upgrade reports `completed` while the old version keeps running.
3. **Outbound HTTPS to the GitHub API** from the server for the release check, and to `ghcr.io` from the host for the pull. Unauthenticated GitHub API calls are rate-limited per source address; the server caches the release list for an hour and serves stale data with `warning: using stale release metadata` when a refresh fails.
4. `python3`, `podman`, `curl`, and `systemctl` on the host, which the script calls.

Install:

```bash
install -D -m 0755 scripts/cloudpam-upgrade.sh /opt/cloudpam/scripts/cloudpam-upgrade.sh
install -m 0644 deploy/cloudpam-upgrade.path deploy/cloudpam-upgrade.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now cloudpam-upgrade.path
```

Trigger from the Updates page, or:

```bash
curl -s -X POST -H "Authorization: Bearer cpam_..." http://<host>/api/v1/updates/upgrade
curl -s -H "Authorization: Bearer cpam_..." http://<host>/api/v1/updates/status
```

The trigger returns `202` with `status: upgrade_requested` and writes an audit event with action `update` and resource type `system`. Watch the host side with `journalctl -fu cloudpam-upgrade.service`.

Symptoms and causes:

| Symptom | Cause | Fix |
|---------|-------|-----|
| Status stays `idle` after the trigger; the UI says it is waiting for the host | The request file landed in a directory nothing watches | Check `ls /var/lib/cloudpam/control/` on the host and `systemctl status cloudpam-upgrade.path`. Fix requirement 1, or enable the path unit. |
| `409 upgrade already in progress` | A previous run left `status: running`, usually because the script was killed | Read `journalctl -u cloudpam-upgrade.service`, then delete `upgrade-status.json` and any stale `upgrade-requested` |
| `failed` at step 1, `Failed to pull ...` | The host cannot reach `ghcr.io`, or the tag does not exist | Pull the same reference by hand. Scripts older than this runbook pulled a `v`-prefixed tag that the registry never had; update the script. |
| `failed` at step 2 | `systemctl restart cloudpam.service` failed | `journalctl -u cloudpam.service` |
| `failed` at step 3, `Health check failed after 30s` | The new container did not answer `/healthz` in `HEALTH_TIMEOUT` seconds, often a failed migration or a missing environment variable | `journalctl -u cloudpam.service`, then roll back |
| `completed` but the version did not change | Requirement 2 is not met | Compare `cat /var/lib/cloudpam/control/image.env` with the image `podman ps` shows; add the `EnvironmentFile` and `${CLOUDPAM_IMAGE}` from requirement 2 and restart |
| `could not fetch releases` from `GET /api/v1/updates` | No GitHub API access, or rate-limited | Check egress; set `CLOUDPAM_GITHUB_API_ROOT` to a mirror if you run one |

To roll back on the host, apply the schema rule from 2.1 before anything restarts. Check whether the release you are leaving added a migration. If it did, `systemctl stop cloudpam.service` and restore the pre-upgrade backup (runbook 1, steps 1 to 3) while the service is stopped. Then write the previous reference into `image.env` (`CLOUDPAM_IMAGE=ghcr.io/badgerops/cloudpam/server:<previous>`), pull it, and `systemctl restart cloudpam.service`. The old image must not start until the schema it expects is in place.

## 3. Rotate the encryption key

`CLOUDPAM_OIDC_ENCRYPTION_KEY` is a 32-byte key, hex-encoded (64 characters). `internal/secrets` uses it for AES-256-GCM, storing each value as hex-encoded nonce plus ciphertext. There is no key identifier in the ciphertext and no re-encryption command. Rotating the key therefore means re-entering every protected value under the new key.

Values the key protects on current releases:

| Value | Where it is stored | How to re-enter |
|-------|--------------------|-----------------|
| OIDC client secrets | `oidc_providers.client_secret_encrypted` | `PATCH /api/v1/settings/oidc/providers/{id}` with `{"client_secret": "..."}`, or Configuration, SSO, edit provider |
| AI provider API key | `settings` row with key `ai_provider` | `PATCH /api/v1/settings/ai` with `{"api_key": "..."}`, or Configuration, AI Provider |
| Discovery account credentials, where the release stores them | `account_credentials.payload_encrypted` | Save the credential again on the account |

Procedure:

1. **Inventory.** `GET /api/v1/settings/oidc/providers` lists the providers. `GET /api/v1/settings/ai` shows `sources.api_key: database` when a key is stored. On the database, `SELECT COUNT(*) FROM account_credentials` tells you whether discovery credentials exist.
2. **Collect the plaintexts** from their sources of truth. CloudPAM never returns a stored secret. If an IdP client secret cannot be retrieved, create a new one at the IdP now and plan to enter that.
3. **Back up** (runbook 1) and keep the old key with the backup.
4. **Generate the new key** and put it in the secret store that feeds the server (`secrets.CLOUDPAM_OIDC_ENCRYPTION_KEY` or `existingSecret` in the Helm chart):

```bash
openssl rand -hex 32
```

5. **Restart every replica.** The key is read once at startup (`parseOIDCEncryptionKey` in `cmd/cloudpam/main.go`). With several replicas on PostgreSQL, stop all before starting any; during a rolling restart a replica on the new key cannot read values the old key wrote, and the reverse, so SSO logins and AI calls fail intermittently until every replica runs the new key.
6. **Re-enter each value** from the table above. The AI endpoint rule applies, so a `PATCH` that changes or clears the stored endpoint while a key is stored must carry `api_key` or `clear_api_key` in the same request.
7. **Verify.** `POST /api/v1/settings/oidc/providers/{id}/test` must return `success: true`; sign in through each provider. `POST /api/v1/settings/ai/test` must report `verified`. Run a discovery sync for each account with stored credentials. The server log must show no `cannot be decrypted` or `failed to decrypt client secret` lines.

Until step 6 completes, the symptoms are `500 failed to decrypt client secret` on OIDC login and on the provider test, `stored api key cannot be decrypted` from the AI settings, and `stored credential cannot be decrypted` on discovery. The same symptoms appear when the server was started without the variable: it then logs `CLOUDPAM_OIDC_ENCRYPTION_KEY not set`, generates a key for that process only, and every restart orphans whatever was saved. The fix is the same: set the key, restart, re-enter.

If the old key was exposed, rotate the secrets themselves at the IdP and the AI vendor too. Backups hold the old ciphertexts, and the old key opens them.

## 4. OIDC troubleshooting

Where the evidence is:

- `POST /api/v1/settings/oidc/providers/{id}/test` (the Test button) runs discovery with the stored secret and returns `{"success": false, "message": "discovery failed: ..."}` with the library's error. It tests discovery only; a wrong client secret passes here and fails at the callback.
- The callback (`GET /api/v1/auth/oidc/callback`) answers errors as JSON in the browser tab, with `error` and `detail`. When the IdP itself reports an error, the callback redirects to `/?error=<code>`; read the code from the address bar, the login page does not display it.
- Audit events: `oidc_provision` (resource `user`, status 201) on first login, `login` (resource `session`) on success, `login_failed` for a disabled account. Provider changes are `create`, `update`, `delete` on `oidc_provider`.
- Server logs carry a request ID that the JSON error omits; search by path `/api/v1/auth/oidc/callback` and status.

### 4.1 Discovery failures

`NewProvider` in `internal/auth/oidc/provider.go` fetches `<issuer_url>/.well-known/openid-configuration` and requires the document's `issuer` to equal the configured `issuer_url` byte for byte.

| Message contains | Cause | Fix |
|------------------|-------|-----|
| `issuer did not match the issuer returned by provider` | `issuer_url` differs from what the IdP publishes (trailing slash, `http` vs `https`, missing tenant or realm path) | Copy the `issuer` value out of the discovery document and store exactly that. Examples: Entra ID `https://login.microsoftonline.com/<tenant-id>/v2.0`, Keycloak `https://<host>/realms/<realm>`, Okta `https://<org>.okta.com/oauth2/default` or `https://<org>.okta.com` depending on the authorization server |
| `404` or an HTML body | `issuer_url` points at a login page, not the issuer root | Same as above |
| `x509: certificate signed by unknown authority` | The IdP uses a private CA | Mount the CA bundle and point Go at it with `SSL_CERT_FILE=/path/ca.pem` (or `SSL_CERT_DIR`) on the server |
| `dial tcp ... i/o timeout` or `no such host` | No egress or DNS from the server to the IdP | Fix network policy or DNS; the server, not the browser, performs discovery and token exchange |

### 4.2 Redirect URI mismatch

The `redirect_uri` CloudPAM sends is the value of `CLOUDPAM_OIDC_CALLBACK_URL`, unchanged. When the variable is empty, no `redirect_uri` is sent and the IdP falls back to its registered URI or rejects the request (`redirect_uri_mismatch`, Entra `AADSTS50011`, Keycloak `Invalid parameter: redirect_uri`). The IdP shows that error on its own page before any redirect.

The fix is to register `https://<public-host>/api/v1/auth/oidc/callback` at the IdP, set `CLOUDPAM_OIDC_CALLBACK_URL` to the identical string (scheme, host, port, path), and restart the server. Behind an ingress, the public host is the ingress host, not the pod or node address.

`403 invalid state` at the callback means the browser did not send the `oidc_state` cookie that the login step set. The cookie is scoped to the host where login started, to the path `/api/v1/auth/oidc/`, lives ten minutes, and is marked `Secure` when the login request arrived over TLS or with `X-Forwarded-Proto: https`. Causes, in order of frequency:

- Login started on one host (an internal address, or `http://localhost:8080`) and the IdP sent the browser back to the registered public host. Start and finish on the public host.
- The login was initiated over HTTPS through the proxy and the callback arrived over plain HTTP on the server, so the `Secure` cookie was withheld. Send `X-Forwarded-Proto` from the proxy, and make the callback URL `https`.
- More than ten minutes passed on the IdP's login page.

### 4.3 Token exchange failures

`401 token exchange failed` with the detail:

- `oauth2: ... invalid_client` or `unauthorized_client`: the client secret is wrong, usually rotated at the IdP. Re-enter it (runbook 3, step 6). A `500 failed to decrypt client secret` instead means the encryption key changed; runbook 3.
- `no id_token in response`: the IdP application is a plain OAuth2 client, not an OpenID Connect one, or the `openid` scope is not granted. CloudPAM always requests `openid`; enable OIDC on the application.
- `verify id_token: ... token is expired` or `issued in the future`: clock skew between the server and the IdP beyond the library's tolerance. Fix NTP on the server host.

### 4.4 Group mapping that maps nobody

How the mapping works (`internal/auth/oidc/claims.go`, `MapRole`, and the callback):

- `role_mapping` is a map from IdP group name to a built-in role name: `admin`, `operator`, `viewer`, or `auditor`. A custom role name is not valid here and is ignored.
- The groups come from the `groups` claim of the ID token only. Claims served by the userinfo endpoint are not fetched.
- Every matching group contributes its role; the highest wins. With no match, `default_role` applies. With no match and no valid `default_role`, a newly provisioned user becomes `viewer` and an existing user keeps the role they have.
- Roles sync on every login, but only when a mapping matches. Removing a user from all mapped groups does not demote them unless `default_role` is set.
- Users are matched by issuer and subject. A local user with the same email is not linked, and a username collision provisions the new user with a `_<first eight characters of sub>` suffix.

The symptoms are that every SSO user lands as `viewer`, and an admin added to the admin group is still `viewer` after signing in again.

Checks:

1. Capture an ID token for an affected user (the IdP's token debugger, or a `jwt.io` paste of the `id_token` from a manual authorization code flow) and look for a `groups` array. If it is absent, the IdP is not emitting it:
   - Entra ID: add the `groups` optional claim to the ID token under Token configuration. It emits group object IDs by default, not display names; map the IDs.
   - Okta: add a `groups` claim to the authorization server with a filter that matches your groups, include it in the ID token, and add `groups` to the provider's `scopes` in CloudPAM.
   - Keycloak: add a Group Membership mapper to the client scope with "Add to ID token" on and "Full group path" off, otherwise names arrive as `/parent/group`.
   - Google Workspace: Google issues no `groups` claim in the ID token. Mapping cannot work; use `default_role`.
2. Compare the claim values with the mapping keys character for character. Case and whitespace matter.
3. Confirm each mapped value is one of the four built-in role names.
4. `403 user not provisioned` means `auto_provision` is off for the provider and the user has never signed in. Turn it on, or expect first logins to fail.

## 5. Audit forwarding

CloudPAM forwards audit events as CEF over syslog when `CLOUDPAM_AUDIT_SYSLOG_ADDR` is set (`host:port`). `CLOUDPAM_AUDIT_SYSLOG_NETWORK` is `udp` (default) or `tcp`, without TLS; `CLOUDPAM_AUDIT_SYSLOG_APP_NAME` defaults to `cloudpam`. Every event is written to the local audit store first and then sent to the sink. Delivery is synchronous, one connection per event, with a two-second dial timeout, no queue, and no retry (`internal/audit/forwarding.go`, `internal/audit/cef.go`). A failed send is logged and the request proceeds.

### 5.1 Verify delivery

1. Startup log: `audit syslog forwarding enabled` with `address`, `network`, and `format=cef`. `audit syslog forwarding disabled` with an error means the network value was not `udp` or `tcp`.
2. Produce an event. Signing in writes `login` on resource `session`; `GET /api/v1/audit?limit=1` returns it with an `id`.
3. Look for the record at the receiver. The syslog message is RFC 5424 with facility `local4`, and the CEF header is `CEF:0|BadgerOps|CloudPAM|<version>|<action>|<resource_type>.<action>|<severity>|`. One record looks like:

```
<166>1 2026-11-02T10:15:04.123456Z cloudpam-1 cloudpam 1 login - CEF:0|BadgerOps|CloudPAM|0.23.11|login|session.login|3|act=login outcome=success rt=1793614504123 externalId=9c1d... suser=admin cs1Label=actor_type cs1=user src=203.0.113.5 request=req-... cs2Label=resource_type cs2=session cs3Label=resource_id cs3=... cn1Label=http_status cn1=200
```

   `externalId` is the audit event `id`, so a SIEM search for it proves the exact event arrived. Severity is 8 for 5xx, 6 for 4xx, 7 for `login_failed` and `account_locked`, 5 for `delete`, 1 for `read`, 3 otherwise.

4. Failures appear in the server log as `audit syslog forwarding failed` with `audit_event_id`, `action`, and the dial or write error. UDP sends rarely fail at the sender even when nothing listens, so the receiver is the only proof for UDP.

To test without a SIEM, run a listener on the server host and point the variable at it:

```bash
socat -u UDP-RECV:5514 STDOUT          # or: nc -lu 5514
CLOUDPAM_AUDIT_SYSLOG_ADDR=127.0.0.1:5514 cloudpam
```

TCP uses newline framing (non-transparent). For TLS or vendor APIs, forward to a local relay (rsyslog, Vector) and let it carry the credentials; see [OBSERVABILITY.md](OBSERVABILITY.md#20-generic-siem-forwarding).

### 5.2 What a missing `src` means

The CEF formatter writes `src=<ip>` only when the event's `ip_address` parses as an IP address.

- **Releases before 0.23.6** never populated `ip_address`, so every forwarded record lacked `src`, and brute-force clustering or impossible-travel rules in the SIEM had nothing to key on. Upgrading is the fix; the local audit store for that period has no address either, so it cannot be backfilled.
- **0.23.6 and later** (#293) resolve the client address once per request in `ClientIPMiddleware` and stamp it on every event emitted inside that request, including events logged as `system` such as the automatic `account_unlocked`. A record without `src` on these releases means the event did not originate in an HTTP request. There are no such emitters today, so treat it as a bug and report the action and resource type.

The address can be present and wrong. Behind a proxy without `CLOUDPAM_TRUSTED_PROXIES`, `src` is the proxy's address on every record. `X-Forwarded-For` is honoured only when the TCP peer is inside `CLOUDPAM_TRUSTED_PROXIES`, and the recorded address is then the rightmost entry that is not itself a trusted proxy. List every hop (load balancer and ingress ranges) and nothing wider. The `trusted_proxies` field on the Security settings page does not configure this middleware; the environment variable does.

## 6. Lockout recovery for the last admin

How local sign-in can stop working for an administrator, and how each case looks:

| Cause | Response at `POST /api/v1/auth/login` | Audit event |
|-------|----------------------------------------|-------------|
| Account lockout after `account_lockout_attempts` failures (0 disables it, the default) for `account_lockout_cooldown_minutes` (default 15, range 1 to 1440) | `423 account locked`, detail `try again after <time>` | `account_locked`, then `login_failed` with status 423 per attempt |
| Login rate limit, `login_rate_limit_per_minute` per client address (default 5) | `429` | none |
| Account deactivated | `403 account disabled` | `login_failed` with status 403 |
| Local authentication turned off (`local_auth_enabled: false`) while SSO is broken | `403 local authentication is disabled` | none |
| Forgotten password | `401 invalid credentials` | `login_failed` with status 401 |

`GET /api/v1/auth/users` shows `lockout_until`, `failed_login_attempts`, and `is_active` per user to admins.

Recovery, least invasive first:

1. **Wait.** A lockout clears itself at `lockout_until`; the next login attempt after that time resets the counters and writes `account_unlocked` as `system`. A rate limit clears within a minute.
2. **Another admin unlocks.** `POST /api/v1/auth/users/{id}/unlock`, admin only. Sessions and API keys of other admins keep working during a lockout, because lockout applies to password login only.
3. **An admin API key.** A key with the `*` scope, which admins can issue, can call the unlock endpoint and can turn local authentication back on (step 5). Keep one such key in the secret store as break-glass; this is the only recovery that needs no host access.
4. **Reset the password from the host.** This clears the lockout, reactivates the account, sets a new password, and revokes the user's sessions, without starting the HTTP server:

```bash
# Binary with -tags sqlite, server running or stopped (WAL mode permits a second writer)
printf '%s\n' '<new-password>' | SQLITE_DSN='file:<data>/cloudpam.db?cache=shared&_fk=1' cloudpam -reset-password admin

# Binary with -tags postgres
printf '%s\n' '<new-password>' | DATABASE_URL="$DATABASE_URL" cloudpam -reset-password admin

# Kubernetes: the image has no shell or env, so pass the password on stdin
printf '%s\n' '<new-password>' | kubectl -n <ns> exec -i deploy/<name> -- cloudpam -reset-password admin

# Podman
printf '%s\n' '<new-password>' | podman exec -i <container> cloudpam -reset-password admin
```

   The password must pass the policy (12 characters minimum by default). The command exits non-zero when the user does not exist or the store is unreachable.

5. **Turn local authentication back on.** With an API key that has `settings:write`, fetch the settings, flip the flag, and send the whole document back (the handler validates every field, so a partial body fails):

```bash
curl -s -H "Authorization: Bearer cpam_..." http://<host>/api/v1/settings/security > security.json
jq '.local_auth_enabled = true' security.json | curl -s -X PATCH -H "Authorization: Bearer cpam_..." -H 'Content-Type: application/json' --data @- http://<host>/api/v1/settings/security
```

   Without a key, change the stored document directly. The server reads it on every login, so no restart is needed:

```sql
-- SQLite
UPDATE settings SET value = json_set(value, '$.local_auth_enabled', json('true')) WHERE key = 'security';
-- PostgreSQL
UPDATE settings SET value = jsonb_set(value::jsonb, '{local_auth_enabled}', 'true')::text WHERE key = 'security';
```

6. **Create a new admin at startup.** `CLOUDPAM_ADMIN_USERNAME` and `CLOUDPAM_ADMIN_PASSWORD` create that user with the admin role on startup if the username does not exist. Set them to a new name such as `breakglass`, restart, sign in, repair the original account, then delete the extra user and unset the variables.

To prevent this, leave `account_lockout_attempts` at 0 unless policy requires it, keep the cooldown short, keep a break-glass admin API key, and keep two administrators. Nothing stops the last admin from deactivating or demoting themselves.
