# Search

Search lets a user find pools and accounts by text, or find which pool contains an IP address or CIDR, from the header search box or the search API.

## Sub-features

- `search-open` opens the search dialog from the header box.
- `search-text` matches pool and account names.
- `search-cidr` matches pools that contain an IP address or prefix.
- `search-empty` shows a no-results state for a miss.
- `search-api` returns the same matches from the terminal.

## How to get to it (user POV)

- Click the header box labeled `Search pools, CIDRs, accounts` (placeholder `Search pools, CIDRs, accounts...`) on any page; a search dialog opens with placeholder `Search pools, accounts, or enter an IP address...`.
- `GET /api/v1/search?q=<text>` or `GET /api/v1/search?cidr_contains=<ip>` from a terminal.

## Driving it with cloudpam-verify

Preconditions:

- Baseline seed present: `Corp Supernet` (`10.0.0.0/8`), `Prod East VPC` (`10.20.0.0/16`), account `prod-platform`.

- **Open.** After `login()`, click `getByLabel('Search pools, CIDRs, accounts')`. An input with placeholder `Search pools, accounts, or enter an IP address...` appears and has focus.
- **Text match.** Fill that input with `Prod East`. A result for `Prod East VPC` appears; `Corp Supernet` does not.
- **CIDR match.** Replace the query with `10.20.1.5`. Results include `Prod East VPC` and `Corp Supernet` (both contain the address).
- **Empty.** Replace the query with `volcano`. A no-results message appears and no pool names are listed.
- **Close.** Press `Escape`. The dialog closes.
- **API text.** Run `cloudpam-verify api --save search-text GET '/api/v1/search?q=prod'`. Status `200`; body lists `Prod East VPC` and account `prod-platform`.
- **API containment.** Run `cloudpam-verify api --save search-cidr GET '/api/v1/search?cidr_contains=10.20.1.5'`. Status `200`; body lists `Prod East VPC` and `Corp Supernet`.
- **Proof.** `shot('search-results')` and `aria('search-results')` with the populated dialog, plus the saved API responses.

## Gotchas

- The header box is a click target that opens a dialog, not the input you type into; fill the dialog input.
- Results are debounced; wait for a result row or the empty message, not a fixed sleep.
- The dialog closes on `Escape` and on choosing a result; reopen before proving another query.
- Only IPv4 containment queries are supported; an IPv6 address is a text miss.
- The exact empty-state wording and result row structure are not yet confirmed from a run; the first journey should record them here.
