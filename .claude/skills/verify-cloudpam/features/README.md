# CloudPAM verification map

This directory is the maintained source for verifying the user-facing behavior of CloudPAM. Read the index before driving the app, then use the matching feature file as the recipe. The harness is `cloudpam-verify` (`.claude/skills/verify-cloudpam/bin/cloudpam-verify`); see `../SKILL.md` for launch, doctor, evidence, and cleanup.

## Baseline preconditions

- `cloudpam-verify launch` has started a disposable instance (SQLite build, fresh database, migrations applied) on `http://127.0.0.1:<PORT>` with `PORT` in 18080-18179. `cloudpam-verify env` prints `BASE_URL`, credentials, and paths.
- The admin is `verify-admin` / `VerifyDev12345!` (override with `CLOUDPAM_VERIFY_USERNAME` / `CLOUDPAM_VERIFY_PASSWORD` before launch).
- Seed data: pool `Corp Supernet` (`10.0.0.0/8`, type supernet), child pool `Prod East VPC` (`10.20.0.0/16`), account `prod-platform` (key `aws:111111111111`, provider aws).
- `cloudpam-verify doctor` passes every check.
- Never drive an instance that this run did not start. `doctor` proves the port is ours by matching `/healthz` `version` to the run's `APP_VERSION`.

## Driving conventions

- Start every recipe from the baseline state unless its preconditions say otherwise.
- Browser actions go through a journey file run by `cloudpam-verify browser <journey.cjs>`; `journeys/` holds proven ones. Inside a journey, prefer `getByRole`/`getByLabel`; where a form has no label association (login, accounts), use the placeholder named in the feature file.
- API actions go through `cloudpam-verify api METHOD PATH [JSON]` (session cookie + CSRF handled) or the journey's `api()` helper.
- Treat every command, name, and CIDR as literal.
- Mutations leave rows behind. Use a unique name per run (`verify-<timestamp>`) rather than deleting seed data; the instance is disposable.
- Cleanup removes the instance and its state, never the evidence.

## Proof and skip reporting

- Capture the user action and the resulting state, not only the final screen: a screenshot before submit, one after, and an ARIA snapshot of the end state.
- Mutation proof includes a second read-only view of the stored value (`GET /api/v1/...`) and, where the action is audited, the matching `GET /api/v1/audit` event.
- API proof includes method, path, status, and body (`cloudpam-verify api --save NAME ...` writes `evidence/api/NAME.txt`).
- Record the feature ID and entry point used with every artifact (the journey log does this for browser runs).
- Report an unreachable path with the attempted command and the unmet precondition. Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every user entry point.
3. `Driving it with cloudpam-verify` starts with `Preconditions:` and uses labeled bullets that pair each user action with an exact command or journey step and the observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable handles, required state, commands, and observable proof.

## Features

- [Sign in](./login.md) covers first-boot setup, the login form, rejected credentials, and the session landing in the app shell. Proven journey: `journeys/login.cjs`.
- [Create a pool](./create-pool.md) covers the Address Pools form, parent selection, overlap and duplicate refusal, API creation, and audit side effects. Proven journey: `journeys/create-pool.cjs`.
- [Create a cloud account](./create-account.md) covers the Cloud Accounts form, duplicate-key refusal, API creation, and deletion. Proven journey: `journeys/create-account.cjs`.
- [Search](./search.md) covers the header search box, CIDR containment lookups, and the search API. Proven journey: `journeys/search.cjs` (its last step fails at e0818fd on a product bug; see the feature file).
- [Navigation permissions](./navigation-permissions.md) covers which sidebar links a limited user sees, the redirect from gated URLs, the header API keys shortcut, and the matching API refusals. Proven journey: `journeys/navigation-permissions.cjs`.

Not yet mapped (the sidebar reaches them, no recipe here): Dashboard, Allocated Blocks, Discovery, Drift Detection, Audit Log, Schema Planner, Recommendations, AI Planner, API Keys, Release Notes (and the update banner), Identity, Configuration, Log Destinations, and the `Import/Export` button. A proof that needs one of these starts by adding its feature file.
