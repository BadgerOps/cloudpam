<!--
Pulled from the "CloudPAM Design System" Claude Design project
(templates/repo-changes.md) on 2026-08-04. Source of truth is the design
project; re-pull rather than editing in place if it changes there.
-->

# Upstream changes needed in `ui/`

These belong in the CloudPAM repo, not here — this design system is synced read-only.
Everything below came out of the Administration/Configuration review; the templates in
this folder are the reference implementations.

Identity decisions that drive §1 and §2 are recorded in `identity-decisions.md`.

---

## 1. Collapse the Administration nav to one Settings destination

`ui/src/components/Sidebar.tsx`

Today the Administration group lists five siblings, three of which are children of
`/config`:

| Current | Route |
|---|---|
| API Keys | `/config/api-keys` |
| Release Notes | `/changelog` |
| Identity | `/identity` |
| Configuration | `/config` |
| Log Destinations | `/config/log-destinations` |

Panes, after the identity decisions: `/settings/people`, `/settings/identity-providers`,
`/settings/service-accounts`, `/settings/access`, `/settings/log-destinations`,
`/settings/system`. Note **API Keys is no longer a pane** — keys live under the service
account that owns them.

Replace with a single entry:

```tsx
{(canSettings || canUsers) && (
  <NavLink to="/settings" className={linkClass}>
    <Settings className="w-5 h-5 flex-shrink-0" />
    <span>Settings</span>
  </NavLink>
)}
```

- **Bug fix included:** `NavLink to="/config"` has no `end`, so `/config/api-keys`
  highlights both "API Keys" and "Configuration" at once. A single route removes it.
- Gate consistently on `settings:read || users:list` — currently API Keys and Release
  Notes are ungated while the other three require `settings:read`.
- **Release Notes is not administration.** Move it into the Settings → System pane as a
  link, or into the Header's help menu.
- Redirect the old paths so existing links and bookmarks survive:

```tsx
<Route path="/identity" element={<Navigate to="/settings/users" replace />} />
<Route path="/config" element={<Navigate to="/settings" replace />} />
<Route path="/config/api-keys" element={<Navigate to="/settings/api-keys" replace />} />
<Route path="/config/log-destinations" element={<Navigate to="/settings/log-destinations" replace />} />
```

The settings sub-nav should be a nested route so each pane is linkable. `idp:manage` is
a new permission gating the Identity Providers pane; add it to the built-in admin role.

---

## 1b. Split service accounts out of `users`

From the identity decisions: a service account has no password and no mailbox, holds a
role, and owns API keys. Today `ops-bot` is a user row with a password column, which is
why Users and API Keys feel tangled.

- New table + endpoints: `/api/v1/auth/service-accounts`, with keys nested under the
  account rather than free-floating.
- Migration: existing machine users become service accounts; their password hashes are
  dropped, not carried over.
- An account with no keys cannot authenticate — surface that rather than showing an empty
  row (`templates/settings-shell/` shows the copy).
- Keys need an owning team; `UNOWNED` is a real state worth flagging, since nobody is
  accountable for rotating those.

---

## 1c. Federated people are not deactivatable in CloudPAM

`useUsers().deactivate` currently applies to everyone. For SAML/OIDC/SCIM people this is
wrong — the next SCIM sync undoes it. Gate the action on `source === 'local'` and show
"Deprovision in {provider}" otherwise. Unlock stays available for all sources, because
lockout is CloudPAM-side state.

Also needed: a guard so SCIM deprovisioning cannot remove the last admin.

---

## 1d. Deny unmapped sign-ins, and log why

No fallback role. When a federated principal authenticates but no mapping applies, refuse
the session and write an audit event carrying a machine-readable reason:

```ts
type SignInDenialCode = 'NO_GROUP_MATCH' | 'ROLE_MISSING' | 'NOT_A_PERSON';
// audit event: { action: 'auth.signin.denied', code, subject, provider, groups[] }
```

- `NO_GROUP_MATCH` — authenticated, holds no mapped group.
- `ROLE_MISSING` — matched a mapping whose target role was deleted. **Role deletion must
  check mappings first** to prevent this.
- `NOT_A_PERSON` — a service account attempted interactive sign-in.

Surface the last 7 days in the Identity Providers pane (implemented in the template), each
row linking to its audit entry. A denial that only exists in the log will reach an admin
as a support ticket instead.

Also: **enabling SCIM must not silently rewrite roles.** It needs a preview step listing
who changes role and who loses access before it applies.

---

## 2. Retire `UsersAdminPanel`

`ui/src/components/UsersAdminPanel.tsx` → delete once the Users pane lands.

The pane in `templates/settings-shell/` supersedes it. What it fixes, for the record —
these are the defects to make sure the replacement does not inherit:

- Hand-rolled pills for role and Active/Locked/Disabled instead of `StatusBadge`, the
  DS's one labeling primitive.
- Role editing hidden behind a badge that looks like a badge, discoverable only via a
  `title` tooltip. Replaced by an always-visible labeled `<select>`.
- Deactivate fires with no confirmation and no feedback. Replaced by `ConfirmDialog`
  plus a toast with Undo.
- `handleRoleSave` swallows every failure in `catch {}`.
- The entire header is duplicated for `embedded` vs not, differing only by tag and one
  sentence. Should be `title` / `description` props.
- No search, sort, or pagination — unusable past ~50 users.
- Below `md` it hides Email, Failures, and Last Login simultaneously, leaving mobile
  with almost nothing.
- `dark:hover:bg-gray-750` is not a real Tailwind color. Dead class; no hover in dark
  mode. (Compiled Tailwind silently drops unknown utilities, so this never errored.)
- Admin sets the new user's password directly, which forces an out-of-band secret
  handoff. Replaced by an emailed one-time invite link, 72-hour expiry.
- "Failures" as a column header is jargon. Now a count badge attached to the Locked
  state, where it means something.

Keep `useUsers` / `useRoles` / `usePendingAction` — the hooks are fine, only the panel
goes.

---

## 2b. Role lifecycle

`useRoles` already exposes `create` / `update` / `remove`, but no UI reaches them.
Implemented in `templates/settings-shell/` Access & Roles:

- **Create is always a copy.** A new custom role starts from an existing role's grant set,
  never an empty grid — the dialog states how many of the N permissions come with the
  chosen baseline. Built-in roles get a Duplicate action for exactly this reason.
- **Rename rewrites references.** Group mappings, member assignments, and grant keys all
  move with the name. The dialog warns when mappings exist, because the role name is part
  of the public API surface and external scripts calling it by name will break.
- **Delete is reference-checked, not confirmed.** `DELETE /roles/:name` must refuse while
  anything points at the role, and return the blockers so the UI can list them:

```ts
// 409 Conflict
{ error: 'role_in_use', members: 3, mappings: ['eng-all', 'platform-oncall'] }
```

  The dialog shows each blocker with a link that navigates to where it gets cleared
  (People filtered to that role, or the mapping table). Deletion only offers a destructive
  button once both counts are zero. **This is what prevents the `ROLE_MISSING` denial
  from §1d** — today nothing stops deleting a mapped role and locking out a whole group.
- Built-in roles cannot be renamed or deleted; their cards say `fixed` rather than
  offering a disabled button.

Also needed server-side: block deleting the last role that grants `users:update`, or an
admin can strip their own ability to fix it.

---

## 2c. First-run states

Flip `deploymentState` to `fresh` in the template to see all of this.

A brand-new install has one local admin from the installer and nothing else. That is the
first configuration screen every customer sees, and today it renders as a set of empty
tables.

**A Setup pane leads the sub-nav while incomplete**, showing `n/5`, and disappears on
completion — no dismissable banner to re-surface later. Steps are ordered, only the next
one is emphasized, and each states why it matters rather than just what to click:

1. **Connect an identity provider** — until one exists, only the install-time admin can
   sign in.
2. **Map a group to a role** — a federated person matching no mapping is denied, so
   nobody gets in without this.
3. **Rotate the install-time password** — the bootstrap admin ships with an installer
   password; rotate it and keep it as break-glass.
4. **Create a service account** — automation should authenticate as itself, not as a
   person who might leave.
5. **Forward audit events** — records stay local until a destination exists.

Step order is load-bearing: 1 before 2 because mappings need a provider, and 2 carries a
warning to add your own admin group first so you don't lock yourself out of the session
you're using.

**Each pane also stands alone**, because people arrive by deep link, not always through
setup:

| Pane | First-run state |
|---|---|
| People | "Just you so far" — explains people arrive via SCIM or first sign-in, not invites |
| Identity Providers | three Connect cards (SAML / OIDC / SCIM) instead of an empty grid |
| Group mapping | "No mappings yet — nobody can sign in", offers starter mappings |
| Denied sign-ins | "Nothing has attempted to sign in yet" |
| Service Accounts | explains why automation needs its own identity |
| Log Destinations | "Audit events are staying on this box" + the compliance consequence |

Note the distinction the current code misses: **zero-records is not zero-results.** A
filtered empty table says "no matches, clear the filter"; an unconfigured one explains
what the feature is for and offers the first action. `EmptyState` needs to express both.

---

## 2d. Responsive behavior

Set `layout: mobile` in the template to review it at 430px without resizing the window.

Breakpoint at **760px**. Two structural changes and one rethink:

- **The nav rail becomes a sticky top bar** with horizontally scrolling pills, keeping the
  section name visible. It does not become a hamburger — with six destinations, hiding
  them behind a tap costs more than the vertical space it saves.
- **Tables become cards, not scrollers.** Each person is one card: identity and status on
  the first line, source and group on the second, role control and actions below a
  divider. Every touch target is at least 44px. The same treatment applies to the group
  mapping table.
- **The permissions matrix does not scroll horizontally — it transposes.** A role selector
  runs across the top, and the body becomes a single-column list of permissions with one
  switch each, for the selected role only. A 37×5 grid cannot be usefully panned on a
  phone; comparing roles is a desktop task, while *checking or changing one role* is the
  thing anyone actually does on mobile.

Checkbox squares become proper `role="switch"` toggles at mobile size — 20px squares are
below the touch minimum, and the single-role view no longer needs the compact grid
alignment that justified them.

Two implementation notes for the port: the breakpoint is measured in JS here because DC
templates cannot carry media queries; in the real app this is a Tailwind `md:` variant, and
`hidden md:block` / `md:hidden` pairs replace the `narrow`/`wide` branches. And the
current `hidden md:table-cell` approach in `UsersAdminPanel` is the thing to avoid —
dropping three columns leaves a table that is narrow but useless, rather than a layout
that fits.

---

## 3. Colorize the missing status labels

`ui/src/utils/badges.ts` (`getStatusBadgeClass`)

`locked`, `disabled`, `revoked`, and `idle` all fall through to the grey default today,
so a locked account and a planned pool look identical. Add:

```ts
locked:   "bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300",
disabled: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
revoked:  "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
idle:     "bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300",
```

All four color families already ship in the compiled stylesheet at `-100` / `-700`, so
no Tailwind rebuild is needed beyond the new class strings appearing in source.

Until this lands, the Users pane in the template renders those three states with its own
literal styles matching these exact values — swap them back to `<StatusBadge>` after the
sync.

**Correction to the spec:** the claim above that these four fall through to grey "in the
live UI" is false — verified during the final review of Plan A. `getStatusBadgeClass` has
exactly one call site, `StatusBadge.tsx:32` (the `variant === 'status'` default case), and
nothing in the app currently passes it `locked`/`disabled`/`revoked`/`idle`. The surfaces
that actually render these statuses today hand-roll their own pills instead of going
through `StatusBadge`: `UsersAdminPanel.tsx:309-321` (Locked/Active/Disabled) and
`ApiKeysPage.tsx:91-106` (Revoked/Expired/Expiring/No expiry/Active). So there is no live
defect to fix — the four entries are still correct to add (Plan C routes these surfaces
through `StatusBadge`), just pre-wiring rather than a fix. One follow-on detail for that
port: the hand-rolled pills use `dark:bg-amber-900/30`, while `getStatusBadgeClass` (in
`ui/src/utils/format.ts` — the `badges.ts` path above is stale; see the correction note in
`docs/superpowers/plans/2026-08-04-pool-type-color-and-status-badges.md`) uses
`dark:bg-amber-900` with no opacity suffix, so swapping them in is a visible color delta,
not a no-op.

---

## 4. Extract the config primitives

See `templates/primitives/` for working implementations and prop contracts.

| Primitive | Replaces |
|---|---|
| `Modal` | separate backdrops in `SearchModal`, `ImportExportModal`, and two dialogs in Settings |
| `ConfirmDialog` | nothing — destructive actions currently fire immediately |
| `Field` | ad-hoc label/input/error markup in every form |
| `Toggle` | nothing — booleans are checkboxes or absent |
| `DataTable` | bespoke tables in `UsersAdminPanel`, API keys, audit log |
| `EmptyState` | centered grey text with an opacity-40 icon |

`Modal` is the highest-leverage one: it removes four copies of backdrop + escape
handling + focus trap, and only one of those copies traps focus today.

---

## 5. Adopt `ToastContext` everywhere

`ToastContext` and `ToastContainer` ship and are almost never called. Suggested rules:

- **Toast every mutation whose result is off-screen or ambiguous** — role changed, user
  deactivated, key revoked, destination paused, permissions saved.
- **Do not toast** navigation, form validation (that's `Field`'s `error`), or anything
  the user can already see change.
- **Destructive and reversible actions get Undo in the toast**, with a 6-second life
  instead of the usual 2.5. Deactivate, pause delivery, and role changes are all
  reversible and should offer it. Revoke is genuinely irreversible — it gets a
  `ConfirmDialog` instead, never an Undo it cannot honor.
- One toast at a time; a new one replaces the old rather than stacking.
- `role="status"` on the container so screen readers announce it, which the current
  `ToastContainer` does not set.

---

## 6. One pool-type color map

Outside Administration, but a one-function fix with a correctness bug attached. Mockup:
`templates/pool-type-color/`.

Three sources of truth today:

| | `getPoolTypeColor` | `TreeNode.TYPE_COLORS` | `SchemaPlanner` legend |
|---|---|---|---|
| supernet | purple-500 | purple-500 | purple (literal) |
| root | purple-500 | **missing → grey** | purple (literal) |
| region | blue-500 | blue-500 | blue (literal) |
| environment | green-500 | green-500 | green (literal) |
| vpc | amber-500 | amber-500 | amber (literal) |
| account | amber-500 | **missing → grey** | — |
| subnet | orange-500 | **grey-400** | — |

- **Bug:** `root` and `account` hit the unknown fallback in the Schema Planner, so the
  same pool is purple in `PoolTree` and grey in the wizard. The wizard's own legend
  contradicts its own tree.
- **Grey must mean one thing.** Once grey also means "subnet", an unrecognised type is
  invisible.
- **The wizard's de-emphasis of subnets is legitimate** — they are leaves not yet
  committed — but it is implemented by replacing the hue, which destroys the type. Move it
  to the row as `opacity-50` and keep the hue. (`dimTreatment` in the mockup compares
  opacity, hollow ring, and none; opacity is the recommendation.)
- **Render the legend from `POOL_TYPES`** instead of literal swatches, so it cannot drift
  again.
- No `dark:` variants needed; the `-500` weights hold on both grounds. `TYPE_COLORS` only
  needed them for the grey it invented.

Net change: delete `TYPE_COLORS`, one call site edited, legend mapped over the shared
constant.
