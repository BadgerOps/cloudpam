# Sign in

Sign in takes a local user from the login page into the app shell with a session cookie, after a one-time first-boot setup that creates the first admin.

## Sub-features

- `login-setup` creates the first admin on a fresh instance and clears `needs_setup`.
- `login-form` accepts a valid username and password and lands in the app shell.
- `login-reject` keeps the user on the login page with an error for a wrong password.
- `login-session` makes every later page and API call authenticated.

## How to get to it (user POV)

- Open any app URL on an instance that still needs setup; the SPA redirects to `/setup`.
- Open `/login` or any protected route while signed out; the SPA redirects to `/login`.
- `POST /api/v1/auth/setup` and `POST /api/v1/auth/login` are the same two steps from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- `cloudpam-verify launch` has completed (it already ran setup and logged in with curl; the browser starts signed out).
- `cloudpam-verify doctor` reports `needs_setup false`.

- **Setup is done.** Run `cloudpam-verify api GET /healthz`. Status `200`; body has `"needs_setup":false` and `"version":"cloudpam-verify-..."`.
- **Open login.** In a journey, `await page.goto('/login')`. The heading area shows the CloudPAM title, a `Username` field (placeholder `admin`), a `Password` field, and a `Sign In` button that is disabled until both fields have text.
- **Reject wrong password.** `await page.getByPlaceholder('admin').fill(username)`, `await page.locator('input[autocomplete="current-password"]').fill('wrong-password-123')`, click `getByRole('button', { name: 'Sign In' })`. An error box appears under the form and the URL still ends in `/login`.
- **Sign in.** The journey helper `await login()` fills the real form with the run's credentials, clicks `Sign In`, and waits for `getByRole('navigation', { name: 'Main navigation' })`. The sidebar shows `Dashboard`, `Address Pools`, `Cloud Accounts`.
- **Session proof.** From the same journey, `await api('GET', '/api/v1/auth/me')` returns `200` with `"username":"verify-admin"`. From a terminal, `cloudpam-verify api --save auth-me GET /api/v1/auth/me` saves the same.
- **Proof.** `await shot('signed-in')` and `await aria('signed-in')` after the navigation landmark is visible.

## Gotchas

- The login inputs have `<label>` text but no `htmlFor`; `getByLabel('Username')` finds nothing. Use the placeholder `admin` and `input[autocomplete="current-password"]`.
- `Sign In` stays disabled while either field is empty; a click on a disabled button times out instead of failing fast.
- Bootstrapping an admin with `CLOUDPAM_ADMIN_USERNAME` does not clear `needs_setup`; only `POST /api/v1/auth/setup` does. The launch script uses setup for that reason.
- Login rate limiting is on by default. Five quick wrong passwords can lock the next attempts for a while; test rejection once.
- The curl session from launch and the browser session are separate cookies; a journey must call `login()` itself.
