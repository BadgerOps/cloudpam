# Pool-Type Color Unification & Status Badge Vocabulary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make pool-type color a single source of truth so the same pool never renders a different color in two places, and give `locked` / `disabled` / `revoked` / `idle` their own badge colors instead of falling through to grey.

**Architecture:** Introduce one `POOL_TYPES` constant that owns the id → label → dot-class mapping. `getPoolTypeColor` reads from it, the Schema Planner legend renders from it, and `TreeNode`'s private `TYPE_COLORS` map is deleted. Subnet de-emphasis in the wizard moves from "replace the hue with grey" to "keep the hue, dim the row", so grey once again means only "unrecognised type". Separately, four missing entries are added to `getStatusBadgeClass`.

**Tech Stack:** React 18, TypeScript 5.7, Vite 8, Vitest 4, Tailwind CSS v4.

## Global Constraints

- Frontend only. No Go, no SQL, no API changes. Nothing in this plan touches `internal/` or `migrations/`.
- All commands run inside the Nix dev shell: prefix with `nix develop --command`, e.g. `nix develop --command bash -c 'cd ui && npx vitest run'`. Node is not on the default PATH.
- Tailwind v4 compiles only classes it finds as **literal strings** in `ui/src`. Never build a class name by concatenation — write the full literal (`'bg-purple-500'`, not `` `bg-${hue}-500` ``), or it will silently not exist at runtime.
- `getStatusBadgeClass` lives in `ui/src/utils/format.ts`. There is **no** `ui/src/utils/badges.ts` — the spec's §3 path is wrong; use `format.ts`.
- Existing behavior that must not regress: `getPoolTypeColor('unknown')` returns `'bg-gray-400'`, and `getPoolTypeColor('supernet')` returns `'bg-purple-500'`. Both are asserted in `ui/src/__tests__/format.test.ts`.
- Commit after each task with the message given in the task's final step.

---

## Decisions — resolved

1. **Which types belong in the legend? — RESOLVED: exactly five.** `internal/domain/types.go:9-13` is authoritative: `supernet`, `region`, `environment`, `vpc`, `subnet`. `IsValidPoolType` rejects anything else, so no other value can reach the frontend from the API.

   `getPoolTypeColor`'s `root` and `account` keys are **dead** and are deleted by this plan:
   - `root` is a *node id*, not a type — `ui/src/wizard/hooks/useSchemaGenerator.ts:22` sets `id: 'root'` on a node whose `type` is `'supernet'` (line 24).
   - `account` is a *search-result kind* — the only occurrence is `ui/src/components/SearchModal.tsx:45` (`i.type === 'account'`, separating pool hits from account hits). It never passes through `getPoolTypeColor`, and there is no `PoolTypeAccount` in Go.

   The current legend (`ui/src/wizard/steps/PreviewStep.tsx:82-85`) therefore labels two things the system cannot produce (*Root*, *Account*) while omitting two it can (*Supernet*, *Subnet*). All five real types go in the legend, so no `inLegend` flag is needed.

2. **Should `getPoolTypeColor` keep accepting `string`? — Yes, deliberately.** It is typed `(type: string)` today, which is what lets unknown values reach the grey fallback. Do not "tighten" it to `PoolType`: the fallback is load-bearing, and after this change grey means exactly one thing.

---

## File Structure

| File | Responsibility |
|---|---|
| `ui/src/utils/poolTypes.ts` | **New.** `POOL_TYPES` — the single source of truth for pool-type id, human label, and dot class. Plus `UNKNOWN_POOL_TYPE_DOT`. |
| `ui/src/utils/format.ts` | **Modify.** `getPoolTypeColor` delegates to `POOL_TYPES`; `getStatusBadgeClass` gains four entries. |
| `ui/src/wizard/components/TreeNode.tsx` | **Modify.** Delete the private `TYPE_COLORS`; use the shared color; dim subnet rows via opacity. |
| `ui/src/wizard/steps/PreviewStep.tsx` | **Modify.** Legend renders from `POOL_TYPES` instead of four literal swatches. |
| `ui/src/__tests__/poolTypes.test.ts` | **New.** Guards the constant's shape and the no-duplicate-grey invariant. |
| `ui/src/__tests__/format.test.ts` | **Modify.** Extend for the new status colors and the shared color source. |
| `ui/src/__tests__/PoolTree.test.tsx` | **Modify.** Add the subnet-hue regression test. |

---

### Task 1: Create the `POOL_TYPES` single source of truth

**Files:**
- Create: `ui/src/utils/poolTypes.ts`
- Test: `ui/src/__tests__/poolTypes.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `interface PoolTypeMeta { id: PoolType; label: string; dot: string }`
  - `const POOL_TYPES: readonly PoolTypeMeta[]`
  - `const UNKNOWN_POOL_TYPE_DOT = 'bg-gray-400'`
  - `function poolTypeDot(type: string): string`

- [ ] **Step 1: Write the failing test**

Create `ui/src/__tests__/poolTypes.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { POOL_TYPES, UNKNOWN_POOL_TYPE_DOT, poolTypeDot } from '../utils/poolTypes'

describe('POOL_TYPES', () => {
  it('matches the five pool types the Go domain accepts', () => {
    // internal/domain/types.go ValidPoolTypes is authoritative.
    const ids = POOL_TYPES.map((t) => t.id)
    expect(ids).toEqual(['supernet', 'region', 'environment', 'vpc', 'subnet'])
  })

  it('does not resurrect the dead root/account keys', () => {
    // 'root' is a node id (useSchemaGenerator) and 'account' is a search-result
    // kind (SearchModal) — neither is a pool type.
    expect(poolTypeDot('root')).toBe(UNKNOWN_POOL_TYPE_DOT)
    expect(poolTypeDot('account')).toBe(UNKNOWN_POOL_TYPE_DOT)
  })

  it('never assigns the unknown-type grey to a known type', () => {
    // Grey must mean exactly one thing: "type not recognised". If a known
    // type also renders grey, an unrecognised type becomes invisible.
    const greys = POOL_TYPES.filter((t) => t.dot === UNKNOWN_POOL_TYPE_DOT)
    expect(greys).toEqual([])
  })

  it('gives every type a non-empty label', () => {
    for (const t of POOL_TYPES) {
      expect(t.label.length).toBeGreaterThan(0)
    }
  })

  it('resolves a known type to its dot class', () => {
    expect(poolTypeDot('subnet')).toBe('bg-orange-500')
    expect(poolTypeDot('supernet')).toBe('bg-purple-500')
  })

  it('falls back to grey for an unrecognised type', () => {
    expect(poolTypeDot('nonsense')).toBe(UNKNOWN_POOL_TYPE_DOT)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/poolTypes.test.ts'`
Expected: FAIL — `Failed to resolve import "../utils/poolTypes"`.

- [ ] **Step 3: Write minimal implementation**

Create `ui/src/utils/poolTypes.ts`:

```ts
// Single source of truth for pool-type presentation.
//
// Grey is reserved: it means "type not recognised". Nothing in POOL_TYPES may
// use UNKNOWN_POOL_TYPE_DOT, or an unknown type becomes indistinguishable from
// a known one. Dot classes are written as full literal strings because
// Tailwind v4 only compiles classes it can find literally in ui/src.
// The five ids mirror internal/domain/types.go ValidPoolTypes, which the API
// enforces via IsValidPoolType. Do not add 'root' or 'account': the former is a
// node id in the schema generator, the latter a search-result kind.
import type { PoolType } from '../api/types'

export interface PoolTypeMeta {
  id: PoolType
  label: string
  dot: string
}

export const UNKNOWN_POOL_TYPE_DOT = 'bg-gray-400'

export const POOL_TYPES: readonly PoolTypeMeta[] = [
  { id: 'supernet', label: 'Supernet', dot: 'bg-purple-500' },
  { id: 'region', label: 'Region', dot: 'bg-blue-500' },
  { id: 'environment', label: 'Environment', dot: 'bg-green-500' },
  { id: 'vpc', label: 'VPC', dot: 'bg-amber-500' },
  { id: 'subnet', label: 'Subnet', dot: 'bg-orange-500' },
]

const BY_ID = new Map(POOL_TYPES.map((t) => [t.id, t]))

export function poolTypeDot(type: string): string {
  return BY_ID.get(type)?.dot ?? UNKNOWN_POOL_TYPE_DOT
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/poolTypes.test.ts'`
Expected: PASS, 5 tests.

- [ ] **Step 5: Commit**

```bash
git add ui/src/utils/poolTypes.ts ui/src/__tests__/poolTypes.test.ts
git commit -m "feat(ui): add POOL_TYPES as the single source of pool-type color"
```

---

### Task 2: Point `getPoolTypeColor` at `POOL_TYPES`

**Files:**
- Modify: `ui/src/utils/format.ts:31-42`
- Test: `ui/src/__tests__/format.test.ts`

**Interfaces:**
- Consumes: `poolTypeDot`, `UNKNOWN_POOL_TYPE_DOT` from Task 1.
- Produces: `getPoolTypeColor(type: string): string` — unchanged signature, now backed by `POOL_TYPES`.

- [ ] **Step 1: Write the failing test**

Append to `ui/src/__tests__/format.test.ts` (inside the existing top-level `describe`, next to the current `getPoolTypeColor` test):

```ts
  it('getPoolTypeColor agrees with POOL_TYPES for every known type', () => {
    for (const t of POOL_TYPES) {
      expect(getPoolTypeColor(t.id)).toBe(t.dot)
    }
  })

  it('getPoolTypeColor gives subnet its own hue, not grey', () => {
    // Regression: the wizard used to grey out subnets, which collided with
    // the unknown-type fallback.
    expect(getPoolTypeColor('subnet')).toBe('bg-orange-500')
    expect(getPoolTypeColor('subnet')).not.toBe(UNKNOWN_POOL_TYPE_DOT)
  })
```

Add to that file's imports:

```ts
import { POOL_TYPES, UNKNOWN_POOL_TYPE_DOT } from '../utils/poolTypes'
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/format.test.ts'`
Expected: The two new tests FAIL — `getPoolTypeColor('root')` currently returns `'bg-purple-500'` but is a separate literal map, and `account` is absent from the new constant's ordering check. If both happen to pass, the map still needs replacing for Task 4 to have a single source; continue.

- [ ] **Step 3: Write minimal implementation**

In `ui/src/utils/format.ts`, add the import at the top of the file:

```ts
import { poolTypeDot } from './poolTypes'
```

Replace the whole existing `getPoolTypeColor` function (lines 31-42) with:

```ts
export function getPoolTypeColor(type: string): string {
  return poolTypeDot(type)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/format.test.ts'`
Expected: PASS, including the pre-existing `getPoolTypeColor('unknown') === 'bg-gray-400'` assertion.

- [ ] **Step 5: Commit**

```bash
git add ui/src/utils/format.ts ui/src/__tests__/format.test.ts
git commit -m "refactor(ui): back getPoolTypeColor with POOL_TYPES"
```

---

### Task 3: Delete `TYPE_COLORS` and dim subnets without destroying the hue

**Files:**
- Modify: `ui/src/wizard/components/TreeNode.tsx:6-12` (delete `TYPE_COLORS`), `:26-45` (row + dot)
- Test: `ui/src/__tests__/PoolTree.test.tsx`

**Interfaces:**
- Consumes: `getPoolTypeColor` from Task 2.
- Produces: no new exports. `TreeNode`'s rendered dot class now comes from the shared source.

- [ ] **Step 1: Write the failing test**

Append to `ui/src/__tests__/PoolTree.test.tsx`:

```tsx
import TreeNode from '../wizard/components/TreeNode'
import type { SchemaNode } from '../wizard/utils/cidr'

describe('TreeNode pool-type color', () => {
  const node = (type: SchemaNode['type']): SchemaNode => ({
    id: 'n1',
    name: 'node',
    type,
    cidr: '10.0.0.0/24',
    children: [],
  })

  it('gives a subnet its own hue instead of grey', () => {
    const { container } = render(<TreeNode node={node('subnet')} />)
    expect(container.querySelector('.bg-orange-500')).toBeTruthy()
    expect(container.querySelector('.bg-gray-400')).toBeNull()
  })

  it('dims the subnet row rather than recoloring the dot', () => {
    const { container } = render(<TreeNode node={node('subnet')} />)
    expect(container.querySelector('.opacity-50')).toBeTruthy()
  })

  it('does not dim a non-subnet row', () => {
    const { container } = render(<TreeNode node={node('region')} />)
    expect(container.querySelector('.opacity-50')).toBeNull()
    expect(container.querySelector('.bg-blue-500')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/PoolTree.test.tsx'`
Expected: FAIL — the subnet dot renders `bg-gray-400 dark:bg-gray-500`, so `.bg-orange-500` is null and `.bg-gray-400` is found.

- [ ] **Step 3: Write minimal implementation**

In `ui/src/wizard/components/TreeNode.tsx`:

Delete the entire `TYPE_COLORS` constant (lines 6-12):

```ts
const TYPE_COLORS: Record<string, string> = {
  supernet: 'bg-purple-500',
  region: 'bg-blue-500',
  environment: 'bg-green-500',
  vpc: 'bg-amber-500',
  subnet: 'bg-gray-400 dark:bg-gray-500',
}
```

Add to the imports at the top of the file:

```ts
import { getPoolTypeColor } from '../../utils/format'
```

Replace the dot element:

```tsx
<div className={`w-2 h-2 rounded-full ${TYPE_COLORS[node.type] ?? 'bg-gray-400 dark:bg-gray-500'}`} />
```

with:

```tsx
<div className={`w-2 h-2 rounded-full ${getPoolTypeColor(node.type)}`} />
```

Then dim the row instead. Find the row `<div>` whose `className` starts with `flex items-center gap-2 py-1.5 px-2 rounded` and add the opacity to its template literal, immediately after the `node.conflict` ternary:

```tsx
className={`flex items-center gap-2 py-1.5 px-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 cursor-pointer ${
  node.conflict ? 'bg-red-50 dark:bg-red-900/30 hover:bg-red-100 dark:hover:bg-red-900/50' : ''
} ${node.type === 'subnet' ? 'opacity-50' : ''}`}
```

`opacity-50` is a literal string here so Tailwind will compile it.

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/PoolTree.test.tsx'`
Expected: PASS.

Then confirm nothing else referenced the deleted map:

Run: `grep -rn "TYPE_COLORS" ui/src`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add ui/src/wizard/components/TreeNode.tsx ui/src/__tests__/PoolTree.test.tsx
git commit -m "fix(ui): keep subnet hue in the wizard tree, dim the row instead

TYPE_COLORS greyed out subnets and omitted root/account entirely, so the
same pool rendered purple in PoolTree and grey in the Schema Planner, and
grey meant both 'subnet' and 'unknown type'."
```

---

### Task 4: Render the Schema Planner legend from `POOL_TYPES`

Decision 1 is resolved: all five real pool types appear in the legend. This task is unblocked.

**Files:**
- Modify: `ui/src/wizard/steps/PreviewStep.tsx:80-87`

**Interfaces:**
- Consumes: `POOL_TYPES` from Task 1.
- Produces: no new exports.

- [ ] **Step 1: Write the failing test**

Append to `ui/src/__tests__/DimensionsStep.test.tsx` — or create `ui/src/__tests__/PreviewStepLegend.test.tsx` if you prefer isolation; this plan assumes the new file:

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import PreviewStep from '../wizard/steps/PreviewStep'
import type { SchemaNode } from '../wizard/utils/cidr'
import { POOL_TYPES } from '../utils/poolTypes'

const schema: SchemaNode = {
  id: 'root',
  name: 'Corporate Supernet',
  type: 'supernet',
  cidr: '10.0.0.0/8',
  children: [],
}

describe('PreviewStep legend', () => {
  it('lists every legend-visible pool type', () => {
    render(
      <PreviewStep
        schema={schema}
        conflicts={[]}
        conflictsLoading={false}
        conflictsError={null}
        onExport={() => {}}
      />,
    )
    for (const t of POOL_TYPES) {
      expect(screen.getByText(t.label)).toBeTruthy()
    }
  })

  it('includes Subnet, which the old literal legend omitted', () => {
    render(
      <PreviewStep
        schema={schema}
        conflicts={[]}
        conflictsLoading={false}
        conflictsError={null}
        onExport={() => {}}
      />,
    )
    expect(screen.getByText('Subnet')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/PreviewStepLegend.test.tsx'`
Expected: FAIL — the legend has four hard-coded labels (Root, Region, Environment, Account) and no "Subnet" or "Supernet".

- [ ] **Step 3: Write minimal implementation**

In `ui/src/wizard/steps/PreviewStep.tsx`, add to the imports:

```ts
import { POOL_TYPES } from '../../utils/poolTypes'
```

Replace the four literal `<span>` swatches at lines 82-85 with:

```tsx
{POOL_TYPES.map((t) => (
  <span key={t.id} className="flex items-center gap-1">
    <div className={`w-2 h-2 rounded-full ${t.dot}`} /> {t.label}
  </span>
))}
```

All five types are legend-visible, so there is no filter. If a sixth pool type is ever added to `internal/domain/types.go`, adding it to `POOL_TYPES` is the only frontend change needed.

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/PreviewStepLegend.test.tsx'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/wizard/steps/PreviewStep.tsx ui/src/__tests__/PreviewStepLegend.test.tsx
git commit -m "fix(ui): render Schema Planner legend from POOL_TYPES

The legend was four literal swatches naming Root and Account, neither of
which the wizard emits, while omitting Supernet and Subnet, which it does."
```

---

### Task 5: Give `locked` / `disabled` / `revoked` / `idle` their own colors

**Files:**
- Modify: `ui/src/utils/format.ts:69-82` (`getStatusBadgeClass`)
- Test: `ui/src/__tests__/format.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `getStatusBadgeClass` — unchanged signature, four new recognised statuses.

- [ ] **Step 1: Write the failing test**

Append inside the existing top-level `describe` in `ui/src/__tests__/format.test.ts`:

```ts
  it('getStatusBadgeClass colors account lifecycle states distinctly', () => {
    expect(getStatusBadgeClass('locked')).toContain('amber')
    expect(getStatusBadgeClass('disabled')).toContain('red')
    expect(getStatusBadgeClass('revoked')).toContain('red')
    expect(getStatusBadgeClass('idle')).toContain('amber')
  })

  it('getStatusBadgeClass no longer greys out known account states', () => {
    // Regression: a locked account and a planned pool rendered identically.
    const grey = getStatusBadgeClass('some-unknown-status')
    for (const s of ['locked', 'disabled', 'revoked', 'idle']) {
      expect(getStatusBadgeClass(s)).not.toBe(grey)
    }
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/format.test.ts'`
Expected: FAIL — all four currently return the grey default, so `.toContain('amber')` fails.

- [ ] **Step 3: Write minimal implementation**

In `ui/src/utils/format.ts`, add these four entries to the `classes` record inside `getStatusBadgeClass`, alongside the existing `active` / `planned` / `deprecated` keys:

```ts
    locked: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
    disabled: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    revoked: 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300',
    idle: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
```

All eight classes already ship in the compiled stylesheet at `-100` / `-700` / `dark:-900` / `dark:-300`, so no new Tailwind surface is introduced.

- [ ] **Step 4: Run test to verify it passes**

Run: `nix develop --command bash -c 'cd ui && npx vitest run src/__tests__/format.test.ts'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/utils/format.ts ui/src/__tests__/format.test.ts
git commit -m "fix(ui): color locked/disabled/revoked/idle status badges

All four fell through to the grey default, making a locked account and a
planned pool visually identical."
```

---

### Task 6: Remove the dead `dark:hover:bg-gray-750` class

**Files:**
- Modify: `ui/src/components/UsersAdminPanel.tsx:259`
- Modify: `ui/src/pages/ApiKeysPage.tsx:276`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing.

`gray-750` is not a Tailwind color. It compiles to nothing, so these two tables have no dark-mode row hover at all. The spec flagged only `UsersAdminPanel`; `ApiKeysPage` has the identical bug.

- [ ] **Step 1: Confirm the class is genuinely dead**

Run:
```bash
nix develop --command node -e 'console.log(require("fs").readFileSync("ui/.ds-css/compiled.css","utf8").includes("gray-750"))'
```
Expected: `false` — the class does not exist in the compiled stylesheet.

Run: `grep -rn "gray-750" ui/src`
Expected: exactly two hits, the two lines above.

- [ ] **Step 2: Replace both occurrences**

In both files, change:

```
dark:hover:bg-gray-750
```

to:

```
dark:hover:bg-gray-700
```

`dark:hover:bg-gray-700` is already present in the compiled stylesheet.

- [ ] **Step 3: Verify no occurrences remain**

Run: `grep -rn "gray-750" ui/src`
Expected: no output.

- [ ] **Step 4: Run the full suite and type check**

Run: `nix develop --command bash -c 'cd ui && npx tsc --noEmit && npx vitest run'`
Expected: type check clean, all tests pass.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/UsersAdminPanel.tsx ui/src/pages/ApiKeysPage.tsx
git commit -m "fix(ui): use a real Tailwind color for dark-mode row hover

gray-750 does not exist, so dark:hover:bg-gray-750 compiled to nothing and
neither table had a dark-mode hover state."
```

---

### Task 7: Rebuild and re-sync the design system

The design system ships CloudPAM's **compiled** Tailwind CSS, so the color changes above do not reach claude.ai/design until the bundle is rebuilt. `TreeNode` and `PoolTree` previews will both change appearance.

**Files:**
- Modify: none by hand. `ui/.ds-css/compiled.css` and `ui/types/` are regenerated.

- [ ] **Step 1: Run the full frontend build**

Run: `nix develop --command bash -c 'cd ui && npm run build'`
Expected: exit 0.

- [ ] **Step 2: Re-sync**

Follow `.design-sync/NOTES.md`. In short, from the repo root:

```bash
ln -sfn .. ui/node_modules/cloudpam-ui   # only if npm ci has run since the last sync
nix develop --command bash -c 'cd ui && npx tsc -p tsconfig.dts.json'
nix develop --command node .ds-sync/resync.mjs --config .design-sync/config.json \
  --node-modules ui/node_modules --out ./ds-bundle \
  --remote .design-sync/.cache/remote-sync.json
```

Expected: the verdict JSON lists `TreeNode` and `PoolTree` under `verification.changed` (their render hashes moved), with `upload.any: true`.

- [ ] **Step 3: Re-grade the changed previews**

Read `ds-bundle/_screenshots/review/schema-wizard__TreeNode.png` and `.../data-display__PoolTree.png`. Confirm subnet dots are orange and subnet rows in the wizard are dimmed. Write verdicts to `.design-sync/.cache/review/<Name>.grade.json` per the design-sync skill.

- [ ] **Step 4: Upload**

Per the design-sync skill's §5 atomic path — the project is pinned and non-empty, so it updates in one pass.

- [ ] **Step 5: Commit any sync-input changes**

```bash
git add .design-sync
git commit -m "chore(design-sync): re-sync after pool-type color unification"
```

---

## Self-Review

**1. Spec coverage.** This plan covers §6 (Tasks 1–4), §3 (Task 5), and the `dark:hover:bg-gray-750` defect noted in §2 (Task 6). Deliberately **out of scope**, each deferred to its own plan: §1/§1b/§1c/§1d (Settings nav, service accounts, federated deactivation, denial codes), §2/§2b/§2c/§2d (retiring `UsersAdminPanel`, role lifecycle, first-run, responsive), §4 (six primitives), §5 (toast adoption).

**2. Placeholder scan.** No TBDs. Every code step carries the literal code, and there are no remaining conditionals — Decision 1 is resolved to five pool types.

**3. Type consistency.** `PoolTypeMeta` / `POOL_TYPES` / `UNKNOWN_POOL_TYPE_DOT` / `poolTypeDot` are defined in Task 1 and used with those exact names in Tasks 2 and 4. `getPoolTypeColor(type: string): string` keeps its existing signature throughout. `SchemaNode` is imported from `../wizard/utils/cidr`, matching the existing source.

**Correction to the spec:** §3 gives the path `ui/src/utils/badges.ts`. That file does not exist; `getStatusBadgeClass` is in `ui/src/utils/format.ts`. Task 5 uses the real path.

---

## Remaining Plans (not yet written)

| Plan | Covers | Blocked by |
|---|---|---|
| **B — Config primitives** | §4: `Modal`, `ConfirmDialog`, `Field`, `Toggle`, `DataTable`, `EmptyState`; §5 toast rules | Nothing. Should be written next — C depends on it. |
| **C — Settings shell** | §1 nav collapse + redirects, §2 retire `UsersAdminPanel`, §2b role lifecycle UI, §2c first-run, §2d responsive | Plan B; and §2b's UI needs Plan D's 409 response. |
| **D — Identity backend** | §1b service accounts (new table, endpoints, migration `0022`, three storage backends), §1c local-only deactivation, §1d denial codes + audit, §2b reference-checked role deletion, `idp:manage` permission | All five open questions in `docs/design/identity-decisions.md`. |

Plan D is the largest and the only one touching Go, SQL, and `internal/auth`. Migrations are currently at `0021_network_objects_relationships.sql`, so it starts at `0022`. Note `CLAUDE.md` still says migrations end at `0017` — it is out of date and should be corrected as part of Plan D.
