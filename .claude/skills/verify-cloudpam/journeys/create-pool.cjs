const assert = require('node:assert/strict');

module.exports = async ({ page, login, api, shot, aria, note }) => {
  const name = `verify-${Date.now()}`;
  const parentLabel = 'Corp Supernet (10.0.0.0/8)';
  const pageSearch = () => page.getByPlaceholder('Search pools by name, CIDR, or description...');

  await login();
  // Reruns against one instance keep earlier pools, so pick a free /16 (the seed holds 10.20.0.0/16).
  const existing = (await api('GET', '/api/v1/pools')).body.map((p) => p.cidr);
  let third = 30 + Math.floor(Math.random() * 200);
  while (existing.includes(`10.${third}.0.0/16`)) third = 30 + ((third - 29) % 200);
  const cidr = `10.${third}.0.0/16`;
  const overlapCidr = `10.${third}.128.0/17`;
  note(`using ${name} ${cidr}`);
  await page.getByRole('link', { name: 'Address Pools' }).click();
  await page.getByRole('heading', { name: 'Address Pools' }).waitFor();
  // The table lists root pools only until a search is typed; the seeded root proves data loaded.
  await page.getByRole('cell', { name: 'Corp Supernet' }).waitFor();
  await shot('01-pools-list');

  await page.getByRole('button', { name: 'New Pool' }).click();
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page.getByLabel('CIDR', { exact: true }).fill(cidr);
  await page.getByLabel('Parent Pool', { exact: true }).selectOption({ label: parentLabel });
  await shot('02-create-form-filled');
  await page.getByRole('button', { name: 'Create Pool' }).click();
  await page.getByText('Pool created').waitFor({ timeout: 10000 });

  await pageSearch().fill(name);
  await page.getByRole('cell', { name, exact: true }).waitFor();
  await page.getByRole('cell', { name: cidr, exact: true }).waitFor();
  await shot('03-pool-row-visible');
  await aria('03-pool-row-visible');

  const list = await api('GET', '/api/v1/pools');
  assert.equal(list.status, 200);
  const created = list.body.find((p) => p.name === name);
  assert.ok(created, 'created pool present in GET /api/v1/pools');
  assert.equal(created.cidr, cidr);
  const parent = list.body.find((p) => p.name === 'Corp Supernet');
  assert.equal(created.parent_id, parent.id, 'parent_id points at Corp Supernet');
  note(`pool id ${created.id} parent_id ${created.parent_id}`);

  const audit = await api('GET', '/api/v1/audit?action=create&resource_type=pool&limit=50');
  assert.equal(audit.status, 200);
  const events = Array.isArray(audit.body) ? audit.body : audit.body.items || audit.body.events || [];
  assert.ok(events.some((e) => JSON.stringify(e).includes(name)), 'audit event for the new pool exists');

  const expectedChildren = list.body.filter((p) => p.parent_id === parent.id).length;
  const stats = await api('GET', `/api/v1/pools/${parent.id}/stats`);
  assert.equal(stats.status, 200);
  const direct = stats.body.direct_children ?? stats.body.stats?.direct_children;
  assert.equal(direct, expectedChildren, `parent stats direct_children (${direct}) matches child count (${expectedChildren})`);
  note(`parent ${parent.id} stats direct_children=${direct}`);

  // Only the heading is checked: GET /api/v1/pools/{id} returns no stats, so the panel renders none.
  await pageSearch().fill('');
  const parentRow = page.getByRole('row', { name: /Corp Supernet/ });
  await parentRow.getByTitle('View details').click();
  await page.getByRole('heading', { name: 'Corp Supernet' }).waitFor({ timeout: 10000 });
  await shot('04-parent-detail-panel');

  await page.getByRole('button', { name: 'New Pool' }).click();
  await page.getByLabel('Name', { exact: true }).fill(`${name}-overlap`);
  await page.getByLabel('CIDR', { exact: true }).fill(overlapCidr);
  await page.getByLabel('Parent Pool', { exact: true }).selectOption({ label: parentLabel });
  await page.getByRole('button', { name: 'Create Pool' }).click();
  await page.getByText(/overlap/i).first().waitFor({ timeout: 10000 });
  await shot('05-overlap-refused');
  const after = await api('GET', '/api/v1/pools');
  assert.ok(!after.body.some((p) => p.name === `${name}-overlap`), 'overlapping pool was not stored');
};
