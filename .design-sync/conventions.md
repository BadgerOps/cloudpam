# Building with CloudPAM's UI

CloudPAM is an IP Address Management platform. These components come from its
real application frontend (`ui/`), not a standalone component library — so they
assume an app context (router + auth) and they style themselves with Tailwind.

## Wrap everything in DSPreviewProvider

Nearly every component here reads React Router and/or CloudPAM's auth context.
Rendered bare, `Sidebar` shows an empty nav, `Header` renders signed-out, and
`Layout` / `ProtectedRoute` render **nothing at all** (they redirect or hit a
router-less crash).

`DSPreviewProvider` supplies a `MemoryRouter` plus a signed-in admin, and is the
correct root for anything you build:

```jsx
const { DSPreviewProvider, Layout, Routes, Route } = window.CloudPAM

<DSPreviewProvider>
  <Routes>
    <Route element={<Layout />}>
      <Route path="*" element={<YourPage />} />
    </Route>
  </Routes>
</DSPreviewProvider>
```

**Import `Routes` / `Route` / `Navigate` / `Outlet` from the design system, not
from `react-router-dom`.** A separate copy of react-router is a separate context
— its `<Routes>` cannot see `DSPreviewProvider`'s router, and route-composed UI
silently renders blank.

To vary the signed-in principal, override the context directly. Permission-gated
nav and guards respond to `hasPermission`:

```jsx
const { AuthContext, previewAuth } = window.CloudPAM
const viewer = { ...previewAuth, role: 'viewer', hasPermission: (p) => p === 'pools:read' }

<AuthContext.Provider value={viewer}>…</AuthContext.Provider>
```

`ThemeContext` (light/dark) and `ToastContext` (`{ toasts, showToast }`) work the
same way; `ToastContainer` renders nothing until `toasts` is non-empty.

## Styling: Tailwind, but a CLOSED set of classes

The shipped stylesheet is CloudPAM's **compiled** Tailwind v4 output. It contains
only the ~745 utilities the app itself uses. **A Tailwind class that CloudPAM
doesn't already use does not exist here and will silently do nothing** — this is
the single most common way to produce broken-looking output.

Two safe options:
1. Reuse the vocabulary below (all verified present in the bundle).
2. For anything else — especially explicit sizing — use **inline `style={{}}`**.

Verified families:

| Purpose | Available |
|---|---|
| Brand / primary | `bg-blue-500` `bg-blue-600` `bg-blue-100` `text-blue-600` `text-blue-700` `border-blue-500` |
| Neutrals | `bg-gray-100`–`bg-gray-400`, `bg-gray-600`, `bg-gray-900`; `text-gray-400`–`text-gray-900`; `border-gray-100`–`border-gray-300` |
| Status color | `green` (active/ok), `red` (error/deprecated), `amber` (warning), all at `-100` bg / `-700` text |
| Type scale | `text-xs` `text-sm` `text-base` `text-lg` `text-xl` `text-2xl` |
| Weight | `font-normal` `font-medium` `font-semibold` `font-bold`; `font-mono` for CIDRs |
| Padding | `p-0`–`p-6`, `p-8` (plus `px-*` / `py-*`) |
| Gap | `gap-1` `gap-1.5` `gap-2` `gap-3` `gap-4` `gap-6` |
| Radius | `rounded` `rounded-md` `rounded-lg` `rounded-xl` `rounded-full` |
| Elevation | `shadow-sm` `shadow` `shadow-md` `shadow-lg` `shadow-xl` |
| Width | `max-w-xs` `max-w-sm` `max-w-md` `max-w-2xl` `max-w-4xl` `max-w-5xl` |

Heights are sparse — only `h-2`…`h-12`, `h-16` exist. Use inline styles for
anything taller.

**Dark mode** is class-based, not media-based: it activates under a `.dark`
ancestor. 170 `dark:` variants ship, so pair colors as
`bg-white dark:bg-gray-800`, `text-gray-900 dark:text-gray-100`.

There are no brand webfonts — type is the system stack (`--font-sans`), with
`font-mono` for addresses. CIDRs are always monospace in this product.

## Where the truth lives

- `_ds/<folder>/styles.css` → `@import`s `_ds_bundle.css`, the full compiled
  stylesheet. Read it to confirm a class exists before using it.
- `components/<group>/<Name>/<Name>.d.ts` — the real prop contract.
- `components/<group>/<Name>/<Name>.prompt.md` — per-component usage notes.

## Product idiom

- CIDRs render monospace next to a colored type dot. `POOL_TYPES` in
  `src/utils/poolTypes.ts` is the single source: supernet=`bg-purple-500`,
  region=`bg-blue-500`, environment=`bg-green-500`, vpc=`bg-amber-500`,
  subnet=`bg-orange-500`. Grey (`bg-gray-400`) is reserved for an unrecognised
  type and must never be used for a known one. `getPoolTypeColor` and the Schema
  Wizard's `TreeNode` both read that map, so every surface agrees; `TreeNode`
  dims a subnet row with `opacity-50` rather than changing its hue.
- Utilization is a bar plus a percentage; it turns red as a pool fills.
- `StatusBadge` is the one labeling primitive — `variant` picks the vocabulary
  (`status` / `provider` / `tier` / `action` / `type`), so use it instead of
  hand-rolling pills.

```jsx
const { DSPreviewProvider, PoolDetailPanel, StatusBadge } = window.CloudPAM

<DSPreviewProvider>
  <div className="p-6 space-y-4 max-w-md">
    <div className="flex items-center gap-2">
      <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">production</h2>
      <StatusBadge label="active" />
      <StatusBadge label="aws" variant="provider" />
    </div>
    <PoolDetailPanel pool={pool} onClose={() => {}} onEdit={() => {}} />
  </div>
</DSPreviewProvider>
```
