const assert = require('node:assert/strict');

// Feature #278: delete managed network objects from Discovery > Merged Network > Objects.
// Rule: 409 only while the object has child objects; relationships that reference it are removed with it.
module.exports = async ({ page, login, api, shot, aria, note }) => {
  const stamp = Date.now();
  const parentName = `verify-parent-${stamp}`;
  const childName = `verify-child-${stamp}`;

  await login();
  const accounts = await api('GET', '/api/v1/accounts');
  assert.equal(accounts.status, 200);
  const account = accounts.body.find((a) => a.key === 'aws:111111111111') || accounts.body[0];
  assert.ok(account, 'seeded account present');
  const third = 50 + Math.floor(Math.random() * 150);
  const parent = await api('POST', '/api/v1/network/objects', {
    object_type: 'vpc', account_id: account.id, name: parentName, cidr: `10.${third}.0.0/16`, provider: 'aws', region: 'us-east-1',
  });
  assert.equal(parent.status, 201, `create parent: ${JSON.stringify(parent.body)}`);
  const child = await api('POST', '/api/v1/network/objects', {
    object_type: 'subnet', account_id: account.id, name: childName, cidr: `10.${third}.1.0/24`, provider: 'aws', region: 'us-east-1',
    parent_object_id: parent.body.id,
  });
  assert.equal(child.status, 201, `create child: ${JSON.stringify(child.body)}`);
  const relId = `rel-${stamp}`;
  const rel = await api('POST', '/api/v1/network/relationships', {
    id: relId, type: 'contains', source_kind: 'network_object', source_id: String(child.body.id), target_kind: 'pool', target_id: '1',
  });
  assert.equal(rel.status, 201, `create relationship: ${JSON.stringify(rel.body)}`);
  note(`feature #278 entry point: Discovery > Merged Network > Objects; parent ${parent.body.id} ${parentName}, child ${child.body.id} ${childName} (referenced by relationship ${relId})`);

  await page.getByRole('link', { name: 'Discovery' }).click();
  await page.getByRole('heading', { name: 'Discovery' }).first().waitFor();
  await page.getByRole('button', { name: 'Merged Network' }).click();
  await page.getByRole('button', { name: 'Objects', exact: true }).click();
  const delParent = page.getByRole('button', { name: `Delete network object ${parentName}` });
  const delChild = page.getByRole('button', { name: `Delete network object ${childName}` });
  await delParent.waitFor({ timeout: 10000 });
  await delChild.waitFor({ timeout: 10000 });
  await shot('objects-before');
  await aria('objects-before');

  // 1. Parent with a child object: blocked with the children toast, row stays.
  await delParent.click(); // window.confirm is auto-accepted by the runner
  const blockedToast = page.getByText(/Cannot delete .*child object/);
  await blockedToast.first().waitFor({ timeout: 10000 });
  note(`blocked delete toast: ${await blockedToast.first().innerText()}`);
  await shot('objects-blocked-409');
  assert.equal(await delParent.count(), 1, 'parent row stays after 409');
  const stillThere = await api('GET', `/api/v1/network/objects/${parent.body.id}`);
  assert.equal(stillThere.status, 200, 'parent still exists');

  // 2. Child referenced only by a relationship: deletes, relationship disappears.
  await delChild.click();
  await page.getByText(`Deleted network object ${childName}`).waitFor({ timeout: 10000 });
  await delChild.waitFor({ state: 'detached', timeout: 10000 });
  const rels = await api('GET', '/api/v1/network/relationships');
  assert.equal(rels.status, 200);
  assert.ok(!(rels.body.items || []).some((r) => r.id === relId), 'relationship removed with the object');
  const getChild = await api('GET', `/api/v1/network/objects/${child.body.id}`);
  assert.equal(getChild.status, 404, 'deleted child is gone');
  note(`UI delete of relationship-referenced child OK: toast shown, row removed, GET -> 404, relationship ${relId} gone`);

  // 3. Parent now has no children: deletes.
  await delParent.click();
  await page.getByText(`Deleted network object ${parentName}`).waitFor({ timeout: 10000 });
  await delParent.waitFor({ state: 'detached', timeout: 10000 });
  await shot('objects-after');
  await aria('objects-after');
  const getParent = await api('GET', `/api/v1/network/objects/${parent.body.id}`);
  assert.equal(getParent.status, 404, 'deleted parent is gone');

  const audit = await api('GET', '/api/v1/audit?action=delete&resource_type=network_object&limit=50');
  assert.equal(audit.status, 200);
  const events = audit.body.events || [];
  for (const [n, o] of [[childName, child.body], [parentName, parent.body]]) {
    assert.ok(events.some((e) => e.resource_name === n && e.resource_id === String(o.id)), `audit delete event names ${n}`);
  }
  note('UI delete of parent after child OK; audit has delete events for both objects');
};
