# Create a cloud account

Create a cloud account registers a provider account (AWS account, GCP project, and so on) that pools can later be assigned to, from the Cloud Accounts page or the API, and refuses a duplicate key.

## Sub-features

- `account-open-form` shows the create form from the `New Account` button.
- `account-create` persists a key, name, and provider and lists the account.
- `account-reject-duplicate` refuses a second account with the same key.
- `account-create-api` creates the same shape from the API.
- `account-delete` removes an account after a browser `confirm()`.

## How to get to it (user POV)

- Choose `Cloud Accounts` in the sidebar, then the `New Account` button.
- `POST /api/v1/accounts` with `{"key","name","provider"}` from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present: account `prod-platform` with key `aws:111111111111`.
- No account with key `aws:<id>` exists, where `<id>` is the last 12 digits of `Date.now()`.

- **Open form.** After `login()`, click `getByRole('link', { name: 'Cloud Accounts' })`, wait for `getByRole('heading', { name: 'Cloud Accounts' })`, click `getByRole('button', { name: 'New Account' })`. A form with placeholders `e.g., aws:123456789012` (key) and `e.g., Production AWS` (name) appears.
- **Create.** `getByPlaceholder('e.g., aws:123456789012').fill('aws:<id>')`, `getByPlaceholder('e.g., Production AWS').fill('verify-acct-<id>')`, click `getByRole('button', { name: 'Create Account' })`. Toast `Account created` appears, the form closes, and the table has a row for `verify-acct-<id>`.
- **Confirm persistence.** `api('GET', '/api/v1/accounts')` returns `200` and includes an account with `key` `aws:<id>`.
- **Reject duplicate.** Open the form, fill key `aws:111111111111` and name `dup`, click `Create Account`. An error toast `account key already exists` appears and the form stays open (`Cancel` closes it). From a terminal, `cloudpam-verify api POST /api/v1/accounts '{"key":"aws:111111111111","name":"dup","provider":"aws"}'` returns `409` with body `{"error":"account key already exists","detail":"aws:111111111111"}` on every store; the helper exits 1, as it does for any non-2xx.
- **API entry.** Run `cloudpam-verify api --save account-create POST /api/v1/accounts '{"key":"gcp:verify-<ts>","name":"verify-gcp-<ts>","provider":"gcp"}'`. Status `201`; body has the new `id` and `key`.
- **Delete.** In `getByRole('row', { name: /verify-acct-<id>/ })`, click `getByRole('button', { name: 'Delete' })`; the `confirm()` dialog `Delete account "verify-acct-<id>"?` is auto-accepted by the journey runner. Toast `Account deleted` appears, the row goes, and `GET /api/v1/accounts` no longer includes the key.
- **Proof.** `shot('02-account-created')` and `aria('02-account-created')` with the new row visible, plus the saved API responses. The proven journey is `journeys/create-account.cjs`.

## Gotchas

- The account form labels are not associated with their inputs; use the placeholders above, not `getByLabel`.
- `key` is `provider:id` with a lowercase provider from `aws`, `gcp`, `azure`, `onprem`. An `aws` id is exactly 12 digits, a `gcp` id matches `^[a-z][a-z0-9-]{4,28}[a-z0-9]$`, an `azure` id is a UUID, and an `onprem` id is up to 64 letters, digits, `-` or `_`. The whole key is at most 100 characters. A bad key returns `400` `invalid account key "<key>": <reason>`, so `aws:<Date.now()>` (13 digits) fails.
- A reused key returns `409` `account key already exists`. On SQLite a soft-deleted account still holds its key (issue #268).
- Deleting an account that still owns pools needs `?force=true` on the API. The UI never sends it: after the `confirm()`, an error toast `account in use by pools: conflict` appears and the row stays.
- Toasts disappear after 3 seconds; wait for them right after the click.
