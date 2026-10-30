const assert = require('node:assert/strict');

module.exports = async ({ page, login, shot, aria, note }) => {
  await login();
  await page.getByRole('button', { name: 'Search pools, CIDRs, accounts' }).click();
  const panel = page.getByTestId('search-panel');
  const input = page.getByPlaceholder('Search pools, accounts, or enter an IP address...');
  await input.waitFor();
  // Focus lands on the next animation frame, so poll for it.
  await page.waitForFunction((ph) => document.activeElement?.getAttribute('placeholder') === ph, 'Search pools, accounts, or enter an IP address...', { timeout: 5000 });
  await panel.getByText('Search by name, CIDR, or IP address...').waitFor();

  await input.fill('Prod East');
  await panel.getByRole('button', { name: /Prod East VPC/ }).waitFor();
  assert.equal(await panel.getByRole('button', { name: /Corp Supernet/ }).count(), 0, 'text match excludes Corp Supernet');
  await shot('01-search-text');

  // The previous results stay on screen while the next query loads, so wait on a row new to this query.
  await input.fill('10.20.1.5');
  await panel.getByRole('button', { name: /Corp Supernet/ }).waitFor();
  await panel.getByRole('button', { name: /Prod East VPC/ }).waitFor();
  // The count is a separate span with a CSS margin, so the text content is "Pools(2)".
  await panel.getByText(/^Pools\s*\(2\)$/).waitFor();
  assert.equal(await panel.getByRole('button', { name: /prod-platform/ }).count(), 0, 'containment query lists no accounts');
  await shot('02-search-results');
  await aria('02-search-results');

  await page.keyboard.press('Escape');
  await panel.waitFor({ state: 'detached' });
  await page.keyboard.press('Control+k');
  await input.waitFor();
  await page.waitForFunction((ph) => document.querySelector(`input[placeholder="${ph}"]`)?.value === '', 'Search pools, accounts, or enter an IP address...', { timeout: 5000 });
  note('Escape closes; Ctrl+K reopens with an empty input');

  // Last on purpose: at e0818fd a miss crashes the SPA (features/search.md, Gotchas).
  await input.fill('volcano');
  await panel.getByText('No results for \u201cvolcano\u201d').waitFor({ timeout: 10000 });
  assert.equal(await panel.getByRole('button', { name: /Corp Supernet|Prod East VPC/ }).count(), 0, 'miss lists no pools');
  await shot('03-search-empty');
  await page.keyboard.press('Escape');
  await panel.waitFor({ state: 'detached' });
};
