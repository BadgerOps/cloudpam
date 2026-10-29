# Sign in

Sign in takes a local user from the login page into the app shell with a session cookie, after a one-time first-boot setup that creates the first admin.

## Sub-features

- `login-setup` creates the first admin on a fresh instance and clears `needs_setup`.
- `login-form` accepts a valid username and password and lands in the app shell.
- `login-reject` keeps the user on the login page with an error for a wrong password.
- `login-session` makes every later page and API call authenticated.

## How to get to it (user POV)

- Open any app URL on an instance that still needs setup; the SPA redirects to `/setup`.
- Open `/login` or any protected route while signed out; the SPA redirects to `/login`. Once setup is done, `/setup` also redirects to `/login`.
- `POST /api/v1/auth/setup` and `POST /api/v1/auth/login` are the same two steps from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- `cloudpam-verify launch` has completed (it already ran setup and logged in with curl; the browser starts signed out).
- `cloudpam-verify doctor` reports `needs_setup false`.

- **Setup is done.** Run `cloudpam-verify api GET /healthz`. Status `200`; body has `"needs_setup":false` and `"version":"cloudpam-verify-..."`. A second `cloudpam-verify api POST /api/v1/auth/setup '{"username":"x","password":"abcdefghijkl"}'` returns `403` `setup already completed`.
- **Open login.** In a journey, `await page.goto('/setup')` lands on `/login`. The page shows the heading `CloudPAM`, a `Username` field (placeholder `admin`), a `Password` field, and a `Sign In` button that is disabled until both fields have text.
- **Reject wrong password.** `await page.getByPlaceholder('admin').fill(username)`, `await page.locator('input[autocomplete="current-password"]').fill('wrong-password-123')`, click `getByRole('button', { name: 'Sign In' })`. A red box with the text `invalid credentials` appears above `Sign In` and the URL still ends in `/login`.
- **Sign in.** The journey helper `await login()` fills the real form with the run's credentials, clicks `Sign In`, and waits for `getByRole('navigation', { name: 'Main navigation' })`. The sidebar shows `Dashboard`, `Address Pools`, `Cloud Accounts`.
- **Session proof.** From the same journey, `await api('GET', '/api/v1/auth/me')` returns `200` with `auth_type` `session` and `user.username` `verify-admin`. From a terminal, `cloudpam-verify api --save auth-me GET /api/v1/auth/me` saves the same. A signed-in visit to `/login` lands on `/`.
- **Proof.** `await shot('02-signed-in')` and `await aria('02-signed-in')` after the navigation landmark is visible. The proven journey is `journeys/login.cjs`.

## Gotchas

- The login inputs have `<label>` text but no `htmlFor`; `getByLabel('Username')` finds nothing. Use the placeholder `admin` and `input[autocomplete="current-password"]`.
- `Sign In` stays disabled while either field is empty; a click on a disabled button times out instead of failing fast.
- `login()` works once per browser context: a signed-in visit to `/login` redirects to `/`, so the form is not there the second time. Call `context.clearCookies()` first to sign in again.
- Bootstrapping an admin with `CLOUDPAM_ADMIN_USERNAME` does not clear `needs_setup` until the server restarts; `POST /api/v1/auth/setup` clears it at once. The launch script uses setup for that reason.
- Every login attempt, right or wrong, spends one of 5 per-IP tokens that refill one every 12 seconds; `RATE_LIMIT_RPS=0` does not lift this. The sixth quick attempt gets `429` `too many login attempts`. `login()` waits out the 60-second `Retry-After` once and retries; a raw form fill in a journey does not, so test rejection once.
- Account lockout is off by default. When an admin turns it on, a locked account gets `423` `account locked` for 15 minutes.
- The server keeps 10 sessions per user and evicts the oldest. After ten browser logins in one run that is the launch's curl session; `cloudpam-verify api` and `doctor` log `curl session evicted (401); logging in again` and continue.
- The curl session from launch and the browser session are separate cookies; a journey must call `login()` itself.
