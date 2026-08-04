<!--
Prop contracts extracted from the "CloudPAM Design System" Claude Design project,
templates/primitives/Primitives.dc.html, on 2026-08-04. The mockups are DC
templates and need the DC runtime, so they are not copied into the repo — open
them in the design project to see them render. This file is the actionable part.
-->

# Config primitives — prop contracts

Source of truth for `docs/design/repo-changes.md` §4. Verbatim from the mockup unless
marked **[deviation]**.

```ts
interface ModalProps {
  open: boolean
  title: string
  description?: string
  size?: 'sm' | 'md' | 'lg'
  onClose: () => void
  footer?: React.ReactNode
  children: React.ReactNode
}

interface ConfirmDialogProps extends Omit<ModalProps, 'footer'> {
  confirmLabel: string
  tone?: 'default' | 'danger'
  onConfirm: () => void
}

interface FieldProps {
  label: string
  hint?: string
  error?: string
  required?: boolean
  htmlFor?: string
  children: React.ReactNode
}

interface Column<T> {
  key: string
  label: string
  width?: string
  align?: 'left' | 'right'
  priority?: 1 | 2 | 3
  sortable?: boolean
  render?: (row: T) => React.ReactNode
}

interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T) => string
  loading?: boolean
  error?: string
  empty?: React.ReactNode
  toolbar?: React.ReactNode
}

interface EmptyStateProps {
  title: string
  description?: string
  action?: { label: string; onClick: () => void }
}
```

## Gaps in the mockup, and how they're resolved

| Gap | Resolution |
|---|---|
| **`Toggle` has no interface.** It is the only one of the six without a `pre` block; the heading gives `<Toggle checked onChange label>`. | **[deviation]** `interface ToggleProps { checked: boolean; onChange: (checked: boolean) => void; label: string; hint?: string; disabled?: boolean }`. The mockup's markup supplies the behavior: `role="switch"`, `aria-checked`, and an accessible name of `` `${label}, ${on ? 'on' : 'off'}` ``. |
| **`DataTable` cannot express both empty states.** `primitives.md` rule 4 requires *filtered-empty* and *never-configured* to be different designs, but the contract has a single `empty?` slot. | **[deviation]** Split into `empty?: React.ReactNode` (never configured) and `emptyFiltered?: React.ReactNode` (search returned nothing). Without this the caller has to branch, which is exactly what the rule forbids. |
| **`size` widths undefined.** | **[deviation]** Taken from the mockup's own markup: `sm` = 420px (its ConfirmDialog), `md` = 480px (its Modal), `lg` = 720px. |
| **`priority` semantics undefined.** The reference implementation never sets it. | **[deviation]** `1` always visible; `2` hidden below `md`; `3` hidden below `lg`. Default `1`. |
| **`EmptyState` has no icon prop** though §4 says it replaces "centered grey text with an opacity-40 icon". | Left out. The mockup renders a dashed square placeholder, not an icon; add later if a caller needs one. |

## Behavioral contract for `Modal`

From the mockup's own copy — "Escape closes, the backdrop closes, focus is trapped inside,
and the page behind cannot scroll — none of which the current hand-rolled overlays do
consistently." All four are required; `SearchModal` implements only Escape.

## Toast: mockup vs spec

The mockup's toast is bottom-centre, dark pill, `role="status"`, 2600 ms, single (a new one
replaces the old). `repo-changes.md` §5 asks for 2.5 s default / **6 s with Undo**.

**The mockup has no Undo affordance at all**, so §5 is the authority there. CloudPAM's
current `ToastContainer` differs from both: bottom-**right**, white card with a coloured
border, **stacking**, 3000 ms, and no `role="status"`.

## Pool-type colour mockup — validation

`templates/pool-type-color/PoolTypeColor.dc.html` proposes the same fix as
`docs/superpowers/plans/2026-08-04-pool-type-color-and-status-badges.md`, but two of its
claims do not survive contact with the code:

1. **Its headline example is unreachable.** The mockup's "TODAY" column shows pools typed
   `root` and `account` falling through to grey, and it lists eight entries including both.
   But `internal/domain/types.go` defines exactly five pool types and `IsValidPoolType`
   rejects everything else, so **no pool can have type `root` or `account`**. In the
   frontend, `root` is a *node id* (`useSchemaGenerator.ts:22`, on a node whose type is
   `supernet`) and `account` is a *search-result kind* (`SearchModal.tsx:45`). The real
   bug — `subnet` rendering grey in `TreeNode` but orange via `getPoolTypeColor` — stands.
2. **It contradicts itself on the de-emphasis default.** The template's `data-props`
   default is `"ring"`, while `renderVals()` falls back to `'opacity'`, and §6 of
   `repo-changes.md` states opacity is the recommendation. The plan uses **opacity**.

Its code sample also dims on `isUncommitted`, which has no source in `SchemaNode` — and in
the wizard every node is uncommitted, so it would dim the whole tree. The plan dims on
`type === 'subnet'`, which is what §6 actually describes.
