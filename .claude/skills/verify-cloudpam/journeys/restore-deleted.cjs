const assert = require('node:assert/strict');

module.exports = async ({ page, login, api, shot, aria, note }) => {
  const stamp = Date.now();
  const parentName = `verify-restore-parent-${stamp}`;
  const childName = `verify-restore-child-${stamp}`;

  await login();
  const existing = (await api('GET', '/api/v1/pools')).body.map((p) => p.cidr);
  let second = 100 + Math.floor(Math.random() * 100);
  while (existing.includes(`${second}.0.0.0/16`)) second = 100 + ((second - 99) % 100);
  const parentCidr = `${second}.0.0.0/16`;
  const childCidr = `${second}.0.1.0/24`;

  const parent = await api('POST', '/api/v1/pools', { name: parentName, cidr: parentCidr });
  assert.equal(parent.status, 201, 'parent pool created');
  const child = await api('POST', '/api/v1/pools', { name: childName, cidr: childCidr, parent_id: parent.body.id });
  assert.equal(child.status, 201, 'child pool created');
  const del = await api('DELETE', `/api/v1/pools/${parent.body.id}?force=true`);
  assert.equal(del.status, 204, 'parent cascade-deleted');
  note(`deleted ${parentName} ${parentCidr} (id ${parent.body.id}) with child ${childName} (id ${child.body.id})`);

  await page.getByRole('link', { name: 'Recently Deleted' }).click();
  await page.getByRole('heading', { name: 'Recently Deleted' }).waitFor();
  const childButton = page.getByRole('button', { name: `Restore pool ${childName}` });
  await childButton.waitFor();
  assert.equal(await childButton.isDisabled(), true, 'child restore is blocked while its parent is deleted');
  await page.getByText(`inside deleted pool ${parentName}`).waitFor();
  await shot('01-recently-deleted');
  await aria('01-recently-deleted');

  await page.getByRole('button', { name: `Restore pool ${parentName}` }).click();
  await page.getByText(`Restored ${parentName}`).waitFor({ timeout: 10000 });
  assert.equal(await childButton.isDisabled(), false, 'child restore is enabled once the parent is back');
  await shot('02-parent-restored');

  await childButton.click();
  await page.getByText(`Restored ${childName}`).waitFor({ timeout: 10000 });
  await shot('03-child-restored');
  await aria('03-child-restored');

  const live = (await api('GET', '/api/v1/pools')).body;
  const restoredChild = live.find((p) => p.id === child.body.id);
  assert.ok(live.some((p) => p.id === parent.body.id), 'parent is live in GET /api/v1/pools');
  assert.ok(restoredChild, 'child is live in GET /api/v1/pools');
  assert.equal(restoredChild.parent_id, parent.body.id, 'child keeps its parent');
  const deleted = (await api('GET', '/api/v1/pools/deleted')).body;
  assert.ok(!deleted.some((p) => p.id === parent.body.id || p.id === child.body.id), 'neither pool is still listed as deleted');

  const again = await api('POST', `/api/v1/pools/${child.body.id}/restore`);
  assert.equal(again.status, 404, 'restoring a live pool is 404');

  const audit = await api('GET', '/api/v1/audit?action=restore&resource_type=pool&limit=50');
  assert.equal(audit.status, 200);
  const events = Array.isArray(audit.body) ? audit.body : audit.body.items || audit.body.events || [];
  for (const name of [parentName, childName]) {
    assert.ok(events.some((e) => JSON.stringify(e).includes(name)), `restore audit event for ${name}`);
  }
  note('both pools restored through the UI, re-read through the API, and audited');
};
