const assert = require('node:assert/strict');

module.exports = async ({ page, login, api, shot, aria, note }) => {
  // An aws key needs exactly 12 digits; Date.now() has 13.
  const id = String(Date.now()).slice(-12);
  const key = `aws:${id}`;
  const name = `verify-acct-${id}`;
  const keyField = () => page.getByPlaceholder('e.g., aws:123456789012');
  const nameField = () => page.getByPlaceholder('e.g., Production AWS');
  note(`using ${key} ${name}`);

  await login();
  await page.getByRole('link', { name: 'Cloud Accounts' }).click();
  await page.getByRole('heading', { name: 'Cloud Accounts' }).waitFor();
  await page.getByRole('button', { name: 'New Account' }).click();
  await keyField().fill(key);
  await nameField().fill(name);
  await shot('01-form-filled');
  await page.getByRole('button', { name: 'Create Account' }).click();
  await page.getByText('Account created').waitFor({ timeout: 10000 });
  const row = page.getByRole('row', { name: new RegExp(name) });
  await row.waitFor();
  await shot('02-account-created');
  await aria('02-account-created');

  const list = await api('GET', '/api/v1/accounts');
  assert.equal(list.status, 200);
  assert.ok(list.body.some((a) => a.key === key), 'new key present in GET /api/v1/accounts');

  await page.getByRole('button', { name: 'New Account' }).click();
  await keyField().fill('aws:111111111111');
  await nameField().fill('dup');
  await page.getByRole('button', { name: 'Create Account' }).click();
  await page.getByText('account key already exists').first().waitFor({ timeout: 10000 });
  await shot('03-duplicate-refused');
  const dup = await api('POST', '/api/v1/accounts', { key: 'aws:111111111111', name: 'dup', provider: 'aws' });
  assert.equal(dup.status, 409, 'duplicate key is 409');
  assert.equal(dup.body.error, 'account key already exists');
  note(`duplicate key -> ${dup.status} ${JSON.stringify(dup.body)}`);
  await page.getByRole('button', { name: 'Cancel' }).click();

  await row.getByRole('button', { name: 'Delete' }).click();
  await page.getByText('Account deleted').waitFor({ timeout: 10000 });
  await row.waitFor({ state: 'detached' });
  await shot('04-account-deleted');
  const after = await api('GET', '/api/v1/accounts');
  assert.ok(!after.body.some((a) => a.key === key), 'deleted key absent from GET /api/v1/accounts');
};
