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

  const seedKey = 'aws:111111111111';
  const seedBefore = after.body.find((a) => a.key === seedKey);
  assert.ok(seedBefore, 'seeded prod-platform account present');
  const renamed = `prod-platform-${id}`;
  const seedRow = page.getByRole('row', { name: /prod-platform/ });
  await seedRow.getByRole('button', { name: 'Edit' }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit Account' });
  await dialog.waitFor();
  assert.equal(await dialog.getByLabel('Name (required)').evaluate((el) => el === document.activeElement), true, 'name field has focus on open');
  await page.keyboard.press('Escape');
  await dialog.waitFor({ state: 'detached' });
  assert.equal(await seedRow.getByRole('button', { name: 'Edit' }).evaluate((el) => el === document.activeElement), true, 'Edit button regains focus after Escape');
  await seedRow.getByRole('button', { name: 'Edit' }).click();
  await dialog.waitFor();
  assert.equal(await dialog.getByLabel('Key (required)').inputValue(), seedKey, 'edit form pre-filled with the key');
  assert.equal(await dialog.getByLabel('Key (required)').isEditable(), false, 'key is read-only on edit');
  assert.equal(await dialog.getByLabel('Name (required)').inputValue(), seedBefore.name, 'edit form pre-filled with the name');
  await dialog.getByLabel('Name (required)').fill(renamed);
  await dialog.getByLabel('Regions').fill('us-east-1, us-west-2');
  await shot('05-edit-filled');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await page.getByText(`Updated ${renamed}`).waitFor({ timeout: 10000 });
  await dialog.waitFor({ state: 'detached' });
  const editedRow = page.getByRole('row', { name: new RegExp(renamed) });
  await editedRow.getByRole('cell', { name: 'us-east-1, us-west-2' }).waitFor();
  await editedRow.getByRole('cell', { name: 'just now' }).waitFor();
  await shot('06-account-edited');
  await aria('06-account-edited');

  const edited = await api('GET', '/api/v1/accounts');
  const seedAfter = edited.body.find((a) => a.key === seedKey);
  assert.equal(seedAfter.name, renamed, 'GET /api/v1/accounts shows the new name');
  assert.deepEqual(seedAfter.regions, ['us-east-1', 'us-west-2'], 'GET /api/v1/accounts shows the new regions');
  assert.equal(seedAfter.provider, seedBefore.provider, 'untouched provider preserved');
  assert.equal(seedAfter.id, seedBefore.id, 'same account id');
  note(`edit ${seedKey}: ${JSON.stringify(seedBefore)} -> ${JSON.stringify(seedAfter)}`);
  const audit = await api('GET', '/api/v1/audit?action=update&resource_type=account&limit=1');
  assert.equal(audit.status, 200, 'GET /api/v1/audit is 200');
  const event = audit.body.events[0];
  assert.ok(event, 'an update/account audit event exists after the edit');
  assert.equal(event.resource_type, 'account');
  assert.equal(event.action, 'update');
  assert.equal(event.resource_id, String(seedBefore.id), 'audit event names the edited account');
  assert.equal(event.resource_name, renamed, 'audit event carries the new name');
  note(`audit update/account -> ${JSON.stringify(event)}`);
};
