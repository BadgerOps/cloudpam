<!--
Pulled from the "CloudPAM Design System" Claude Design project
(templates/primitives/README.md) on 2026-08-04. Source of truth is the design
project; re-pull rather than editing in place if it changes there.
The working implementations live in that project's templates/primitives/Primitives.dc.html.
-->

# Config primitives — the standard

Every new CloudPAM configuration or administration screen uses these six. Do not
hand-roll an overlay, a label/input pair, a switch, or a table again.

Open `Primitives.dc.html` for the working implementations and full prop contracts.
`../settings-shell/` is the standard applied end to end.

## The set

| Primitive | Use it for | Never instead |
|---|---|---|
| `Modal` | any overlay | a bespoke fixed-inset div |
| `ConfirmDialog` | destructive or irreversible actions | firing the action on click |
| `Field` | every form control | a loose `<label>` + `<input>` |
| `Toggle` | booleans that apply immediately | a checkbox, or a button that reads as a badge |
| `DataTable` | any list of records | a bare `<table>` |
| `EmptyState` | zero results, zero records, filtered-out | centered grey "No results" text |

## Rules

1. **Destructive actions get `ConfirmDialog`, never a bare click.** If the action is
   reversible, skip the dialog and give the toast an Undo instead — one interruption or
   the other, not both.
2. **Validation lives in `Field`'s `error`, not in a toast** and not in a red paragraph
   at the foot of the form. Mark the field that failed.
3. **Every mutation confirms itself** via `ToastContext`. Reversible ones carry Undo at
   6s; plain confirmations get 2.5s. Never toast navigation or anything already visible.
4. **`DataTable` needs all five states** wired: loading, error, populated,
   **filtered-empty**, and **never-configured**. The last two are different designs, not
   one: filtered-empty says "no matches, clear the filter"; never-configured explains what
   the feature is for and offers the first action. A table with only the populated case is
   unfinished.
5. **Columns declare `priority`**, so narrow viewports drop the lowest-value column
   first instead of hiding three at once.
6. **`role="switch"` with state in the accessible name** on every `Toggle`, and
   `aria-invalid` on every errored `Field`.
7. **Status text goes through `StatusBadge`.** If a label renders grey, the vocabulary
   is missing an entry — add it to `getStatusBadgeClass` rather than styling a one-off
   pill.

## Theming

Both templates carry a `theme` tweak. The palette is CSS custom properties set on
`:root` at mount, with light values as inline `var()` fallbacks — so the design paints
immediately while streaming and no style attribute contains a template hole. The root
also gets the `.dark` class, so DS components keep working through their own `dark:`
variants. Reuse this pattern; it is the only approach that gets dark mode into a
template without duplicating markup.

## Pending upstream

`Modal`, `ConfirmDialog`, `Field`, `Toggle`, `DataTable`, and `EmptyState` do not exist
in `cloudpam-ui` yet — see `../repo-changes.md` §4. Until they land, copy from here.
