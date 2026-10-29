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
- No account with key `aws:<ts>` exists.

- **Open form.** After `login()`, click `getByRole('link', { name: 'Cloud Accounts' })`, wait for `getByRole('heading', { name: 'Cloud Accounts' })`, click `getByRole('button', { name: 'New Account' })`. A form with placeholders `e.g., aws:123456789012` (key) and `e.g., Production AWS` (name) appears.
- **Create.** `getByPlaceholder('e.g., aws:123456789012').fill('aws:<ts>')`, `getByPlaceholder('e.g., Production AWS').fill('verify-acct-<ts>')`, click `getByRole('button', { name: 'Create Account' })`. Toast `Account created` appears and the list shows `verify-acct-<ts>`.
- **Confirm persistence.** `api('GET', '/api/v1/accounts')` returns `200` and includes an account with `key` `aws:<ts>`.
- **Reject duplicate.** Run `cloudpam-verify api POST /api/v1/accounts '{"key":"aws:111111111111","name":"dup","provider":"aws"}'`. Status `400` with body `{"error":"conflict: constraint failed: UNIQUE constraint failed: accounts.key (2067)"}` (confirmed on SQLite; the in-memory store words it differently).
- **API entry.** Run `cloudpam-verify api --save account-create POST /api/v1/accounts '{"key":"gcp:verify-<ts>","name":"verify-gcp-<ts>","provider":"gcp"}'`. Status `201`; body has the new `id` and `key`.
- **Delete.** In the row for `verify-acct-<ts>`, click the delete control; the `confirm()` dialog is auto-accepted by the journey runner. Toast `Account deleted` appears and `GET /api/v1/accounts` no longer includes the key.
- **Proof.** `shot('account-created')` and `aria('account-created')` with the new row visible, plus the saved API responses.

## Gotchas

- The account form labels are not associated with their inputs; use the placeholders above, not `getByLabel`.
- `key` must be unique and is validated like a name; spaces or very long keys return `400`. A reused key also returns `400`, not `409`, and the message is the raw database constraint text; assert on `UNIQUE constraint failed: accounts.key`, not on the status alone.
- Deleting an account that still owns pools needs `?force=true` on the API; the UI dialog will surface the refusal as an error toast.
- This feature has no proven journey yet; the first run should save one to `journeys/create-account.cjs`.
