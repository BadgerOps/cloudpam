const assert = require('node:assert/strict');

// Proves #309: the Blocks and Audit pages page through results and keep their
// filters. Seeds 60 blocks under the seeded Prod East VPC so both lists pass
// one page (blocks page size 50, audit page size 25).
module.exports = async ({ page, login, api, shot, aria, note }) => {
  await login();

  const pools = (await api('GET', '/api/v1/pools')).body;
  const vpc = pools.find((p) => p.name === 'Prod East VPC');
  assert.ok(vpc, 'seeded Prod East VPC present');
  const accounts = (await api('GET', '/api/v1/accounts')).body;
  const account = accounts.find((a) => a.name === 'prod-platform');
  assert.ok(account, 'seeded prod-platform account present');

  const existing = (await api('GET', '/api/v1/blocks?page_size=all')).body.total;
  if (existing < 60) {
    for (let i = existing; i < 60; i++) {
      const res = await api('POST', '/api/v1/pools', {
        name: `verify-block-${String(i).padStart(2, '0')}`,
        cidr: `10.20.${i}.0/24`,
        parent_id: vpc.id,
        account_id: account.id,
      });
      assert.equal(res.status, 201, `create block ${i}: ${JSON.stringify(res.body)}`);
    }
  }
  const total = (await api('GET', '/api/v1/blocks?page_size=all')).body.total;
  note(`blocks total ${total}`);
  assert.ok(total >= 60);

  await page.getByRole('link', { name: 'Allocated Blocks' }).click();
  await page.getByRole('heading', { name: 'Allocated Blocks' }).waitFor();
  await page.getByText(`Showing 1-50 of ${total} blocks`).waitFor();
  await shot('01-blocks-page-1');

  await page.getByRole('button', { name: 'Next' }).click();
  await page.getByText(`Showing 51-${Math.min(100, total)} of ${total} blocks`).waitFor();
  await page.getByRole('cell', { name: 'verify-block-50', exact: true }).waitFor();
  await shot('02-blocks-page-2');
  await aria('02-blocks-page-2');

  const inAccount = (await api('GET', `/api/v1/blocks?accounts=${account.id}&page_size=all`)).body.total;
  note(`blocks in ${account.name}: ${inAccount}`);
  assert.ok(inAccount > 50 && inAccount < total, 'the account filter changes the total and still passes one page');
  const accountSelect = page.getByRole('combobox').filter({ hasText: 'All Accounts' });
  await accountSelect.selectOption({ label: account.name });
  await page.getByText(`Showing 1-50 of ${inAccount} blocks`).waitFor();
  await page.getByRole('button', { name: 'Next' }).click();
  await page.getByText(`Showing 51-${Math.min(100, inAccount)} of ${inAccount} blocks`).waitFor();
  const page2Rows = await page.getByRole('row').filter({ hasText: account.name }).count();
  assert.ok(page2Rows >= 10, `page 2 shows ${page2Rows} rows for the filtered account`);
  await shot('03-blocks-page-2-filtered');
  await page.getByRole('button', { name: 'Previous' }).click();
  await page.getByText(`Showing 1-50 of ${inAccount} blocks`).waitFor();

  await page.getByRole('link', { name: 'Audit Log' }).click();
  await page.getByRole('heading', { name: 'Audit Log' }).waitFor();
  await page.getByRole('combobox').filter({ hasText: 'All Actions' }).selectOption('create');
  await page.getByRole('combobox').filter({ hasText: 'All Resources' }).selectOption('pool');
  const audit = (await api('GET', '/api/v1/audit?action=create&resource_type=pool&limit=1')).body;
  assert.ok(audit.total > 25, `filtered audit total ${audit.total} passes one page`);
  await page.getByText(`Showing 1-25 of ${audit.total} events`).waitFor();
  await shot('04-audit-filtered-page-1');
  await page.getByRole('button', { name: 'Next' }).click();
  await page.getByText(`Showing 26-${Math.min(50, audit.total)} of ${audit.total} events`).waitFor();
  const badges = await page.getByText('create', { exact: true }).count();
  assert.ok(badges >= 10, `page 2 keeps the create filter (${badges} create badges)`);
  await shot('05-audit-filtered-page-2');
  await aria('05-audit-filtered-page-2');
  note('proved #309 on Blocks (account filter kept across pages) and Audit (action and resource filters kept)');
};
