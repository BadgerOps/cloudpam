# Pool growth runway

The pool detail panel shows a `Growth` section with the pool's runway (time until its address space runs out at the recent allocation rate), the fitted rate, the month the pool fills, and the projected utilization 3, 6 and 12 months out. The same numbers come back as `projection` on the pool stats API. A pool with under 7 days of utilization history says so instead of guessing.

## Sub-features

- `runway-insufficient-history` shows `Not enough history to project growth yet` for a pool without snapshots older than 7 days.
- `runway-fit` shows `Runway`, a duration such as `11.6 years`, the rate line `<n> IPs/day over the last 90 days · full by <Mon YYYY>`, and three forecast columns `3 mo`, `6 mo`, `12 mo` with a percentage each.
- `runway-no-growth` shows `No growth over the last 90 days` with the forecast columns flat when usage did not rise inside the window.
- `runway-api` returns `projection` with `status`, `window_days`, `sample_count`, `growth_ips_per_day`, `runway_days`, `exhaustion_date`, and `forecast` from the stats API.

## How to get to it (user POV)

- Choose `Address Pools` in the sidebar, search for a pool, click the row's `View details` control; the side panel shows `Growth` under the pool facts.
- Choose `Dashboard` and click a pool in the tree; the same panel opens.
- `GET /api/v1/pools/{id}/stats` from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present: `Prod East VPC` (`10.20.0.0/16`, id `2` on a fresh instance).
- Nothing records utilization snapshots yet, so a fit needs rows inserted into `utilization_snapshots` in `state/cloudpam.db` (columns `pool_id, total_ips, used_ips, available_ips, utilization, child_count, captured_at`, with `captured_at` as RFC3339 UTC). Python's `sqlite3` module works; `sqlite3` the binary is not in the dev shell. The live stats are the newest point, so the rows only need to be older than 7 days and lower than the live `used_ips`.

- **No history.** Run `cloudpam-verify api --save stats-before GET /api/v1/pools/2/stats`. Status `200`; `projection.status` is `insufficient_history`, `sample_count` is `1`, `forecast` is `[]`, no `runway_days`. The panel's `Growth` section says `Not enough history to project growth yet`.
- **Seed history.** Create two children (`POST /api/v1/pools` with `10.20.0.0/24` and `10.20.1.0/24`, `parent_id` 2) and insert snapshots for pool 2 at 30, 20 and 10 days ago with `used_ips` 0, 256 and 256.
- **API fit.** Run `cloudpam-verify api --save stats-after GET /api/v1/pools/2/stats`. Status `200`; `projection.status` is `ok`, `sample_count` is `4`, `growth_ips_per_day` is about `15.4`, `runway_days` is about `4230`, and `forecast` has three entries with rising `used_ips`.
- **Panel.** After `login()`, click `getByRole('link', { name: 'Address Pools' })`, fill the page search box (placeholder `Search pools by name, CIDR, or description...`) with `Prod East`, click `getByTitle('View details').first()`, wait for the level-3 heading `Prod East VPC`. `getByTestId('runway')` reads `11.6 years` and `getByTestId('growth-forecast')` contains `3 mo`, `6 mo`, `12 mo`.
- **Proof.** `shot('01-pool-detail-runway')` and `aria('01-pool-detail-runway')` with the panel open, plus the two saved API responses. The proven journey is `journeys/pool-runway.cjs` (run: `cloudpam-verify browser .claude/skills/verify-cloudpam/journeys/pool-runway.cjs`); it asserts the API state itself, so seed before running it.

## Gotchas

- The window is 90 days and the live stats count as the newest point: one snapshot from 8 days ago with lower usage is enough for a fit, while snapshots older than 90 days are ignored even when they are the only history.
- `runway_days` is a float and can be large (thousands of days); the panel rounds it to days, months or years.
- The hierarchy and `include_stats` list responses never carry `projection`; only the single-pool stats endpoint does.
- Snapshots are a `UNIQUE(pool_id, captured_at)` pair; inserting the same second twice fails.
