# API key rate limit

An admin creates an API key with its own request rate on the API Keys page, the table shows that rate next to the key, and requests made with the key are refused with `429` once the rate is exceeded, whatever the server-wide limit is.

## Sub-features

- `keyrl-form` shows `Requests per second` and `Burst` on the create form to a caller holding `settings:write`, and hides them otherwise.
- `keyrl-create` stores the pair with the key and echoes it as `rate_limit` in the create response.
- `keyrl-table` shows `<rate>/s, burst <n>` in the `Rate limit` column, or `Default` for a key without one.
- `keyrl-enforce` refuses the key's requests with `429` and `Retry-After` once its bucket is empty, even on an instance started with `RATE_LIMIT_RPS=0`.
- `keyrl-reject` refuses a zero or negative value with `400`, and a caller without `settings:write` with `403`.

## How to get to it (user POV)

- Sidebar `API Keys` (Administration), or `/config/api-keys`; `Create Key` opens the form.
- `POST /api/v1/auth/keys` with a `rate_limit` block, and `GET /api/v1/auth/keys` to read it back.
- Any API call with `Authorization: Bearer <key>` exercises the limit.

## Driving it with cloudpam-verify

Preconditions:

- `cloudpam-verify launch` has completed; the browser starts signed out and `login()` signs in as the admin, who holds `settings:write`.
- The instance runs with `RATE_LIMIT_RPS=0` (launch sets it), so only a key's own limit can produce `429`.

- **Open the form.** After `login()`, `await page.goto(baseURL + '/config/api-keys')`, wait for `getByRole('heading', { name: 'API Keys' })`, click `getByRole('button', { name: 'Create Key' })`. The form shows `getByLabel('Requests per second')` and `getByLabel('Burst')` under `Rate limit (optional, ...)`.
- **Fill and create.** `getByPlaceholder('e.g. CI Pipeline').fill(name)`, `getByLabel('Requests per second').fill('0.2')`, `getByLabel('Burst').fill('2')`, `shot('01-create-form-filled')`, click `getByRole('button', { name: 'Create', exact: true })`. The text `API key created` appears with the plaintext key.
- **Table proof.** `getByRole('row', { name: /<name>/ }).getByText('0.2/s, burst 2')` is visible; a key created without the block shows `Default`. `shot` and `aria` here.
- **Stored proof.** `await api('GET', '/api/v1/auth/keys')` returns `200` and the key's `rate_limit` equals `{ requests_per_second: 0.2, burst: 2 }`; a key created without it has no `rate_limit`.
- **Enforcement.** From a terminal, `cloudpam-verify api --save key-create-throttled POST /api/v1/auth/keys '{"name":"throttled","scopes":["pools:read"],"rate_limit":{"requests_per_second":0.2,"burst":2}}'`, then three `curl -D - -H "Authorization: Bearer <key>" $BASE_URL/api/v1/pools`: `200`, `200`, then `429` with `Retry-After: 5` and `X-Ratelimit-Limit: 0.2`. Unauthenticated `GET /healthz` stays `200` because the server-wide limiter is off.
- **Refusals.** `cloudpam-verify api POST /api/v1/auth/keys '{"name":"bad","scopes":["pools:read"],"rate_limit":{"requests_per_second":0,"burst":2}}'` returns `400` `invalid rate_limit`.
- **Proof.** The proven journey is `journeys/api-key-rate-limit.cjs`.

## Gotchas

- A half-filled pair (one of the two inputs empty) is refused by the form itself with `A rate limit needs both requests per second and burst, each above zero`; no request is sent.
- Go canonicalizes response header names, so match `X-Ratelimit-Limit` and `X-Ratelimit-Remaining` case-insensitively.
- The limit is fixed at creation. To change it, create a new key and revoke the old one.
- A request to a public route (the SPA, `/healthz`, `/openapi.yaml`) that carries credentials is charged to the client address, not the key or user. With `RATE_LIMIT_RPS=0` that bucket is off; with it on, count those calls against the address when reasoning about `429`s.
- In the Nix dev shell `PLAYWRIGHT_BROWSERS_PATH` points into the read-only store and `launch` still runs `npx playwright install chromium`, which fails with `EACCES ... __dirlock`. Until the launcher skips the install when the browsers are already present, run `launch` inside `nix develop` with `PLAYWRIGHT_BROWSERS_PATH` set to a writable mirror: one directory per store entry (`chromium-1200`, `chromium_headless_shell-1200`, `ffmpeg-1011`), each holding symlinks to the store contents plus an empty `INSTALLATION_COMPLETE` file.
