const assert = require('node:assert/strict');

// #291: an admin sets a per-key rate limit on the API Keys page and the table shows it.
module.exports = async ({ page, baseURL, login, api, shot, aria, note }) => {
  const name = `verify-throttled-${Date.now()}`;
  await login();
  await page.goto(`${baseURL}/config/api-keys`);
  await page.getByRole('heading', { name: 'API Keys' }).waitFor();
  await page.getByRole('button', { name: 'Create Key' }).click();
  await page.getByPlaceholder('e.g. CI Pipeline').fill(name);
  await page.getByLabel('Requests per second').fill('0.2');
  await page.getByLabel('Burst').fill('2');
  await shot('01-create-form-filled');

  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await page.getByText('API key created').waitFor({ timeout: 10000 });
  await shot('02-key-created');

  const row = page.getByRole('row', { name: new RegExp(name) });
  await row.getByText('0.2/s, burst 2').waitFor();
  await shot('03-key-row-rate-limit');
  await aria('03-key-row-rate-limit');

  const list = await api('GET', '/api/v1/auth/keys');
  assert.equal(list.status, 200);
  const created = list.body.keys.find((k) => k.name === name);
  assert.ok(created, 'created key present in GET /api/v1/auth/keys');
  assert.deepEqual(created.rate_limit, { requests_per_second: 0.2, burst: 2 });
  note(`key ${created.id} rate_limit ${JSON.stringify(created.rate_limit)}`);

  // A key created without the block shows the server default.
  const plain = await api('POST', '/api/v1/auth/keys', { name: `${name}-plain`, scopes: ['pools:read'] });
  assert.equal(plain.status, 201);
  assert.equal(plain.body.rate_limit, undefined);
  await page.reload();
  await page.getByRole('row', { name: new RegExp(`${name}-plain`) }).getByText('Default').waitFor();
  await shot('04-plain-key-default');
};
