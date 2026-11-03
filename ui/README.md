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

Every configuration or administration screen builds on the six primitives in
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

The prop contracts and the rules behind them live in `docs/design/primitives.md` and
`docs/design/primitives-contracts.md`. `src/pages/SecuritySettingsPage.tsx` is the
reference migration. Status text still goes through `StatusBadge`.
