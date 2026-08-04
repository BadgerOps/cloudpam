<!--
Pulled from the "CloudPAM Design System" Claude Design project
(templates/identity-decisions.md) on 2026-08-04. Source of truth is the design
project; re-pull rather than editing in place if it changes there.
-->

# Identity model — decisions

Answers given 3 Aug 2026. These are settled unless noted; everything in
`settings-shell/` follows them.

| Question | Decision |
|---|---|
| SSO | **Already shipped** — SAML 2.0 and OIDC both in production |
| Provisioning | **SCIM**, owning both user create/deprovision **and** group→role mapping |
| Local accounts | **Break-glass admin only** — not a general account type |
| Service accounts | **Separate entity.** No password, no mailbox; holds a role and owns API keys |
| Pool scoping | **Yes, eventually** — teams own pool subtrees and roles scope to them |
| Role source | **Settled: conditional on SCIM** (see below) |

## Where a role comes from — settled

**SCIM presence decides it.** There is no user-facing preference:

- **SCIM enabled** → roles come from IdP group→role mapping. The role cell in People is
  read-only text with the source group beneath it; all changes happen in the mapping
  table. Break-glass local accounts are the exception and are assigned directly.
- **SCIM disabled** → each person is assigned a built-in or custom role directly, per
  user. Group membership is still recorded and mappings are shown but not enforced, so
  turning provisioning on later is not a data migration.

The `scimEnabled` tweak in the template shows both states.

Two consequences worth naming:

1. **Enabling SCIM silently overrides every per-user role** with whatever the mapping
   says. That needs a preview-and-confirm step — "12 people will change role, 3 will lose
   access entirely" — not a toggle that applies instantly.
2. **Custom roles still need a mapping target.** Deleting a custom role that a mapping
   points at locks out everyone in that group (see `ROLE_MISSING` in Denied sign-ins).
   Role deletion must check mappings first — this feeds the role-lifecycle gap.

## Unmapped sign-in — settled

**Denied, never defaulted.** No fallback-to-viewer. Every denial is written to the audit
log with a machine-readable reason, and the Identity Providers pane surfaces the last 7
days inline so the failure is visible where it can be fixed:

| Code | Meaning |
|---|---|
| `NO_GROUP_MATCH` | authenticated, but no mapping covers any group they hold |
| `ROLE_MISSING` | matched a mapping whose target role no longer exists |
| `NOT_A_PERSON` | a service account attempted interactive sign-in |

Each row links to its audit entry. The list is the fastest signal that a mapping is
wrong — a new hire's first sign-in failing should be visible to an admin without anyone
filing a ticket.

## What changed in the shell

- **Users → People.** Source column (SAML / OIDC / SCIM / LOCAL) with the IdP group
  beneath it. Role cell is read-only under SCIM, a select without it.
- **Deactivate is local-only.** Federated people show "Deprovision in Okta" instead —
  deactivating them here would just be undone at the next SCIM sync. Unlock stays
  available for everyone, since lockout is CloudPAM-side.
- **Break-glass banner** counts local accounts and states the expectation (keep it to
  one, rotate, expect audit entries). Creating one generates the password rather than
  asking an admin to invent it, and says it's shown once.
- **Invite flow removed.** With SCIM, people appear on provisioning or first sign-in.
  The only account an admin creates by hand is break-glass.
- **API Keys pane → Service Accounts.** Keys are nested under the account that owns
  them, since a key alone has no owner to hold accountable. Accounts with no team get an
  `UNOWNED` badge; an account with no keys says it cannot authenticate yet.
- **New permissions:** `idp:manage` (providers + mapping) added to the matrix; the group
  formerly called "Users & Roles" is now "Identity".
- Role cards show `Scope: all pools` — a placeholder for the tenancy work, so the
  concept has a home before it has behavior.

## Still open

1. **Which provider wins** when someone exists in both Okta SAML and Google OIDC with
   the same email? Today the template shows one source per person.
2. **Can SCIM deprovisioning remove the last admin?** Needs a guard either way.
3. **Do service accounts get scoped roles** once tenancy lands, or stay global?
4. **Break-glass password rotation** — enforced, or advisory as the banner implies?
5. **Enabling SCIM needs a diff step** (see above) — worth designing alongside the
   first-run states gap.
