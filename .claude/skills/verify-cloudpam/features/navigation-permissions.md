# Navigation permissions

The sidebar shows a link only when the signed-in user can open that page, and a page the user cannot open sends them back to the Dashboard when they type its URL. A built-in `viewer` sees the IPAM, Operations, and Planning pages plus Release Notes; the admin-only pages (Audit Log, API Keys, Identity, Configuration, Log Destinations) disappear from the sidebar and redirect on a direct visit.

## Sub-features

- `nav-admin` shows the admin every sidebar link.
- `nav-viewer-links` shows a `viewer` exactly the ten ungated links.
- `nav-viewer-open` opens every link the `viewer` sees without a redirect.
- `nav-gated-redirect` redirects the `viewer` from each gated URL to `/`.
- `nav-user-menu` omits `My API Keys` from the header user menu for a user without `apikeys:list` or `apikeys:read`.
- `nav-api-agrees` refuses the gated pages' endpoints to the same user with `403`.

## How to get to it (user POV)

- Sign in and read the sidebar (`Main navigation` landmark).
- Type a page URL, such as `/audit` or `/config/api-keys`, into the address bar.
- Open the header user menu (the button named after the signed-in user).
- An admin creates the limited user from Identity, or through `POST /api/v1/auth/users`.

## Driving it with cloudpam-verify

Preconditions:

- Baseline instance from `cloudpam-verify launch`, signed in as the seeded admin.
- A second user `verify-viewer` / `ViewerDev12345!` with role `viewer`. The journey creates it with `POST /api/v1/auth/users` `{"username":"verify-viewer","password":"ViewerDev12345!","role":"viewer","email":"verify-viewer@localhost"}` and accepts `201` or `409` (already there from an earlier run).

- **Admin sidebar.** After `login()`, the `Main navigation` links are Dashboard, Address Pools, Allocated Blocks, Cloud Accounts, Discovery, Drift Detection, Audit Log, Schema Planner, Recommendations, AI Planner, API Keys, Release Notes, Identity, Configuration, Log Destinations. Proof `shot('00-admin-sidebar')`.
- **Viewer sign-in.** `context.clearCookies()`, then fill the `/login` form with the viewer's credentials (placeholder `admin`, `input[autocomplete="current-password"]`, button `Sign In`). `GET /api/v1/auth/me` names `verify-viewer` with permissions `accounts:list`, `accounts:read`, `discovery:list`, `discovery:read`, `pools:list`, `pools:read`.
- **Viewer sidebar.** The links are exactly Dashboard, Address Pools, Allocated Blocks, Cloud Accounts, Discovery, Drift Detection, Schema Planner, Recommendations, AI Planner, Release Notes. The Administration section holds only Release Notes. Proof `shot('01-viewer-sidebar')` and `aria('01-viewer-sidebar')`.
- **Viewer opens each link.** `page.goto(href)` for each link above; after the network settles the path is still `href`.
- **Gated URLs redirect.** `page.goto` each of `/audit`, `/config/api-keys`, `/identity`, `/config`, `/config/log-destinations`, `/config/updates`, `/config/security`, `/config/users`; the URL becomes `/`.
- **User menu.** Click the `verify-viewer` header button. `Profile` is listed and `My API Keys` is not. Proof `shot('02-viewer-user-menu')`.
- **Server agrees.** In the viewer's session, `GET /api/v1/audit`, `GET /api/v1/auth/keys` and `GET /api/v1/settings/security` return `403`, and `GET /api/v1/pools` returns `200`.
- **Proof.** The proven journey is `journeys/navigation-permissions.cjs`; its `journey.log` lists both sidebars, every opened and redirected path, and the API statuses.

## Gotchas

- `login()` always signs in the seeded admin. Sign the second user in through the form yourself after clearing cookies, or `login()` and the viewer share one session.
- The journey spends two of the five logins per minute per IP. Run it after other journeys only with a minute's gap, or `login()` waits out the limit.
- A gated URL renders the Dashboard, not an error page. Assert on the URL becoming `/`, not on a heading.
- The Audit Log needs `audit:read`. A custom role holding only `audit:list` sees no Audit Log link, because `GET /api/v1/audit` checks `audit:read` alone.
- API Keys needs `apikeys:list` or `apikeys:read`, not `settings:read`. A custom role with `settings:read` alone sees Configuration but no API Keys link or card.
