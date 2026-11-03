# CloudPAM UI

React, Vite and TypeScript single-page app. Run everything through the Nix dev shell:

```bash
cd ui && npm install
npm run dev          # Vite with /api proxied to :8080
npx tsc --noEmit     # type check
npx vitest run       # tests
npm run build        # writes ../web/dist, embedded by the Go binary
```

## Configuration primitives

Every configuration or administration screen builds on the primitives in
`src/components/primitives/`. Use them. Do not hand-roll an overlay, a label/input
pair, a switch, a table, or an empty message again.

| Primitive | Use it for | Never instead |
|---|---|---|
| `Modal` | any overlay | a bespoke fixed-inset div |
| `ConfirmDialog` | destructive or irreversible actions | firing the action on click |
| `Field` | every form control | a loose `<label>` + `<input>` |
| `Toggle` | booleans that apply immediately | a checkbox, or a button that reads as a badge |
| `DataTable` | any list of records | a bare `<table>` |
| `EmptyState` | zero results, zero records, filtered-out | centered grey "No results" text |
| `MetricCard` | a headline number with its tone and a link to act on it | a bespoke stat tile |

The prop contracts for the first six and the rules behind them live in
`docs/design/primitives.md` and `docs/design/primitives-contracts.md`. `MetricCard`
came later with the dashboard redesign, and its `MetricCardProps` type is its contract.
`src/pages/SecuritySettingsPage.tsx` is the reference migration, and
`src/pages/DashboardPage.tsx` is the reference for `MetricCard`. Status text still goes
through `StatusBadge`.
