# Account credentials

An admin attaches an encrypted cloud credential to an AWS or GCP account from the Cloud Accounts page, so the server's own discovery sync can reach an account the host identity cannot. The modal shows only a non-secret summary, tests the stored credential against the provider, and clears it. The secret never comes back from the API.

## Sub-features

- `credential-open` opens the `Credentials` modal from an AWS or GCP account row, with the `None stored` state and a form fixed by the provider.
- `credential-save` stores a GCP service-account key (or an AWS role ARN plus optional external ID) and shows the summary (`client_email`, `project_id`, or `role_arn` and whether an external ID is set).
- `credential-summary-only` returns the summary from `GET /api/v1/accounts/{id}/credentials` with no payload or private key.
- `credential-test` runs the stored credential against the provider and shows the result (`ok` with the identity, or `failed` with a sanitized error) in the modal and on the credential.
- `credential-clear` deletes the credential after a browser `confirm()` and returns the modal to `None stored`.
- `credential-refuse-ephemeral-key` answers `409` when the server has no `CLOUDPAM_OIDC_ENCRYPTION_KEY`, and the modal tells the operator to set it.
- `credential-audit` writes `update`, `test`, and `delete` events with resource type `account_credential`.

## How to get to it (user POV)

- Choose `Cloud Accounts` in the sidebar, then the key icon `Credentials` button on an AWS or GCP account's row.
- `PUT /api/v1/accounts/{id}/credentials`, `GET`, `DELETE`, and `POST /api/v1/accounts/{id}/credentials/test` from a terminal.
- Choose `Discovery` in the sidebar and open `Where discovery credentials come from` in the setup guide for the four credential paths and the precedence rule.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present (`prod-platform`, provider aws).
- The server was launched with `CLOUDPAM_OIDC_ENCRYPTION_KEY` set. `launch` sets a fixed dev key unless one is exported first, and records it in `state/env` as `OIDC_ENCRYPTION_KEY`.
- No account with key `gcp:verify-cred-<id>` exists, where `<id>` is the last 8 digits of `Date.now()`.
- A throwaway RSA key from `node:crypto` (`generateKeyPairSync('rsa', { modulusLength: 2048 })`, exported as PKCS8 PEM) inside a service-account JSON with `type`, `project_id`, `private_key`, and `client_email`.

- **Create the GCP account.** After `login()`, click `getByRole('link', { name: 'Cloud Accounts' })`, click `New Account`, fill the key and name placeholders, `getByRole('form', { name: 'New Account' }).getByLabel('Provider').selectOption('gcp')`, click `Create Account`. Toast `Account created`; `api('GET', '/api/v1/accounts')` has the key.
- **Open.** In `getByRole('row', { name: /verify-gcp-<id>/ })`, click `getByRole('button', { name: 'Credentials' })`. `getByTestId('account-credential-modal')` appears, `getByTestId('credential-state')` reads `None stored`, and `getByLabel('Service account key JSON (required)')` is a textarea. `GET /api/v1/accounts/{id}/credentials` is `404` `no credential stored for this account`.
- **Save.** Fill the textarea with the service-account JSON and click `Save`. Toast `Credential saved`, the badge reads `Stored`, the modal shows `client_email` and `project_id`, and the textarea is empty. `page.content()` contains neither a base64 line of the key nor `BEGIN PRIVATE KEY`. `GET /api/v1/accounts/{id}/credentials` is `200` with `type`, `summary.client_email`, `summary.project_id`, no `last_test`, no `payload_encrypted`, and no `private_key`.
- **Test.** Click `Test`. `getByTestId('credential-test-result')` appears within 30 seconds reading `failed` with a sanitized error (`token: ...`, `endpoint unreachable`, or `timed out after 15s`), the badge reads `Failing`, and the same `GET` now has `last_test.status` `failed`. No key material in the result text or the page.
- **Clear.** Click `Clear`; the `confirm()` `Clear the stored credential for "verify-gcp-<id>"?` is auto-accepted. Toast `Credential cleared`, the badge reads `None stored`, the result block goes, and the `GET` is `404` again.
- **Audit.** `api('GET', '/api/v1/audit?resource_type=account_credential&limit=20')` lists events for the account id with actions `update`, `test`, and `delete`, each with `resource_name` equal to the account name and no key material in `changes`.
- **Refusal (API).** `cloudpam-verify api --save credential-bad-arn PUT /api/v1/accounts/1/credentials '{"type":"aws_assume_role","aws_assume_role":{"role_arn":"not-an-arn"}}'` returns `400` `invalid aws_assume_role.role_arn` with a `detail` naming the rule. `--save credential-bad-key PUT /api/v1/accounts/1/credentials '{"type":"gcp_service_account","gcp_service_account":{"key_json":"{}"}}'` returns `400` `invalid gcp_service_account.key_json` (fields are validated before the provider check, so a type that does not match the account is reported only once the payload is valid). A server launched without the key returns `409` with `CLOUDPAM_OIDC_ENCRYPTION_KEY` in the error; `launch` always sets a key, so prove that path with a hand-launched server.
- **Proof.** `shot('01-modal-none')`, `shot('02-modal-saved')` and `aria('02-modal-saved')`, `shot('03-modal-tested')` and `aria('03-modal-tested')`, `shot('04-modal-cleared')` and `aria('04-modal-cleared')`, plus the `GET` bodies and the audit events in `journey.log`. The proven journey is `journeys/account-credentials.cjs`.

## Gotchas

- The `Credentials` button exists only on rows whose provider is `aws` or `gcp`. Azure and `Other` accounts have none.
- The modal's form, `Test`, and `Clear` need `accounts:update`. A user with only `accounts:read` sees the summary and no actions.
- The GCP test reads the project named by the account's `external_id`, or the part of the key after `gcp:` when `external_id` is empty. A key whose `project_id` differs still saves, but the test reads the account's project.
- The test calls Google's real token endpoint. With no outbound network it fails fast as `endpoint unreachable`. With network it fails as `token: invalid_grant` or similar for a throwaway key. Either way the status is `failed`; wait up to 30 seconds.
- A save clears `last_test`. Test again after replacing a credential.
- `PUT` with both `aws_assume_role` and `gcp_service_account`, or with an unknown field, is `400`.
- Toasts disappear after 3 seconds; wait for them right after the click.
