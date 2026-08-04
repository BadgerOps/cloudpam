# Config Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the six config primitives (`EmptyState`, `Field`, `Toggle`, `Modal`, `ConfirmDialog`, `DataTable`), extend `ToastContext` to support Undo, and migrate the two existing modals onto `Modal` — removing four hand-rolled overlays.

**Architecture:** A new `ui/src/components/primitives/` directory with one component per file and a barrel export. Built leaf-first so each task compiles against what came before: `EmptyState` → `Field`/`Toggle` → `Modal` → `ConfirmDialog` → `DataTable`. The toast change lands separately because it alters an existing shared context. The final task migrates `SearchModal` and `ImportExportModal`, which is what proves `Modal` is actually sufficient rather than merely written.

**Tech Stack:** React 18, TypeScript 5.7, Vite 8, Vitest 4 (jsdom), `@testing-library/react`, Tailwind CSS v4, `lucide-react`.

## Global Constraints

- Frontend only. No Go, no SQL, no API changes.
- All commands run in the Nix dev shell: `nix develop --command bash -c 'cd ui && npx vitest run'`.
- **There is no `setupFiles` in `vite.config.ts`**, so `@testing-library/jest-dom` matchers are NOT available. Use plain assertions — `expect(x).toBeTruthy()`, `expect(x).toBeNull()`, `expect(el.getAttribute('aria-checked')).toBe('true')`. Never `toBeInTheDocument()`.
- Tests follow the existing convention in `ui/src/__tests__/`: `import { describe, expect, it, vi } from 'vitest'`, `vi.hoisted` for mocks.
- These components live in `ui/src`, so **any Tailwind class you write here will be compiled**. That constraint only bites authored design-system previews, not app source.
- Every component is presentational and controlled — no data fetching, no context reads except `Toast*` in Task 7.
- Commit after each task with the message given in its final step.

## Prop contracts

Taken from `docs/design/primitives-contracts.md`, which extracted them from the design
project's `Primitives.dc.html`. Deviations from the mockup are marked there with the reason;
the three that matter here:

- `ToggleProps` is defined by this plan — the mockup has no interface for it.
- `DataTableProps` gains `emptyFiltered` alongside `empty`, because `primitives.md` rule 4
  requires filtered-empty and never-configured to be *different designs*, and one slot
  cannot express both.
- `size` widths and `priority` breakpoints are specified here; the mockup left both undefined.

---

## File Structure

| File | Responsibility |
|---|---|
| `ui/src/components/primitives/EmptyState.tsx` | Zero-state block: title, description, one optional action. |
| `ui/src/components/primitives/Field.tsx` | Label + control + hint + error, with `aria-invalid` wiring. |
| `ui/src/components/primitives/Toggle.tsx` | `role="switch"` with state in the accessible name. |
| `ui/src/components/primitives/Modal.tsx` | The single overlay: backdrop, Escape, focus trap, scroll lock. |
| `ui/src/components/primitives/ConfirmDialog.tsx` | `Modal` + a confirm/cancel footer and a danger tone. |
| `ui/src/components/primitives/DataTable.tsx` | Generic table with five states and responsive column priority. |
| `ui/src/components/primitives/index.ts` | Barrel export. |
| `ui/src/hooks/useToast.ts` | **Modify.** Action, duration, replace-not-stack. |
| `ui/src/components/ToastContainer.tsx` | **Modify.** `role="status"`, render the Undo action. |
| `ui/src/components/SearchModal.tsx` | **Modify.** Migrate onto `Modal`. |
| `ui/src/components/ImportExportModal.tsx` | **Modify.** Migrate onto `Modal`. |

---

### Task 1: EmptyState

**Files:**
- Create: `ui/src/components/primitives/EmptyState.tsx`
- Test: `ui/src/__tests__/primitives/EmptyState.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: `interface EmptyStateProps { title: string; description?: string; action?: { label: string; onClick: () => void } }` and `export default function EmptyState(props: EmptyStateProps)`.

- [ ] **Step 1: Write the failing test**

```tsx
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import EmptyState from '../../components/primitives/EmptyState'

describe('EmptyState', () => {
  it('renders the title', () => {
    render(<EmptyState title="No pools yet" />)
    expect(screen.getByText('No pools yet')).toBeTruthy()
  })

  it('renders an optional description', () => {
    render(<EmptyState title="No pools yet" description="Create one to get started." />)
    expect(screen.getByText('Create one to get started.')).toBeTruthy()
  })

  it('renders no button when no action is given', () => {
    render(<EmptyState title="No pools yet" />)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('invokes the action', () => {
    const onClick = vi.fn()
    render(<EmptyState title="No pools yet" action={{ label: 'Create pool', onClick }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Create pool' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/EmptyState.test.tsx'`
Expected: FAIL — cannot resolve `../../components/primitives/EmptyState`.

- [ ] **Step 3: Write minimal implementation**

```tsx
export interface EmptyStateProps {
  title: string
  description?: string
  action?: { label: string; onClick: () => void }
}

export default function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <div className="px-5 py-12 text-center">
      <div className="mx-auto mb-4 h-10 w-10 rounded-lg border border-dashed border-gray-300 dark:border-gray-600" />
      <p className="text-sm font-semibold text-gray-900 dark:text-gray-100">{title}</p>
      {description && (
        <p className="mt-1.5 text-sm text-gray-500 dark:text-gray-400">{description}</p>
      )}
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          className="mt-4 rounded-lg border border-gray-300 px-3.5 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
        >
          {action.label}
        </button>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/EmptyState.test.tsx'`
Expected: PASS, 4 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/EmptyState.tsx ui/src/__tests__/primitives/EmptyState.test.tsx
git commit -m "feat(ui): add EmptyState primitive"
```

---

### Task 2: Field

**Files:**
- Create: `ui/src/components/primitives/Field.tsx`
- Test: `ui/src/__tests__/primitives/Field.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: `interface FieldProps { label: string; hint?: string; error?: string; required?: boolean; htmlFor?: string; children: React.ReactNode }`.

Rule 2 of `primitives.md`: validation lives here, on the field that failed — not in a toast, not in a red paragraph at the foot of the form. When `error` is set the hint is replaced, not stacked.

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import Field from '../../components/primitives/Field'

describe('Field', () => {
  it('renders label and hint', () => {
    render(
      <Field label="Pool name" hint="Lowercase, hyphens allowed.">
        <input />
      </Field>,
    )
    expect(screen.getByText('Pool name')).toBeTruthy()
    expect(screen.getByText('Lowercase, hyphens allowed.')).toBeTruthy()
  })

  it('replaces the hint with the error rather than stacking them', () => {
    render(
      <Field label="CIDR" hint="Lowercase, hyphens allowed." error="Prefix must be 0-32.">
        <input />
      </Field>,
    )
    expect(screen.getByText('Prefix must be 0-32.')).toBeTruthy()
    expect(screen.queryByText('Lowercase, hyphens allowed.')).toBeNull()
  })

  it('marks the control aria-invalid when errored', () => {
    const { container } = render(
      <Field label="CIDR" error="Prefix must be 0-32.">
        <input />
      </Field>,
    )
    expect(container.querySelector('[aria-invalid="true"]')).toBeTruthy()
  })

  it('does not mark the control aria-invalid when valid', () => {
    const { container } = render(
      <Field label="CIDR">
        <input />
      </Field>,
    )
    expect(container.querySelector('[aria-invalid="true"]')).toBeNull()
  })

  it('associates the label with the control via htmlFor', () => {
    const { container } = render(
      <Field label="Pool name" htmlFor="pool-name">
        <input id="pool-name" />
      </Field>,
    )
    expect(container.querySelector('label[for="pool-name"]')).toBeTruthy()
  })

  it('marks required fields', () => {
    render(
      <Field label="Pool name" required>
        <input />
      </Field>,
    )
    expect(screen.getByText('*')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Field.test.tsx'`
Expected: FAIL — module not found.

- [ ] **Step 3: Write minimal implementation**

`aria-invalid` is applied by cloning the child, so callers do not have to remember it.

```tsx
import { Children, cloneElement, isValidElement } from 'react'

export interface FieldProps {
  label: string
  hint?: string
  error?: string
  required?: boolean
  htmlFor?: string
  children: React.ReactNode
}

export default function Field({ label, hint, error, required, htmlFor, children }: FieldProps) {
  // Wire aria-invalid onto the control itself; a caller should never have to
  // repeat the error state on the input.
  const control = Children.map(children, (child) =>
    error && isValidElement(child)
      ? cloneElement(child as React.ReactElement<{ 'aria-invalid'?: boolean }>, { 'aria-invalid': true })
      : child,
  )

  return (
    <div>
      <label
        htmlFor={htmlFor}
        className="mb-1 block text-xs font-semibold text-gray-700 dark:text-gray-300"
      >
        {label}
        {required && <span className="ml-0.5 text-red-600 dark:text-red-400">*</span>}
      </label>
      {control}
      {error ? (
        <p className="mt-1.5 text-xs font-medium text-red-700 dark:text-red-400">{error}</p>
      ) : (
        hint && <p className="mt-1.5 text-xs text-gray-500 dark:text-gray-400">{hint}</p>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Field.test.tsx'`
Expected: PASS, 6 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/Field.tsx ui/src/__tests__/primitives/Field.test.tsx
git commit -m "feat(ui): add Field primitive with aria-invalid wiring"
```

---

### Task 3: Toggle

**Files:**
- Create: `ui/src/components/primitives/Toggle.tsx`
- Test: `ui/src/__tests__/primitives/Toggle.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: `interface ToggleProps { checked: boolean; onChange: (checked: boolean) => void; label: string; hint?: string; disabled?: boolean }`.

The mockup gives no interface for this one; the shape above comes from its heading
(`<Toggle checked onChange label>`) and its markup. Rule 6 of `primitives.md` requires
`role="switch"` **with the state in the accessible name**, which is why `aria-label` is
`` `${label}, on|off` `` rather than just `label`.

- [ ] **Step 1: Write the failing test**

```tsx
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import Toggle from '../../components/primitives/Toggle'

describe('Toggle', () => {
  it('exposes role=switch with aria-checked', () => {
    render(<Toggle checked={true} onChange={() => {}} label="Require MFA" />)
    const sw = screen.getByRole('switch')
    expect(sw.getAttribute('aria-checked')).toBe('true')
  })

  it('puts the state in the accessible name', () => {
    render(<Toggle checked={false} onChange={() => {}} label="Require MFA" />)
    expect(screen.getByRole('switch').getAttribute('aria-label')).toBe('Require MFA, off')
  })

  it('reports the NEXT value on change, not the current one', () => {
    const onChange = vi.fn()
    render(<Toggle checked={false} onChange={onChange} label="Require MFA" />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalledWith(true)
  })

  it('renders the hint', () => {
    render(<Toggle checked={true} onChange={() => {}} label="Require MFA" hint="Applies at next sign-in." />)
    expect(screen.getByText('Applies at next sign-in.')).toBeTruthy()
  })

  it('does not fire when disabled', () => {
    const onChange = vi.fn()
    render(<Toggle checked={false} onChange={onChange} label="Require MFA" disabled />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Toggle.test.tsx'`
Expected: FAIL — module not found.

- [ ] **Step 3: Write minimal implementation**

```tsx
export interface ToggleProps {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  hint?: string
  disabled?: boolean
}

export default function Toggle({ checked, onChange, label, hint, disabled }: ToggleProps) {
  return (
    <div className="grid grid-cols-[1fr_auto] items-center gap-4 border-b border-gray-100 py-3 dark:border-gray-700">
      <div>
        <div className="text-sm font-semibold text-gray-900 dark:text-gray-100">{label}</div>
        {hint && <div className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{hint}</div>}
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={`${label}, ${checked ? 'on' : 'off'}`}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`flex h-6 w-11 items-center rounded-full px-0.5 transition-colors ${
          checked ? 'justify-end bg-blue-600' : 'justify-start bg-gray-300 dark:bg-gray-600'
        } ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'}`}
      >
        <span className="block h-5 w-5 rounded-full bg-white" />
      </button>
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Toggle.test.tsx'`
Expected: PASS, 5 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/Toggle.tsx ui/src/__tests__/primitives/Toggle.test.tsx
git commit -m "feat(ui): add Toggle primitive with role=switch"
```

---

### Task 4: Modal

**Files:**
- Create: `ui/src/components/primitives/Modal.tsx`
- Test: `ui/src/__tests__/primitives/Modal.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: `interface ModalProps { open: boolean; title: string; description?: string; size?: 'sm' | 'md' | 'lg'; onClose: () => void; footer?: React.ReactNode; children: React.ReactNode }`.

Four behaviors are the whole point, per the mockup: Escape closes, backdrop closes, focus is
trapped, and the page behind cannot scroll. `SearchModal` implements only the first.
Widths: `sm` 420px, `md` 480px (default), `lg` 720px.

- [ ] **Step 1: Write the failing test**

```tsx
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import Modal from '../../components/primitives/Modal'

const open = (over: Partial<React.ComponentProps<typeof Modal>> = {}) =>
  render(
    <Modal open title="Import / Export" onClose={over.onClose ?? (() => {})} {...over}>
      <button>inside</button>
    </Modal>,
  )

describe('Modal', () => {
  it('renders nothing when closed', () => {
    const { container } = render(
      <Modal open={false} title="Import / Export" onClose={() => {}}>
        <p>body</p>
      </Modal>,
    )
    expect(container.querySelector('[role="dialog"]')).toBeNull()
  })

  it('renders a modal dialog with its title', () => {
    open()
    const dialog = screen.getByRole('dialog')
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(screen.getByText('Import / Export')).toBeTruthy()
  })

  it('closes on Escape', () => {
    const onClose = vi.fn()
    open({ onClose })
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on backdrop click but not on panel click', () => {
    const onClose = vi.fn()
    const { container } = open({ onClose })
    fireEvent.click(container.querySelector('[data-testid="modal-backdrop"]')!)
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('dialog'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('locks page scroll while open and restores it on close', () => {
    const { unmount } = open()
    expect(document.body.style.overflow).toBe('hidden')
    unmount()
    expect(document.body.style.overflow).toBe('')
  })

  it('moves focus into the dialog', () => {
    open()
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  })

  it('renders a footer when given', () => {
    open({ footer: <button>Save</button> })
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Modal.test.tsx'`
Expected: FAIL — module not found.

- [ ] **Step 3: Write minimal implementation**

```tsx
import { useEffect, useRef } from 'react'
import { X } from 'lucide-react'

export interface ModalProps {
  open: boolean
  title: string
  description?: string
  size?: 'sm' | 'md' | 'lg'
  onClose: () => void
  footer?: React.ReactNode
  children: React.ReactNode
}

const WIDTHS: Record<NonNullable<ModalProps['size']>, string> = {
  sm: 'max-w-[420px]',
  md: 'max-w-[480px]',
  lg: 'max-w-[720px]',
}

export default function Modal({
  open, title, description, size = 'md', onClose, footer, children,
}: ModalProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab' || !panelRef.current) return
      // Focus trap: cycle within the panel.
      const focusables = panelRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )
      if (focusables.length === 0) return
      const first = focusables[0]
      const last = focusables[focusables.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)

    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    panelRef.current?.focus()

    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = previousOverflow
    }
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      data-testid="modal-backdrop"
      onClick={onClose}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6"
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        className={`w-full ${WIDTHS[size]} rounded-xl bg-white shadow-xl outline-none dark:bg-gray-800`}
      >
        <div className="flex items-start justify-between gap-4 px-6 pt-5">
          <div>
            <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">{title}</h3>
            {description && (
              <p className="mt-2 text-sm text-gray-500 dark:text-gray-400">{description}</p>
            )}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded p-1 hover:bg-gray-100 dark:hover:bg-gray-700"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="px-6 py-4">{children}</div>
        {footer && (
          <div className="flex justify-end gap-2.5 border-t border-gray-100 px-6 py-4 dark:border-gray-700">
            {footer}
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Modal.test.tsx'`
Expected: PASS, 7 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/Modal.tsx ui/src/__tests__/primitives/Modal.test.tsx
git commit -m "feat(ui): add Modal primitive with focus trap and scroll lock"
```

---

### Task 5: ConfirmDialog

**Files:**
- Create: `ui/src/components/primitives/ConfirmDialog.tsx`
- Test: `ui/src/__tests__/primitives/ConfirmDialog.test.tsx`

**Interfaces:**
- Consumes: `Modal`, `ModalProps` from Task 4.
- Produces: `interface ConfirmDialogProps extends Omit<ModalProps, 'footer'> { confirmLabel: string; tone?: 'default' | 'danger'; onConfirm: () => void }`.

Rule 1 of `primitives.md`: destructive actions get this, never a bare click. Reversible ones
get a toast with Undo instead — one interruption or the other, never both.

- [ ] **Step 1: Write the failing test**

```tsx
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import ConfirmDialog from '../../components/primitives/ConfirmDialog'

describe('ConfirmDialog', () => {
  it('renders the confirm label and fires onConfirm', () => {
    const onConfirm = vi.fn()
    render(
      <ConfirmDialog
        open
        title="Revoke terraform-ci?"
        confirmLabel="Revoke key"
        onConfirm={onConfirm}
        onClose={() => {}}
      >
        <p>Any pipeline using this key starts failing immediately.</p>
      </ConfirmDialog>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Revoke key' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('cancels without confirming', () => {
    const onConfirm = vi.fn()
    const onClose = vi.fn()
    render(
      <ConfirmDialog open title="Revoke?" confirmLabel="Revoke" onConfirm={onConfirm} onClose={onClose}>
        <p>body</p>
      </ConfirmDialog>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('styles the danger tone differently from the default', () => {
    const { unmount } = render(
      <ConfirmDialog open title="t" confirmLabel="Go" tone="danger" onConfirm={() => {}} onClose={() => {}}>
        <p>body</p>
      </ConfirmDialog>,
    )
    const danger = screen.getByRole('button', { name: 'Go' }).className
    unmount()

    render(
      <ConfirmDialog open title="t" confirmLabel="Go" onConfirm={() => {}} onClose={() => {}}>
        <p>body</p>
      </ConfirmDialog>,
    )
    expect(screen.getByRole('button', { name: 'Go' }).className).not.toBe(danger)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/ConfirmDialog.test.tsx'`
Expected: FAIL — module not found.

- [ ] **Step 3: Write minimal implementation**

```tsx
import Modal, { type ModalProps } from './Modal'

export interface ConfirmDialogProps extends Omit<ModalProps, 'footer'> {
  confirmLabel: string
  tone?: 'default' | 'danger'
  onConfirm: () => void
}

export default function ConfirmDialog({
  confirmLabel, tone = 'default', onConfirm, onClose, size = 'sm', ...rest
}: ConfirmDialogProps) {
  const confirmClass =
    tone === 'danger'
      ? 'bg-red-700 text-white hover:bg-red-800'
      : 'bg-blue-600 text-white hover:bg-blue-700'

  return (
    <Modal
      {...rest}
      size={size}
      onClose={onClose}
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-semibold text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            className={`rounded-lg px-4 py-2 text-sm font-semibold ${confirmClass}`}
          >
            {confirmLabel}
          </button>
        </>
      }
    />
  )
}
```

Note `Modal` must export its props type for the `Omit` to work — add `export type { ModalProps }` if Task 4 declared it without `export`.

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/ConfirmDialog.test.tsx'`
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/ConfirmDialog.tsx ui/src/__tests__/primitives/ConfirmDialog.test.tsx
git commit -m "feat(ui): add ConfirmDialog primitive"
```

---

### Task 6: DataTable

**Files:**
- Create: `ui/src/components/primitives/DataTable.tsx`
- Test: `ui/src/__tests__/primitives/DataTable.test.tsx`

**Interfaces:**
- Consumes: `EmptyState` from Task 1.
- Produces:
  - `interface Column<T> { key: string; label: string; width?: string; align?: 'left' | 'right'; priority?: 1 | 2 | 3; sortable?: boolean; render?: (row: T) => React.ReactNode }`
  - `interface DataTableProps<T> { columns: Column<T>[]; rows: T[]; rowKey: (row: T) => string; loading?: boolean; error?: string; empty?: React.ReactNode; emptyFiltered?: React.ReactNode; filtered?: boolean; toolbar?: React.ReactNode }`

All five states from rule 4 must be wired. `empty` and `emptyFiltered` are separate slots —
one cannot express both designs — and `filtered` selects between them.
`priority`: `1` always visible, `2` hidden below `md`, `3` hidden below `lg`.

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import DataTable, { type Column } from '../../components/primitives/DataTable'

type Pool = { id: string; name: string; cidr: string }

const columns: Column<Pool>[] = [
  { key: 'name', label: 'Pool' },
  { key: 'cidr', label: 'CIDR', priority: 2 },
]
const rows: Pool[] = [
  { id: '1', name: 'production-us-east', cidr: '10.42.0.0/16' },
  { id: '2', name: 'staging-us-east', cidr: '10.48.0.0/16' },
]
const base = { columns, rows, rowKey: (r: Pool) => r.id }

describe('DataTable', () => {
  it('renders headers and rows', () => {
    render(<DataTable {...base} />)
    expect(screen.getByText('Pool')).toBeTruthy()
    expect(screen.getByText('production-us-east')).toBeTruthy()
    expect(screen.getByText('10.48.0.0/16')).toBeTruthy()
  })

  it('shows the loading state instead of rows', () => {
    render(<DataTable {...base} loading />)
    expect(screen.getByRole('status')).toBeTruthy()
    expect(screen.queryByText('production-us-east')).toBeNull()
  })

  it('shows the error state instead of rows', () => {
    render(<DataTable {...base} error="Could not load pools (503)." />)
    expect(screen.getByText('Could not load pools (503).')).toBeTruthy()
    expect(screen.queryByText('production-us-east')).toBeNull()
  })

  it('shows the never-configured empty state', () => {
    render(<DataTable {...base} rows={[]} empty={<p>No pools yet</p>} emptyFiltered={<p>No matches</p>} />)
    expect(screen.getByText('No pools yet')).toBeTruthy()
    expect(screen.queryByText('No matches')).toBeNull()
  })

  it('shows the filtered-empty state, which is a different design', () => {
    render(
      <DataTable {...base} rows={[]} filtered empty={<p>No pools yet</p>} emptyFiltered={<p>No matches</p>} />,
    )
    expect(screen.getByText('No matches')).toBeTruthy()
    expect(screen.queryByText('No pools yet')).toBeNull()
  })

  it('uses a column render function when given', () => {
    render(
      <DataTable
        {...base}
        columns={[{ key: 'name', label: 'Pool', render: (r) => <em>{r.name.toUpperCase()}</em> }]}
      />,
    )
    expect(screen.getByText('PRODUCTION-US-EAST')).toBeTruthy()
  })

  it('drops low-priority columns at narrow widths via responsive classes', () => {
    const { container } = render(<DataTable {...base} />)
    // priority 2 => hidden below md
    expect(container.querySelector('.hidden.md\\:block')).toBeTruthy()
  })

  it('renders a toolbar when given', () => {
    render(<DataTable {...base} toolbar={<input aria-label="Filter pools" />} />)
    expect(screen.getByLabelText('Filter pools')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/DataTable.test.tsx'`
Expected: FAIL — module not found.

- [ ] **Step 3: Write minimal implementation**

```tsx
export interface Column<T> {
  key: string
  label: string
  width?: string
  align?: 'left' | 'right'
  priority?: 1 | 2 | 3
  sortable?: boolean
  render?: (row: T) => React.ReactNode
}

export interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  rowKey: (row: T) => string
  loading?: boolean
  error?: string
  /** Shown when the collection has never had records. */
  empty?: React.ReactNode
  /** Shown when a filter excluded everything. A different design from `empty`. */
  emptyFiltered?: React.ReactNode
  filtered?: boolean
  toolbar?: React.ReactNode
}

// priority 1 always shows; 2 drops below md; 3 drops below lg.
const PRIORITY_CLASS: Record<1 | 2 | 3, string> = {
  1: '',
  2: 'hidden md:block',
  3: 'hidden lg:block',
}

export default function DataTable<T>({
  columns, rows, rowKey, loading, error, empty, emptyFiltered, filtered, toolbar,
}: DataTableProps<T>) {
  const grid = { gridTemplateColumns: columns.map((c) => c.width ?? '1fr').join(' ') }

  const body = () => {
    if (loading) {
      return (
        <div role="status" className="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">
          Loading…
        </div>
      )
    }
    if (error) {
      return (
        <div role="alert" className="px-5 py-12 text-center text-sm text-red-700 dark:text-red-400">
          {error}
        </div>
      )
    }
    if (rows.length === 0) {
      return <div>{filtered ? emptyFiltered : empty}</div>
    }
    return rows.map((row) => (
      <div
        key={rowKey(row)}
        style={grid}
        className="grid items-center border-b border-gray-100 py-3 text-sm last:border-0 dark:border-gray-700"
      >
        {columns.map((c) => (
          <div
            key={c.key}
            className={`px-3.5 ${c.align === 'right' ? 'text-right' : ''} ${PRIORITY_CLASS[c.priority ?? 1]}`}
          >
            {c.render ? c.render(row) : String((row as Record<string, unknown>)[c.key] ?? '')}
          </div>
        ))}
      </div>
    ))
  }

  return (
    <div>
      {toolbar && <div className="mb-3.5 flex items-center gap-3">{toolbar}</div>}
      <div className="overflow-hidden rounded-lg border border-gray-200 dark:border-gray-700">
        <div
          style={grid}
          className="grid border-b border-gray-200 bg-gray-50 py-2.5 text-xs font-semibold text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400"
        >
          {columns.map((c) => (
            <div
              key={c.key}
              className={`px-3.5 ${c.align === 'right' ? 'text-right' : ''} ${PRIORITY_CLASS[c.priority ?? 1]}`}
            >
              {c.label}
            </div>
          ))}
        </div>
        {body()}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/DataTable.test.tsx'`
Expected: PASS, 8 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/DataTable.tsx ui/src/__tests__/primitives/DataTable.test.tsx
git commit -m "feat(ui): add DataTable primitive with all five states"
```

---

### Task 7: Extend ToastContext for Undo

**Files:**
- Modify: `ui/src/hooks/useToast.ts`
- Modify: `ui/src/components/ToastContainer.tsx`
- Test: `ui/src/__tests__/primitives/Toast.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `interface ToastAction { label: string; onClick: () => void }`
  - `interface Toast { id: string; message: string; type: 'info' | 'error' | 'success'; action?: ToastAction }`
  - `showToast(message: string, type?: 'info' | 'error' | 'success', action?: ToastAction): void`

Three behavior changes from §5, all breaking the current implementation:
**one toast at a time** (a new one replaces the old, rather than stacking), **6000 ms when
an action is present** and 2500 ms otherwise (currently a flat 3000 ms), and
**`role="status"`** on the container so screen readers announce it.

`showToast`'s existing two-argument signature is preserved, so no current caller breaks.

- [ ] **Step 1: Write the failing test**

```tsx
import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ToastContext, useToastState } from '../../hooks/useToast'
import ToastContainer from '../../components/ToastContainer'

function Harness() {
  const state = useToastState()
  return (
    <ToastContext.Provider value={state}>
      <button onClick={() => state.showToast('Saved', 'success')}>plain</button>
      <button onClick={() => state.showToast('Deactivated', 'success', { label: 'Undo', onClick: undo })}>
        withUndo
      </button>
      <ToastContainer />
    </ToastContext.Provider>
  )
}
const undo = vi.fn()

beforeEach(() => { vi.useFakeTimers(); undo.mockClear() })
afterEach(() => { vi.useRealTimers() })

describe('toast', () => {
  it('announces via role=status', () => {
    render(<Harness />)
    act(() => { fireEvent.click(screen.getByText('plain')) })
    expect(screen.getByRole('status')).toBeTruthy()
  })

  it('shows one toast at a time - a new one replaces the old', () => {
    render(<Harness />)
    act(() => { fireEvent.click(screen.getByText('plain')) })
    act(() => { fireEvent.click(screen.getByText('withUndo')) })
    expect(screen.queryByText('Saved')).toBeNull()
    expect(screen.getByText('Deactivated')).toBeTruthy()
  })

  it('expires a plain toast at 2500ms', () => {
    render(<Harness />)
    act(() => { fireEvent.click(screen.getByText('plain')) })
    act(() => { vi.advanceTimersByTime(2499) })
    expect(screen.queryByText('Saved')).toBeTruthy()
    act(() => { vi.advanceTimersByTime(2) })
    expect(screen.queryByText('Saved')).toBeNull()
  })

  it('gives an Undo toast the longer 6000ms life', () => {
    render(<Harness />)
    act(() => { fireEvent.click(screen.getByText('withUndo')) })
    act(() => { vi.advanceTimersByTime(2600) })
    expect(screen.queryByText('Deactivated')).toBeTruthy()
    act(() => { vi.advanceTimersByTime(3500) })
    expect(screen.queryByText('Deactivated')).toBeNull()
  })

  it('invokes the action and dismisses', () => {
    render(<Harness />)
    act(() => { fireEvent.click(screen.getByText('withUndo')) })
    act(() => { fireEvent.click(screen.getByRole('button', { name: 'Undo' })) })
    expect(undo).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('Deactivated')).toBeNull()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Toast.test.tsx'`
Expected: FAIL — toasts stack, there is no `role="status"`, and `showToast` takes no action.

- [ ] **Step 3: Write minimal implementation**

Replace the body of `ui/src/hooks/useToast.ts`, keeping the existing exports:

```ts
import { createContext, useCallback, useContext, useRef, useState } from 'react'

export interface ToastAction {
  label: string
  onClick: () => void
}

export interface Toast {
  id: string
  message: string
  type: 'info' | 'error' | 'success'
  action?: ToastAction
}

export interface ToastContextValue {
  toasts: Toast[]
  showToast: (message: string, type?: Toast['type'], action?: ToastAction) => void
  dismissToast: (id: string) => void
}

export const ToastContext = createContext<ToastContextValue>({
  toasts: [],
  showToast: () => {},
  dismissToast: () => {},
})

// Reversible actions carry Undo and need longer to read and act on; plain
// confirmations do not. See docs/design/repo-changes.md §5.
const PLAIN_MS = 2500
const ACTION_MS = 6000

export function useToastState(): ToastContextValue {
  const [toasts, setToasts] = useState<Toast[]>([])
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const dismissToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const showToast = useCallback(
    (message: string, type: Toast['type'] = 'info', action?: ToastAction) => {
      const id = Math.random().toString(36).slice(2)
      // One at a time: a new toast replaces the old rather than stacking.
      if (timerRef.current) clearTimeout(timerRef.current)
      setToasts([{ id, message, type, action }])
      timerRef.current = setTimeout(
        () => setToasts((prev) => prev.filter((t) => t.id !== id)),
        action ? ACTION_MS : PLAIN_MS,
      )
    },
    [],
  )

  return { toasts, showToast, dismissToast }
}

export function useToast(): ToastContextValue {
  return useContext(ToastContext)
}
```

Then `ui/src/components/ToastContainer.tsx`:

```tsx
import { useToast } from '../hooks/useToast'

export default function ToastContainer() {
  const { toasts, dismissToast } = useToast()

  if (toasts.length === 0) return null

  return (
    <div role="status" className="fixed right-4 bottom-4 z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`min-w-60 rounded-lg border bg-white px-4 py-3 shadow-lg dark:bg-gray-800 ${
            t.type === 'error'
              ? 'border-red-200 dark:border-red-800'
              : t.type === 'success'
                ? 'border-green-200 dark:border-green-800'
                : 'border-blue-200 dark:border-blue-800'
          }`}
        >
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-sm font-medium">
                {t.type === 'error' ? 'Error' : t.type === 'success' ? 'Success' : 'Info'}
              </div>
              <div className="text-sm text-gray-600 dark:text-gray-300">{t.message}</div>
            </div>
            {t.action && (
              <button
                type="button"
                onClick={() => {
                  t.action?.onClick()
                  dismissToast(t.id)
                }}
                className="shrink-0 text-sm font-semibold text-blue-600 hover:text-blue-700 dark:text-blue-400"
              >
                {t.action.label}
              </button>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/primitives/Toast.test.tsx'`
Expected: PASS, 5 tests.

Then confirm no existing caller broke — `showToast(msg, type)` still works:

Run: `nix develop --command bash -c 'cd ui && npx tsc --noEmit && npx vitest run'`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add ui/src/hooks/useToast.ts ui/src/components/ToastContainer.tsx ui/src/__tests__/primitives/Toast.test.tsx
git commit -m "feat(ui): support Undo actions and single-toast semantics

Adds an optional action with a 6s life (2.5s without), replaces stacking
with one-at-a-time, and puts role=status on the container so screen readers
announce it. showToast's two-argument form is unchanged."
```

---

### Task 8: Migrate SearchModal and ImportExportModal onto Modal

**Files:**
- Modify: `ui/src/components/ImportExportModal.tsx`
- Modify: `ui/src/components/SearchModal.tsx`
- Create: `ui/src/components/primitives/index.ts`
- Test: existing `ui/src/__tests__/ImportExportModal.test.tsx` and `SearchModal.test.tsx` must keep passing.

**Interfaces:**
- Consumes: `Modal` from Task 4.
- Produces: `ui/src/components/primitives/index.ts` re-exporting all six primitives and their prop types.

This is the task that proves `Modal` is sufficient. `ImportExportModal` currently builds its
own backdrop at line 91 (`fixed inset-0 … bg-black/40`) with a click-to-close and no Escape
handler and no focus trap; `SearchModal` has Escape but no focus trap. Both keep their own
`open`/`onClose` props — only the overlay chrome moves.

**`SearchModal` is a command palette, not a dialog box.** It is top-aligned with no title
chrome, so migrating it wholesale would change its appearance. Migrate `ImportExportModal`
fully; for `SearchModal`, adopt `Modal` only if it can be done without visual change — if
not, leave it and record why in the commit message. Do not force it.

- [ ] **Step 1: Confirm the existing tests pass before touching anything**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/ImportExportModal.test.tsx src/__tests__/SearchModal.test.tsx'`
Expected: PASS. This is the baseline the migration must preserve.

- [ ] **Step 2: Write the barrel**

Create `ui/src/components/primitives/index.ts`:

```ts
export { default as EmptyState, type EmptyStateProps } from './EmptyState'
export { default as Field, type FieldProps } from './Field'
export { default as Toggle, type ToggleProps } from './Toggle'
export { default as Modal, type ModalProps } from './Modal'
export { default as ConfirmDialog, type ConfirmDialogProps } from './ConfirmDialog'
export { default as DataTable, type Column, type DataTableProps } from './DataTable'
```

- [ ] **Step 3: Migrate ImportExportModal**

Replace its outer backdrop and panel markup with `Modal`, keeping the two-pane body exactly
as it is:

```tsx
import { Modal } from './primitives'

// …inside the component, replacing the outer <div className="fixed inset-0 …"> wrapper:
return (
  <Modal open={open} onClose={onClose} title="Import / Export" size="lg">
    {/* the existing Export Data / Import Data two-pane body, unchanged */}
  </Modal>
)
```

Delete the now-dead close button and backdrop `onClick` — `Modal` owns both.

- [ ] **Step 4: Run the full suite and type check**

Run: `nix develop --command bash -c 'cd ui && npx tsc --noEmit && npx vitest run'`
Expected: PASS, with the pre-existing ImportExportModal tests still green.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/primitives/index.ts ui/src/components/ImportExportModal.tsx ui/src/components/SearchModal.tsx
git commit -m "refactor(ui): migrate ImportExportModal onto the Modal primitive

Removes a hand-rolled backdrop that had click-to-close but no Escape
handler and no focus trap."
```

---

### Task 9: Re-sync the design system

`ImportExportModal` and `ToastContainer` are both synced components, and both changed. Their
preview cards will move.

- [ ] **Step 1: Rebuild the frontend**

Run: `nix develop --command bash -c 'cd ui && npm run build'`
Expected: exit 0.

- [ ] **Step 2: Update the ToastContainer preview**

`.design-sync/previews/ToastContainer.tsx` has a `Stacked` cell showing three toasts at
once. **That state no longer exists** — §5 made toasts one-at-a-time. Replace it with a
`WithUndo` cell exercising the new action, and drop `Stacked`.

- [ ] **Step 3: Re-sync**

Follow `.design-sync/NOTES.md`. From the repo root:

```bash
nix develop --command bash -c 'cd ui && npx tsc -p tsconfig.dts.json'
nix develop --command node .ds-sync/resync.mjs --config .design-sync/config.json \
  --node-modules ui/node_modules --out ./ds-bundle \
  --remote .design-sync/.cache/remote-sync.json
```

Expected: `verification.changed` lists `ImportExportModal` and `ToastContainer`;
`upload.any: true`.

- [ ] **Step 4: Re-grade and upload**

Read the two review sheets, confirm the cards render, write grades, and upload per the
design-sync skill's atomic path.

- [ ] **Step 5: Commit**

```bash
git add .design-sync
git commit -m "chore(design-sync): re-sync after primitives and toast changes"
```

---

## Self-Review

**1. Spec coverage.** Covers §4 in full — all six primitives with the mockup's contracts —
and §5's toast rules (Undo at 6s, plain at 2.5s, one at a time, `role="status"`). §5's
*usage* guidance ("toast every mutation whose result is off-screen") is a rule for callers,
not a component change; it applies when Plan C wires the Settings panes. Out of scope: §1,
§2, §2b, §2c, §2d (Plan C), §3 and §6 (Plan A), §1b/§1c/§1d (Plan D).

**2. Placeholder scan.** No TBDs. Every code step carries the literal implementation. Task 8
deliberately leaves `SearchModal` conditional — it is a command palette rather than a dialog,
and forcing it through `Modal` would change its appearance; the plan says to judge and record
rather than pretend the answer is known.

**3. Type consistency.** `ModalProps` is defined in Task 4 and consumed by Task 5's `Omit`.
`Column<T>` and `DataTableProps<T>` are defined in Task 6 and re-exported in Task 8's barrel.
`ToastAction` is defined in Task 7 and used by both `useToast.ts` and `ToastContainer.tsx`.
`EmptyState` (Task 1) is available to `DataTable` (Task 6) — note `DataTable` takes empty
states as `ReactNode` slots rather than importing `EmptyState` directly, so callers can pass
anything.

**Deviations from the mockup, all recorded in `docs/design/primitives-contracts.md`:**
`ToggleProps` invented (no interface in the mockup); `DataTable` gains `emptyFiltered` +
`filtered` because one slot cannot satisfy rule 4; `size` widths and `priority` breakpoints
specified because the mockup left both undefined.

---

## Plan C readiness

Plan C (Settings shell) consumes every primitive here. Before writing it, pull the two
remaining mockups — `templates/settings-shell/SettingsShell.dc.html` and
`templates/first-run/FirstRun.dc.html` — which hold the People table, the permissions
matrix, the role dialogs, and the five first-run steps. Plan C also needs Plan D's
`409 role_in_use` response shape for the role-delete dialog.
