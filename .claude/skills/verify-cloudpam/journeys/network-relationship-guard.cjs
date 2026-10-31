const assert = require('node:assert/strict');

// #354 follow-ups: deleting a network object from Discovery > Merged Network > Objects
// removes the relationships that reference it, and the list refresh after the delete
// runs with the filters current at completion time, not the ones at click time.
//
// The stale-filter path is timed deterministically: a page.route holds the UI's DELETE
// response until the object-type filter has been changed to "Subnet" and the filter's own
// refresh has gone out, then releases it. The refresh that onDeleted triggers must then
// carry object_type=subnet. The runner's api() helper uses the request context, which the
// page route never sees.
module.exports = async ({ page, login, api, shot, aria, note }) => {
  const stamp = Date.now();
  const vpcName = `verify-guard-vpc-${stamp}`;
  const subnetName = `verify-guard-subnet-${stamp}`;

  // Console errors are only counted once login has completed.
  let afterLogin = false;
  const consoleErrors = [];
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return;
    const text = `${msg.text()} @ ${msg.location().url || 'n/a'}`;
    note(`console.error ${afterLogin ? '(after login)' : '(before login)'} ${text}`);
    if (afterLogin) consoleErrors.push(text);
  });

  await login();
  afterLogin = true;

  const accounts = await api('GET', '/api/v1/accounts');
  assert.equal(accounts.status, 200);
  const account = accounts.body.find((a) => a.key === 'aws:111111111111') || accounts.body[0];
  assert.ok(account, 'seeded account present');
  const third = 50 + Math.floor(Math.random() * 150);
  const vpc = await api('POST', '/api/v1/network/objects', {
    object_type: 'vpc', account_id: account.id, name: vpcName, cidr: `10.${third}.0.0/16`, provider: 'aws', region: 'us-east-1',
  });
  assert.equal(vpc.status, 201, `create vpc: ${JSON.stringify(vpc.body)}`);
  // Deliberately not parented to the vpc: the children rule (409) is not what this journey proves.
  const subnet = await api('POST', '/api/v1/network/objects', {
    object_type: 'subnet', account_id: account.id, name: subnetName, cidr: `10.${third}.1.0/24`, provider: 'aws', region: 'us-east-1',
  });
  assert.equal(subnet.status, 201, `create subnet: ${JSON.stringify(subnet.body)}`);
  const rel = await api('POST', '/api/v1/network/relationships', {
    type: 'contains', source_kind: 'network_object', source_id: String(vpc.body.id), target_kind: 'network_object', target_id: String(subnet.body.id),
  });
  assert.equal(rel.status, 201, `create relationship: ${JSON.stringify(rel.body)}`);
  const relId = rel.body.id;
  note(`entry point: Discovery > Merged Network > Objects; vpc ${vpc.body.id} ${vpcName} -> subnet ${subnet.body.id} ${subnetName} via relationship ${relId}`);

  // Record every objects-list GET the SPA issues, with a timestamp, so the post-delete
  // refresh can be told apart from the filter change's own refresh.
  const objectListGets = [];
  page.on('request', (req) => {
    const url = new URL(req.url());
    if (req.method() === 'GET' && url.pathname === '/api/v1/network/objects') {
      objectListGets.push({ at: Date.now(), query: url.search });
      note(`objects GET ${url.search}`);
    }
  });

  await page.getByRole('link', { name: 'Discovery' }).click();
  await page.getByRole('heading', { name: 'Discovery' }).first().waitFor();
  await page.getByRole('button', { name: 'Merged Network' }).click();
  await page.getByRole('button', { name: 'Objects', exact: true }).click();
  const delVpc = page.getByRole('button', { name: `Delete network object ${vpcName}` });
  const delSubnet = page.getByRole('button', { name: `Delete network object ${subnetName}` });
  await delVpc.waitFor({ timeout: 10000 });
  await delSubnet.waitFor({ timeout: 10000 });
  await shot('objects-before');
  await aria('objects-before');

  // Hold the UI's DELETE for the vpc until told to release it.
  let release;
  const released = new Promise((resolve) => { release = resolve; });
  let intercepted;
  const interceptedPromise = new Promise((resolve) => { intercepted = resolve; });
  const deletePath = `/api/v1/network/objects/${vpc.body.id}`;
  await page.route((url) => url.pathname === deletePath, async (route) => {
    if (route.request().method() !== 'DELETE') return route.continue();
    note(`holding DELETE ${deletePath}`);
    intercepted();
    await released;
    note(`releasing DELETE ${deletePath}`);
    return route.continue();
  });

  await delVpc.click(); // window.confirm is auto-accepted by the runner
  await interceptedPromise;
  assert.ok(await delVpc.isDisabled(), 'delete button disabled while the delete is in flight');

  // While the delete is in flight, narrow the object type filter to Subnet.
  const typeFilter = page.locator('select:has(option[value="network_interface"])');
  await typeFilter.selectOption('subnet');
  await page.waitForRequest((req) => req.method() === 'GET' && req.url().includes('/api/v1/network/objects?') && req.url().includes('object_type=subnet'), { timeout: 10000 });
  const filterChangedAt = Date.now();
  note(`filter changed to object_type=subnet at ${new Date(filterChangedAt).toISOString()}`);
  await page.waitForTimeout(250); // let the filter's own refresh settle before releasing

  const releasedAt = Date.now();
  release();
  await page.getByText(`Deleted network object ${vpcName}`).waitFor({ timeout: 10000 });
  note('success toast shown: Deleted network object ' + vpcName);
  // The refresh the delete triggers: an objects GET issued after the DELETE was released.
  // Poll the recorded requests (the GET may already have fired by the time the toast shows).
  const deadline = Date.now() + 10000;
  while (!objectListGets.some((g) => g.at >= releasedAt) && Date.now() < deadline) await page.waitForTimeout(100);
  await page.waitForTimeout(500); // catch any late, stale-filtered second refresh
  const postDeleteGets = objectListGets.filter((g) => g.at >= releasedAt);
  note(`objects GETs after release: ${JSON.stringify(postDeleteGets.map((g) => g.query))}`);
  assert.ok(postDeleteGets.length >= 1, 'the delete triggers an objects refresh');
  for (const g of postDeleteGets) {
    assert.ok(g.query.includes('object_type=subnet'), `post-delete refresh carries the current filter, got ${g.query}`);
  }
  await page.unroute((url) => url.pathname === deletePath);

  await delVpc.waitFor({ state: 'detached', timeout: 10000 });
  await delSubnet.waitFor({ timeout: 10000 });
  await shot('objects-after');
  await aria('objects-after');
  assert.equal(await delVpc.count(), 0, 'deleted vpc row is gone');
  assert.equal(await delSubnet.count(), 1, 'subnet row still listed under the Subnet filter');

  // Side effects through the API.
  const getVpc = await api('GET', `/api/v1/network/objects/${vpc.body.id}`);
  assert.equal(getVpc.status, 404, 'deleted vpc is gone');
  const rels = await api('GET', `/api/v1/network/relationships?source_kind=network_object&source_id=${vpc.body.id}`);
  assert.equal(rels.status, 200);
  assert.equal((rels.body.items || []).length, 0, 'no relationships reference the deleted vpc');
  const allRels = await api('GET', '/api/v1/network/relationships');
  assert.ok(!(allRels.body.items || []).some((r) => r.id === relId), `relationship ${relId} removed with the object`);
  const audit = await api('GET', '/api/v1/audit?action=delete&resource_type=network_object&limit=5');
  assert.equal(audit.status, 200);
  const newest = (audit.body.events || [])[0];
  assert.ok(newest, 'audit delete event present');
  assert.equal(newest.resource_id, String(vpc.body.id), 'newest audit delete names the vpc');
  assert.equal(newest.resource_name, vpcName);
  assert.equal(newest.changes && newest.changes.after && newest.changes.after.relationships_removed, 1, `audit relationships_removed: ${JSON.stringify(newest.changes)}`);
  note(`audit event ${newest.id}: relationships_removed=${newest.changes.after.relationships_removed}`);

  assert.deepEqual(consoleErrors, [], `console errors after login: ${JSON.stringify(consoleErrors)}`);
  note('no console errors after login');
};
