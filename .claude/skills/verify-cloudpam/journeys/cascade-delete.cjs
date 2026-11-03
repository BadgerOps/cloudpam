const assert = require('node:assert/strict');

// Replaces window.confirm so the journey decides each answer and records
// every prompt; the runner's dialog auto-accept never sees these calls.
async function stubConfirm(page, answers) {
  await page.evaluate((answers) => {
    window.__confirms = [];
    window.confirm = (message) => {
      window.__confirms.push(message);
      return answers[window.__confirms.length - 1] ?? false;
    };
  }, answers);
}

const prompts = (page) => page.evaluate(() => window.__confirms);

module.exports = async ({ page, login, api, shot, aria, note }) => {
  await login();
  const seedPools = (await api('GET', '/api/v1/pools')).body;
  const supernet = seedPools.find((p) => p.name === 'Corp Supernet');
  const vpc = seedPools.find((p) => p.name === 'Prod East VPC');
  const account = (await api('GET', '/api/v1/accounts')).body.find((a) => a.key === 'aws:111111111111');
  assert.ok(supernet && vpc && account, 'seed pools and account present');

  const owned = await api('POST', '/api/v1/pools', { name: 'verify-owned', cidr: '10.20.1.0/24', parent_id: vpc.id, account_id: account.id });
  assert.equal(owned.status, 201, 'owned subnet created');
  const nested = await api('POST', '/api/v1/pools', { name: 'verify-nested', cidr: '10.20.1.0/25', parent_id: owned.body.id });
  assert.equal(nested.status, 201, 'nested subnet created');
  const loose = await api('POST', '/api/v1/pools', { name: 'verify-loose', cidr: '10.20.2.0/24', parent_id: vpc.id });
  assert.equal(loose.status, 201, 'unassigned subnet created');
  note(`account ${account.id} owns pool ${owned.body.id} with child ${nested.body.id}; supernet ${supernet.id} has 4 descendants`);

  const plain = await api('DELETE', `/api/v1/accounts/${account.id}`);
  assert.equal(plain.status, 409, 'plain account delete is refused');
  note(`DELETE /api/v1/accounts/${account.id} -> ${plain.status} ${JSON.stringify(plain.body)}`);

  await page.getByRole('link', { name: 'Cloud Accounts' }).click();
  await page.getByRole('heading', { name: 'Cloud Accounts' }).waitFor();
  const accountRow = page.getByRole('row', { name: /prod-platform/ });
  await accountRow.waitFor();

  await stubConfirm(page, [true, false]);
  await accountRow.getByRole('button', { name: 'Delete' }).click();
  await page.waitForFunction(() => window.__confirms.length === 2);
  let seen = await prompts(page);
  assert.deepEqual(seen, [
    'Delete account "prod-platform"?',
    'Account "prod-platform" is used by 2 pools. Delete with 2 pools?',
  ], 'account cascade prompt names the owned pool and its child');
  await page.waitForTimeout(500);
  assert.equal(await page.getByText('Account deleted').count(), 0, 'no success toast after declining');
  await accountRow.waitFor();
  await shot('01-account-cascade-declined');
  assert.ok((await api('GET', '/api/v1/accounts')).body.some((a) => a.id === account.id), 'account kept after declining');
  note(`declined: ${JSON.stringify(seen)}`);

  await stubConfirm(page, [true, true]);
  await accountRow.getByRole('button', { name: 'Delete' }).click();
  await page.getByText('Account deleted').waitFor({ timeout: 10000 });
  await accountRow.waitFor({ state: 'detached' });
  seen = await prompts(page);
  assert.equal(seen[1], 'Account "prod-platform" is used by 2 pools. Delete with 2 pools?');
  await shot('02-account-cascade-deleted');
  await aria('02-account-cascade-deleted');
  const accountsAfter = (await api('GET', '/api/v1/accounts')).body;
  assert.ok(!accountsAfter.some((a) => a.id === account.id), 'account gone after the cascade');
  let poolsAfter = (await api('GET', '/api/v1/pools')).body;
  const ids = new Set(poolsAfter.map((p) => p.id));
  assert.ok(!ids.has(owned.body.id) && !ids.has(nested.body.id), 'owned pool and its child removed by the cascade');
  assert.ok(ids.has(supernet.id) && ids.has(vpc.id) && ids.has(loose.body.id), 'unrelated pools untouched');
  const accountAudit = (await api('GET', '/api/v1/audit?action=delete&resource_type=account&limit=1')).body.events[0];
  assert.equal(accountAudit && accountAudit.resource_id, String(account.id), 'delete/account audit event written');
  note(`accepted: ${JSON.stringify(seen)}; pools left ${JSON.stringify([...ids])}`);

  await page.getByRole('link', { name: 'Address Pools' }).click();
  await page.getByRole('heading', { name: 'Address Pools' }).waitFor();
  const poolRow = page.getByRole('row', { name: /Corp Supernet/ });
  await poolRow.waitFor();

  await stubConfirm(page, [true, false]);
  await poolRow.getByRole('button', { name: 'Delete' }).click();
  await page.waitForFunction(() => window.__confirms.length === 2);
  seen = await prompts(page);
  assert.deepEqual(seen, [
    'Delete pool "Corp Supernet" (10.0.0.0/8)?',
    'Pool "Corp Supernet" has 2 child pools. Delete with 2 children?',
  ], 'pool cascade prompt uses child_count from the stats endpoint');
  const stats = await api('GET', `/api/v1/pools/${supernet.id}/stats`);
  assert.equal(stats.body.child_count, 2, 'stats endpoint agrees with the prompt');
  await page.waitForTimeout(500);
  assert.equal(await page.getByText('Pool deleted').count(), 0, 'no success toast after declining');
  await poolRow.waitFor();
  await shot('03-pool-cascade-declined');
  assert.ok((await api('GET', '/api/v1/pools')).body.some((p) => p.id === supernet.id), 'pool kept after declining');

  await stubConfirm(page, [true, true]);
  await poolRow.getByRole('button', { name: 'Delete' }).click();
  await page.getByText('Pool deleted').waitFor({ timeout: 10000 });
  await poolRow.waitFor({ state: 'detached' });
  await shot('04-pool-cascade-deleted');
  await aria('04-pool-cascade-deleted');
  poolsAfter = (await api('GET', '/api/v1/pools')).body;
  assert.deepEqual(poolsAfter, [], 'supernet and every descendant removed by the cascade');
  // The registered pool subroute handler deletes without logAudit, so no delete/pool event exists to assert on.
  note(`accepted: ${JSON.stringify(await prompts(page))}; GET /api/v1/pools -> ${JSON.stringify(poolsAfter)}`);
};
