# Network schema policy

The stored network schema policy decides which account, region, or recorded relationship owns a CIDR and when a repeated CIDR counts as a conflict. An admin sees and changes it on the Configuration page. Discovery's Merged Network views start from the stored policy and let the user pick another one for the current requests only.

## Sub-features

- `NSP-1` The Configuration page shows the stored policy in a `Policy` select with Save disabled until an edit.
- `NSP-2` Saving stores the chosen policy, shows the `Network schema policy saved` toast, and writes an `update` audit event for `settings`.
- `NSP-3` The manual policy exposes a `Duplicate CIDRs` select whose value survives a save and a reload.
- `NSP-4` Discovery > Merged Network starts with the `Schema policy` select on the stored policy, sends no `schema_policy` query, and the summary reads `<name> policy, stored default`.
- `NSP-5` Picking a policy in Discovery sends `schema_policy=<name>`, the summary reads `<name> policy, request-scoped`, and the stored policy is unchanged.
- `NSP-6` A user with `settings:read` but not `settings:write` sees the card read-only; without `settings:read` the card is absent.

## How to get to it (user POV)

- Sidebar `Configuration` > card `Network Schema Policy` (needs `settings:read`; editing needs `settings:write`).
- Sidebar `Discovery` > `Merged Network` button > `Schema policy` select in the filter bar.
- API: `GET /api/v1/settings/network-schema-policy`, `PATCH /api/v1/settings/network-schema-policy` with `{"name":"region_level"}` or `{"name":"manual","duplicate_scope":"global"}`.

## Driving it with cloudpam-verify

Preconditions: baseline launch; the fresh database stores the account policy.

- Journey: `cloudpam-verify browser .claude/skills/verify-cloudpam/journeys/network-schema-policy.cjs` covers `NSP-1` to `NSP-5` in order and writes `01`..`08` screenshots plus ARIA snapshots of the saved card and the Discovery view.
- Stored value: `cloudpam-verify api GET /api/v1/settings/network-schema-policy` prints the policy the card shows; after the journey it is `manual` with `duplicate_scope` `global`.
- Applied value: `cloudpam-verify api GET /api/v1/network/hierarchy` returns `schema_policy` equal to the stored policy when the request names none.
- Audit: `cloudpam-verify api GET '/api/v1/audit?action=update&resource_type=settings'` lists the `network_schema_policy` event.
- `NSP-6`: create a user with a viewer role, sign in as that user, and open Configuration; the card is absent (`navigation-permissions.md` has the user recipe).

## Gotchas

- The Discovery select shows the account policy until the first network view response arrives; wait for the `… policy, stored default` summary before reading it.
- A request-scoped pick in Discovery is kept for the page's lifetime; reopen Discovery to get back to the stored default.
- The server stores `custom` as an alias of `manual`; the card shows both as `Manual policy` and saves the name `manual`.
