# design-sync notes — CloudPAM

Repo-specific gotchas for syncing `ui/` to claude.ai/design. Read this before
re-running the converter.

## The big one: this is an app, not a component library

`ui/` is `private: true` with no library build and no exports entry. Everything
below follows from that.

- **`ui/node_modules/cloudpam-ui` is a self-link** (`ln -sfn .. ui/node_modules/cloudpam-ui`).
  The converter resolves `<node_modules>/<pkg>` for both the `.d.ts` tree and
  preview `import … from 'cloudpam-ui'`. `npm ci` wipes it — **recreate it on
  every fresh install** or the build dies with
  `ENOENT … node_modules/cloudpam-ui/package.json`.
- Because `PKG_DIR` is that symlink, `../` in a config path escapes
  `ui/node_modules/`, not `ui/`. Hence `extraEntries` and `docsDir` use
  `../../../` (three levels up from `ui/node_modules/cloudpam-ui`). Paths that
  point *into* the package (`cssEntry`, `tsconfig`) resolve normally through the
  link.
- There is no `dist/`, so the converter runs in **synth-entry mode**.

## Forked `lib/source-kit.mjs` (declared in `cfg.libOverrides`)

Two repo-specific changes; diff against the bundled `lib/source-kit.mjs` on
re-sync and merge upstream changes.

1. **Default exports.** Every component is `export default function <Name>`.
   The stock synth entry emits only `export * from …`, which does **not**
   re-export defaults — discovery finds the components but
   `window.CloudPAM.<Name>` is `undefined` and every preview fails
   "Element type is invalid". The fork adds `export { default as <Name> }`.
2. **Scoping `srcFiles` to `src/components` + `src/wizard`.** Unscoped, the
   synth entry pulls in `main.tsx` → `index.css` → `@import "tailwindcss"`,
   which esbuild cannot resolve (no `style` condition) and the whole build
   fails. It also over-discovers ~46 "components" (every `*Page`) against the 19
   in scope. Also added `wizard` and `steps` to `GENERIC_DIR` so the doc
   frontmatter `category` can merge the six wizard files into one group — a
   non-generic directory name outranks `category` in `package-build.mjs`.

## Declarations are generated, not shipped

`ui/tsconfig.dts.json` emits `.d.ts` into `ui/types/` (gitignored), which
`findTypesRoot` auto-detects — **no `package.json` change needed**. Without it
every emitted `<Name>Props` is an empty `[key: string]: unknown`, i.e. the design
agent gets no API contract at all. `cfg.buildCmd` runs it.

Five wizard components name their props interface `Props`, not `<Name>Props`, so
the extractor can't match them — they're hand-written in `cfg.dtsPropsFor`
(TemplateStep, DimensionsStep, StrategyStep, PreviewStep, TreeNode).

## CSS is the app's compiled Tailwind output

`cfg.cssEntry` points at `ui/.ds-css/compiled.css`, copied by `buildCmd` from
the hash-named `web/dist/assets/index-*.css`. Consequences:

- **Tailwind v4 only compiles classes it finds under `ui/src`.** Authored
  previews live outside it, so any class they use that the app doesn't already
  use *does not exist*. This silently cost real debugging: `h-40`/`h-56`/`h-96`/
  `max-w-xl` all resolved to nothing, so wrappers had zero height and
  `fixed`-positioned children (ToastContainer, the modals) rendered clipped
  slivers. **Preview-only layout must use inline `style={{}}`.** The same
  constraint is documented for the design agent in `conventions.md`.
- No webfonts ship — type is the system stack, which is why there's no
  `[FONT_MISSING]`.
- **`resync.mjs` does not run `buildCmd`, and reports `build: ok` anyway.** Its
  build stage is `package-build` (the converter's own bundling). If
  `ui/.ds-css/compiled.css` is missing, `package-build` falls back to a 73-byte
  `/* @ds-css-runtime: no extracted CSS */` stub and the verdict still comes
  back `ok: true` with every stage green — so an unstyled design system uploads
  cleanly with nothing to warn you. **Run `buildCmd` yourself first** (the plan's
  Task 7 Step 1 exists for this), then sanity-check
  `ls -l ds-bundle/_ds_bundle.css`: real output is ~90 KB, and it should contain
  a class you know you just changed.

## Preview provider

`.design-sync/preview-provider.tsx` (via `cfg.extraEntries`) supplies
`MemoryRouter` + a signed-in admin. The three app contexts all have *defaults*,
but those defaults are a signed-out user (`hasPermission: () => false`), which
makes `Sidebar`'s nav filter to empty and `Header` render signed-out.

It also re-exports `Routes`/`Route`/`Navigate`/`Outlet`. **A preview that imports
`react-router-dom` directly gets a second module instance whose `<Routes>` can't
see the provider's `MemoryRouter`** — `Layout` and `ProtectedRoute` rendered
blank until this was fixed.

## Per-component gotchas

- **Backend-driven components need a stubbed `fetch`** or they render 404 error
  banners: `UsersAdminPanel` (`/api/v1/auth/users`, `/auth/roles`,
  `/auth/permissions`) and `UpdateBanner` (`/api/v1/updates`). The stubs live at
  module scope in each preview `.tsx` — preview code compiles into the card
  only, never into `_ds_bundle.js`.
- `UpdateBanner` also returns `null` unless `role === 'admin'`.
- `ProtectedRoute` has **two** failure branches: a missing permission redirects
  to `/`, only an unauthenticated session goes to `/login`. Pointing both at
  `/login` produces a redirect loop and a blank card.
- `TemplateStep`'s selected-summary panel reads `rootCidr`, `hierarchy` and
  `recommended` — an id-only `Blueprint` stub renders blank.
- `TreeNode`'s `defaultExpanded` falls back to `depth < 2` only when
  *undefined*; pass an empty `Set` to actually collapse everything.
- `PoolDetailPanel` without `onEdit` swaps "Edit Pool" for a "Manage Pool" link
  (it does not hide the action).
- `Sidebar` needs `cfg.overrides.Sidebar.viewport = 900x1080`; the default
  900x700 capture crops its ~974px nav.

## Known render warns

All six `[GRID_OVERFLOW]` warns were resolved via `cfg.overrides` (`cardMode`
`single` for the fixed/portal overlays ToastContainer, DiscoveryWizard,
ImportExportModal, SearchModal; `column` for the wide SchemaPlanner and
TreeNode). A re-sync should see **zero** warns — anything new is genuinely new.

## Conventions-header validation

Re-validating `conventions.md` against a fresh build flags **`MemoryRouter`** as
"not a component and not a bundle export". That is expected and correct as
written: it ships inside the bundle (used internally by `DSPreviewProvider`) but
is deliberately *not* on `window.CloudPAM`, and the header only mentions it
descriptively. Don't "fix" it by exporting it. A `---` hit for a token is just a
markdown table separator.

**Known false positives in any naive validator** — all three verify fine when
checked properly, so don't "fix" the conventions file for them:

| Flag | Why it's spurious |
|---|---|
| `MemoryRouter` | in the bundle, intentionally not an export (above) |
| `---` | a markdown table separator, not a `--token` |
| `gap-1.5` | Tailwind escapes the dot, so it ships as `.gap-1\.5`; a `includes("." + cls)` check misses it. Any arbitrary-value or decimal class (`h-[28rem]`, `gap-1.5`) has the same problem — match against the escaped form. |

## Deliberately not covered

- `SearchModal` results require typing (300ms debounce) — not statically
  renderable, so the card shows the opened palette and its empty state.
- `Header` has a single cell: `sidebarOpen` only toggles a viewport-gated menu
  button, so a second card would be pixel-identical.
- Only `ui/src/components` and `ui/src/wizard` are synced. `ui/src/pages` is
  deliberately out of scope (user's choice, 2026-08-02).

## Re-sync risks

- **The self-link and `ui/types/` are both gitignored/ephemeral.** A fresh clone
  needs `npm ci`, then the `ln -sfn`, then `cfg.buildCmd`. Forgetting either is
  the most likely first failure.
- **`.design-sync/node_modules` is also a gitignored symlink** (`-> ../.ds-sync/node_modules`,
  per `.gitignore:77`) and just as easy to forget on a fresh clone. Without it the
  converter can't resolve its own dependencies and fails with a misleading `ts-morph`
  resolution error that looks like a config problem, not a missing link. Recreate it
  the same way as the `cloudpam-ui` self-link, before running `cfg.buildCmd`.
- **`cssEntry` is a copy, not a live file.** If someone runs the converter
  without `buildCmd`, `ui/.ds-css/compiled.css` is stale (or absent) and the
  cards render against old CSS. When in doubt, re-run `buildCmd`.
- **`dtsPropsFor` is hand-copied** from five `Props` interfaces. If those props
  change, the shipped `.d.ts` silently lies. Re-check them on any wizard change.
- **The fetch stubs inline API response shapes** (`UserInfo`, `RoleInfo`,
  `UpdateCheckResponse`). If those types change, the stubbed cards keep
  rendering the old shape and will drift from reality without failing.
- **The class vocabulary table in `conventions.md` was generated from this
  build's `_ds_bundle.css`.** It shifts whenever CloudPAM's own class usage
  changes; re-validate the names on each sync (the conventions step does this).
- The fork of `source-kit.mjs` pins behavior against a specific converter
  version — diff it against the bundled lib on every sync.
- Grading used the system Chrome-independent playwright chromium installed at
  `~/Library/Caches/ms-playwright` (2026-08-02).
