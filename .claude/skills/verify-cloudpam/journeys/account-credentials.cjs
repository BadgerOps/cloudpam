const assert = require('node:assert/strict');
const { generateKeyPairSync } = require('node:crypto');

const serverTestTimeoutMs = 15000;

module.exports = async ({ page, login, api, shot, aria, note }) => {
  const consoleErrors = [];
  let loggedIn = false;
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    note(`console.error ${m.text()}`);
    if (loggedIn) consoleErrors.push(m.text());
  });

  const id = String(Date.now()).slice(-8);
  const projectId = `verify-cred-${id}`;
  const key = `gcp:${projectId}`;
  const name = `verify-gcp-${id}`;
  const clientEmail = `cloudpam@${projectId}.iam.gserviceaccount.com`;
  const pem = generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey.export({ type: 'pkcs8', format: 'pem' });
  const keyMarker = pem.split('\n')[2];
  assert.ok(keyMarker.length >= 40, 'key marker is a full base64 line');
  const keyJSON = JSON.stringify({
    type: 'service_account',
    project_id: projectId,
    private_key_id: `verify-${id}`,
    private_key: pem,
    client_email: clientEmail,
    client_id: '123456789012345678901',
    token_uri: 'https://oauth2.googleapis.com/token',
  });
  const leaks = (text, where) => {
    assert.ok(!text.includes(keyMarker), `${where} does not contain the private key body`);
    assert.ok(!text.includes('BEGIN PRIVATE KEY'), `${where} does not contain a PEM header`);
    assert.ok(!text.includes('payload_encrypted'), `${where} does not contain payload_encrypted`);
  };
  note(`using ${key} ${name} ${clientEmail}`);

  await login();
  loggedIn = true;
  await page.getByRole('link', { name: 'Cloud Accounts' }).click();
  await page.getByRole('heading', { name: 'Cloud Accounts' }).waitFor();
  await page.getByRole('button', { name: 'New Account' }).click();
  const form = page.getByRole('form', { name: 'New Account' });
  await form.getByPlaceholder('e.g., aws:123456789012').fill(key);
  await form.getByPlaceholder('e.g., Production AWS').fill(name);
  await form.getByLabel('Provider').selectOption('gcp');
  await form.getByRole('button', { name: 'Create Account' }).click();
  await page.getByText('Account created').waitFor({ timeout: 10000 });
  const row = page.getByRole('row', { name: new RegExp(name) });
  await row.waitFor();
  const accounts = await api('GET', '/api/v1/accounts');
  const account = accounts.body.find((a) => a.key === key);
  assert.ok(account, 'new gcp account present in GET /api/v1/accounts');
  const credentialPath = `/api/v1/accounts/${account.id}/credentials`;

  const before = await api('GET', credentialPath);
  assert.equal(before.status, 404, 'no credential before save');
  assert.equal(before.body.error, 'no credential stored for this account');

  await row.getByRole('button', { name: 'Credentials' }).click();
  const modal = page.getByTestId('account-credential-modal');
  await modal.waitFor();
  const badge = modal.getByTestId('credential-state');
  await badge.filter({ hasText: 'None stored' }).waitFor({ timeout: 10000 });
  const keyField = modal.getByLabel('Service account key JSON (required)');
  await keyField.waitFor();
  await shot('01-modal-none');

  await keyField.fill(keyJSON);
  await modal.getByRole('button', { name: 'Save', exact: true }).click();
  await page.getByText('Credential saved').waitFor({ timeout: 10000 });
  await badge.filter({ hasText: 'Stored' }).waitFor({ timeout: 10000 });
  await modal.getByText(clientEmail).waitFor();
  await modal.getByText(projectId, { exact: true }).waitFor();
  assert.equal(await keyField.inputValue(), '', 'key field is empty after save');
  leaks(await page.content(), 'page HTML after save');
  await shot('02-modal-saved');
  await aria('02-modal-saved');

  const saved = await api('GET', credentialPath);
  assert.equal(saved.status, 200);
  assert.equal(saved.body.type, 'gcp_service_account');
  assert.equal(saved.body.summary.client_email, clientEmail);
  assert.equal(saved.body.summary.project_id, projectId);
  assert.equal(saved.body.last_test, undefined, 'no test result before the first test');
  leaks(JSON.stringify(saved.body), 'GET credentials body');
  assert.ok(!JSON.stringify(saved.body).includes('private_key'), 'GET credentials body has no private_key field');
  note(`saved: ${JSON.stringify(saved.body)}`);

  await modal.getByRole('button', { name: 'Test', exact: true }).click();
  const result = modal.getByTestId('credential-test-result');
  await result.waitFor({ timeout: 2 * serverTestTimeoutMs });
  const resultText = await result.textContent();
  note(`test result: ${resultText}`);
  assert.match(resultText, /failed/, 'the throwaway key fails the provider test, with or without outbound network');
  leaks(resultText, 'test result text');
  await badge.filter({ hasText: 'Failing' }).waitFor({ timeout: 10000 });
  leaks(await page.content(), 'page HTML after test');
  await shot('03-modal-tested');
  await aria('03-modal-tested');

  const tested = await api('GET', credentialPath);
  assert.equal(tested.status, 200);
  assert.equal(tested.body.last_test.status, 'failed', 'the test result is recorded on the credential');
  assert.ok(tested.body.last_test.error, 'a sanitized error is recorded');
  leaks(JSON.stringify(tested.body), 'GET credentials body after test');
  note(`recorded test: ${JSON.stringify(tested.body.last_test)}`);

  await modal.getByRole('button', { name: 'Clear', exact: true }).click();
  await page.getByText('Credential cleared').waitFor({ timeout: 10000 });
  await badge.filter({ hasText: 'None stored' }).waitFor({ timeout: 10000 });
  await result.waitFor({ state: 'detached' });
  await shot('04-modal-cleared');
  await aria('04-modal-cleared');
  const cleared = await api('GET', credentialPath);
  assert.equal(cleared.status, 404, 'no credential after clear');
  assert.equal(cleared.body.error, 'no credential stored for this account');

  const audit = await api('GET', '/api/v1/audit?resource_type=account_credential&limit=20');
  assert.equal(audit.status, 200, 'GET /api/v1/audit is 200');
  const events = audit.body.events.filter((e) => e.resource_id === String(account.id));
  const actions = new Set(events.map((e) => e.action));
  assert.ok(actions.has('update'), 'audit has the update event');
  assert.ok(actions.has('test'), 'audit has the test event');
  assert.ok(actions.has('delete'), 'audit has the delete event');
  assert.ok(events.every((e) => e.resource_name === name), 'audit events name the account');
  leaks(JSON.stringify(events), 'audit events');
  note(`audit account_credential events: ${JSON.stringify(events.map((e) => [e.action, e.status_code, e.changes]))}`);

  note(`console errors after login: ${consoleErrors.length}`);
  assert.deepEqual(consoleErrors, ['Failed to load resource: the server responded with a status of 404 (Not Found)'], 'the only console error is Chromium logging the credentials GET that answers 404 before anything is stored');
};
