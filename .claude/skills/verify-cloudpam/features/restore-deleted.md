# Restore deleted pools and accounts

Recently Deleted lists soft-deleted pools and accounts and brings one back at a time, from the sidebar page or the API. A pool inside a deleted parent cannot be restored until the parent is, and a restore that would collide with a live pool or account is refused.

## Sub-features

- `restore-list` lists deleted pools and accounts, most recently deleted first, with how long ago each was deleted.
- `restore-blocked-child` disables `Restore` on a pool whose parent is also deleted and names the parent.
- `restore-pool` brings a pool back, removes its row, and shows `Restored <name>`.
- `restore-account` brings an account back the same way.
- `restore-conflict` refuses a restore with `409` when a live pool overlaps or holds the CIDR, a live account holds the key, or the parent or account is still deleted; the UI shows the server's message in a toast and keeps the row.
- `restore-audit` records a `restore` audit event per restored record.

## How to get to it (user POV)

- Choose `Recently Deleted` in the sidebar (shown to users with `pools:delete` or `accounts:delete`), then a row's `Restore` button.
- `GET /api/v1/pools/deleted` and `GET /api/v1/accounts/deleted` from a terminal.
- `POST /api/v1/pools/{id}/restore` and `POST /api/v1/accounts/{id}/restore` from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present.
- A parent pool and a child pool, both cascade-deleted. The journey creates them with `api()` on a free `N.0.0.0/16` (N in 100-199) and deletes the parent with `?force=true`.

- **List.** After `login()`, click `getByRole('link', { name: 'Recently Deleted' })` and wait for `getByRole('heading', { name: 'Recently Deleted' })`. Both pools are listed; `getByRole('button', { name: 'Restore pool <child>' })` is disabled and the text `inside deleted pool <parent>` is shown.
- **Restore parent.** Click `getByRole('button', { name: 'Restore pool <parent>' })`. Toast `Restored <parent>` appears, the parent row goes, and the child's `Restore` button becomes enabled.
- **Restore child.** Click the child's `Restore` button. Toast `Restored <child>` appears and `No deleted pools.` remains when nothing else is deleted.
- **Confirm persistence.** `api('GET', '/api/v1/pools')` includes both ids with the child's `parent_id` unchanged; `api('GET', '/api/v1/pools/deleted')` includes neither; `api('POST', '/api/v1/pools/<child>/restore')` returns `404`.
- **Audit.** `api('GET', '/api/v1/audit?action=restore&resource_type=pool&limit=50')` includes an event naming each pool.
- **Proof.** `shot('01-recently-deleted')` and `aria('01-recently-deleted')` before any restore, `shot('02-parent-restored')`, `shot('03-child-restored')` and `aria('03-child-restored')`. The proven journey is `journeys/restore-deleted.cjs`.

## Gotchas

- A screenshot taken right after a sidebar click can still show the previous link highlighted: the links animate their background with `transition-colors`. Read the ARIA snapshot or a later screenshot for the active link.
- A restore brings back one record. Restoring a cascade-deleted parent leaves its children in the list until each is restored.
- Restoring an account does not restore pools deleted with it, nor a discovery credential the delete removed.
