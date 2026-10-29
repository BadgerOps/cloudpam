# Search

Search lets a user find pools and accounts by text, or find which pools contain an IP address or CIDR, from the header search box or the search API.

## Sub-features

- `search-open` opens the search panel from the header box or `Ctrl+K` / `Cmd+K`.
- `search-text` matches pool and account names.
- `search-cidr` matches pools that contain an IP address or prefix.
- `search-empty` shows a no-results state for a miss.
- `search-api` returns the same matches from the terminal.

## How to get to it (user POV)

- Click the header box labeled `Search pools, CIDRs, accounts` (placeholder `Search pools, CIDRs, accounts...`, hint `⌘K`) on any page, or press `Ctrl+K` / `Cmd+K` outside a text field. A search panel opens with an input whose placeholder is `Search pools, accounts, or enter an IP address...`.
- `GET /api/v1/search?q=<text>` or `GET /api/v1/search?cidr_contains=<ip>` from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present: `Corp Supernet` (`10.0.0.0/8`), `Prod East VPC` (`10.20.0.0/16`), account `prod-platform`.

- **Open.** After `login()`, click `getByRole('button', { name: 'Search pools, CIDRs, accounts' })`. The input with placeholder `Search pools, accounts, or enter an IP address...` appears inside `getByTestId('search-panel')`, gets focus on the next frame, and the panel shows `Search by name, CIDR, or IP address...`.
- **Text match.** Fill that input with `Prod East`. A result button for `Prod East VPC` appears; no `Corp Supernet` button does.
- **CIDR match.** Replace the query with `10.20.1.5`. Wait for the `Corp Supernet` button (new to this query); `Prod East VPC` is listed too, the section header reads `Pools (2)`, and no account is listed.
- **Close.** Press `Escape`; the panel goes. `Ctrl+K` opens it again with an empty input. A click on the backdrop also closes it.
- **Empty.** Replace the query with `volcano`. Expected: `No results for “volcano”` (curly quotes) and no result buttons. At e0818fd this step fails: see Gotchas.
- **API text.** Run `cloudpam-verify api --save search-text GET '/api/v1/search?q=prod'`. Status `200`; `items` lists pool `Prod East VPC` and account `prod-platform` (`account_key` `aws:111111111111`), `total` 2; `Corp Supernet` is absent.
- **API containment.** Run `cloudpam-verify api --save search-cidr GET '/api/v1/search?cidr_contains=10.20.1.5'`. Status `200`; `items` lists `Corp Supernet` then `Prod East VPC`, `total` 2, no accounts.
- **Proof.** `shot('02-search-results')` and `aria('02-search-results')` with the populated panel, plus the saved API responses. The proven journey is `journeys/search.cjs`; it runs the empty step last so the steps before it still prove out.

## Gotchas

- Known product bug at e0818fd: a query with no matches crashes the SPA to a blank page. The API answers `{"items":null,"total":0,...}` and the panel calls `.filter` on `null` (`pageerror Cannot read properties of null (reading 'filter')`). `search-empty` fails until that is fixed; do not weaken the assertion.
- The header box is a read-only input with `role="button"` that opens the panel; fill the panel's input, not the header box.
- The panel has no `role="dialog"`; `getByRole('dialog')` finds nothing. Use `getByTestId('search-panel')`.
- Results are debounced 300 ms and the previous results stay on screen while the next query loads. Wait for a row that only the new query can produce, not a fixed sleep.
- Result rows are buttons whose names run CIDR, name, type and status together; match them with a regex such as `/Prod East VPC/`. The `Pools (2)` header is two nodes, so its text content is `Pools(2)`; match `/^Pools\s*\(2\)$/`.
- Text that looks like IPv4 becomes a containment query. A partial address such as `10.20` gets `400` `invalid cidr_contains: invalid CIDR or IP: "10.20"`, shown in red in the panel. An IPv6 address is a text query in the UI, and `cidr_contains=2001:db8::1` on the API returns `400` `invalid cidr_contains: only IPv4 supported: 2001:db8::1`.
- The panel closes on `Escape`, on a backdrop click, and on choosing a result; reopen before proving another query.
