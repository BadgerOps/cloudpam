# Create a pool

Create a pool lets a user add an IPv4 CIDR block, optionally under a parent pool, from the Address Pools page or the API, and refuses blocks that overlap a sibling, repeat a sibling exactly, or escape the parent.

## Sub-features

- `pool-open-form` shows the create form from the `New Pool` button.
- `pool-create-root` creates a top-level pool.
- `pool-create-child` creates a pool under a selected parent and the row shows the new CIDR.
- `pool-reject-overlap` refuses a CIDR that overlaps a sibling under the same parent.
- `pool-reject-duplicate` refuses a CIDR that exactly repeats a sibling under the same parent.
- `pool-reject-outside-parent` refuses a child CIDR not contained by its parent.
- `pool-create-api` creates the same pool shape from the API.
- `pool-audit` records a `create` audit event for the pool.

## How to get to it (user POV)

- Choose `Address Pools` in the sidebar, then the `New Pool` button.
- `POST /api/v1/pools` with `{"name","cidr","parent_id"}` from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present: `Corp Supernet` (`10.0.0.0/8`, id `1` on a fresh instance) and `Prod East VPC` (`10.20.0.0/16`).
- No pool named `verify-<timestamp>` exists and the chosen `/16` under `10.0.0.0/8` is unused. The proven journey picks a free one from `GET /api/v1/pools`. A reused CIDR never shows `Pool created`: an exact repeat is refused with `409`, a partial overlap with `400`.

- **Open form.** After `login()`, click `getByRole('link', { name: 'Address Pools' })`, wait for `getByRole('heading', { name: 'Address Pools' })` and the cell `Corp Supernet`, then click `getByRole('button', { name: 'New Pool' })`. Fields `Name`, `CIDR`, `Type`, `Status`, `Account`, `Parent Pool`, `Description` appear (all `getByLabel`-able).
- **Create child.** `getByLabel('Name', { exact: true }).fill('verify-<ts>')`, `getByLabel('CIDR', { exact: true }).fill('10.<n>.0.0/16')`, `getByLabel('Parent Pool', { exact: true }).selectOption({ label: 'Corp Supernet (10.0.0.0/8)' })`, click `getByRole('button', { name: 'Create Pool' })`. Toast text `Pool created` appears.
- **Find the child.** Type `verify-<ts>` into the page search box (placeholder `Search pools by name, CIDR, or description...`). The table now has cells `verify-<ts>` and `10.<n>.0.0/16`. Clear the box to return to the root-only list.
- **Parent count.** `api('GET', '/api/v1/pools/<Corp Supernet id>/stats')` returns `200` with `direct_children` equal to the number of pools in `GET /api/v1/pools` whose `parent_id` is Corp Supernet's id (2 on a fresh instance: seeded `Prod East VPC` plus the new one).
- **Parent panel.** In the `Corp Supernet` row, click the control titled `View details`. A side panel with heading `Corp Supernet` shows CIDR `10.0.0.0/8`, type, status, source, and `Edit Pool` / `View Audit` buttons. It does not show child counts on this page.
- **Confirm persistence.** `api('GET', '/api/v1/pools')` returns `200` and an array containing the pool with the chosen `cidr` and `parent_id` equal to Corp Supernet's `id`.
- **Audit side effect.** `api('GET', '/api/v1/audit?action=create&resource_type=pool&limit=50')` returns `200` and an `events` entry mentioning the pool name.
- **Reject overlap.** Open the form again, name `verify-<ts>-overlap`, CIDR `10.<n>.128.0/17`, parent `Corp Supernet (10.0.0.0/8)`, click `Create Pool`. Text `cidr overlaps with existing block` appears and `GET /api/v1/pools` does not contain the name. The API returns `400` with detail `conflicts with pool #<id> (<cidr>)`; the UI shows only the error.
- **Reject duplicate.** Run `cloudpam-verify api POST /api/v1/pools '{"name":"verify-dup","cidr":"10.20.0.0/16","parent_id":<Corp Supernet id>}'`. Status `409` with `conflict: constraint failed: UNIQUE constraint failed: index 'uniq_pools_parent_cidr' (2067)` on SQLite. The helper exits 1 for any non-2xx; that is the expected result here.
- **Reject outside parent.** Run `cloudpam-verify api POST /api/v1/pools '{"name":"verify-outside","cidr":"192.168.0.0/24","parent_id":<Corp Supernet id>}'`. Status `400` with `invalid sub-pool cidr` and detail `child not within parent`.
- **API entry.** Run `cloudpam-verify api --save pool-create POST /api/v1/pools '{"name":"verify-api-<ts>","cidr":"10.31.0.0/16","parent_id":<Corp Supernet id>}'`. Status `201`; body has the new `id`.
- **Proof.** `shot('03-pool-row-visible')` and `aria('03-pool-row-visible')` with the new row in view, plus the saved API responses. The proven journey is `journeys/create-pool.cjs` (run: `cloudpam-verify browser .claude/skills/verify-cloudpam/journeys/create-pool.cjs`).

## Gotchas

- Use `getByLabel('CIDR', { exact: true })` (same for `Name`): the header search box carries `aria-label="Search pools, CIDRs, accounts"`, so a substring match resolves to two elements and Playwright's strict mode throws.
- The table shows root pools only (`parent_id` null) until the page search box has text; a child you just created is not in the table until you search for it.
- Parent options are rendered as `Name (cidr)`; `selectOption` needs the full label or the numeric id value.
- The form is inline (not a dialog); `New Pool` toggles it, so clicking it twice hides the form again.
- Overlap is checked only against siblings and containment only against the direct parent. Through `POST /api/v1/pools` the two rules together still keep pools in different subtrees apart.
- Creates are serialized inside one server process: `POST /api/v1/pools` and `POST /api/v1/schema/apply` hold one lock across the list, the check and the insert. Concurrent overlapping siblings give one `201` and the rest `400` or `409`. A browser journey cannot show this; `/verify-pool-overlap` model-checks it. Two server processes on one database are not serialized.
- An exact duplicate shows the raw storage text in the UI error box (`conflict: ... UNIQUE constraint failed ...`), not a friendly message.
- On SQLite, a deleted pool still holds its `(parent, cidr)`: re-creating it returns `409` although `GET /api/v1/pools` no longer lists it (issue #268). The in-memory store allows the re-create.
- Only IPv4 CIDRs are accepted; `fd00::/8` returns `400` `invalid cidr "fd00::/8": only ipv4 is supported`.
- If the page sticks on `Loading pools...`, run `doctor`: its `/api/v1/pools/hierarchy` probe catches a hung pools query (the nested-query hang fixed in #261 looked like this).
- Pool deletion uses a browser `confirm()` dialog; the journey runner auto-accepts dialogs and logs them.
