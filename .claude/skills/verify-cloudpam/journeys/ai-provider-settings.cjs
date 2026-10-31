const assert = require('node:assert/strict');

// AI Provider card on the Configuration page (#283). Needs two OpenAI-compatible endpoints reachable from the
// server: an auth-required one (AI_KEYED_ENDPOINT, accepts AI_KEY) and an authless one (AI_AUTHLESS_ENDPOINT).
// Defaults match the fake provider used in verification: node fake-openai.cjs 18992 verify-token-ABC / 18991.
const KEYED = process.env.AI_KEYED_ENDPOINT || 'http://127.0.0.1:18992/v1';
const AUTHLESS = process.env.AI_AUTHLESS_ENDPOINT || 'http://127.0.0.1:18991/v1';
const KEY = process.env.AI_KEY || 'verify-token-ABC';
const MODEL = 'fake-ui';

module.exports = async ({ page, login, api, shot, aria, note }) => {
  const consoleErrors = [];
  let loggedIn = false;
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    note(`console.error ${m.text()}`);
    if (loggedIn) consoleErrors.push(m.text());
  });

  await login();
  loggedIn = true;

  await page.getByRole('link', { name: 'Configuration', exact: true }).click();
  await page.getByRole('heading', { name: 'Configuration', level: 1 }).waitFor();
  const card = page.locator('section').filter({ has: page.getByRole('heading', { name: 'AI Provider' }) });
  const badge = card.getByTestId('ai-provider-state');
  await badge.waitFor();
  const endpoint = card.getByLabel('Endpoint', { exact: true });
  const model = card.getByLabel('Model', { exact: true });
  const key = card.getByLabel('API key', { exact: true });
  await endpoint.waitFor();
  note(`initial badge: ${await badge.textContent()}`);
  await card.scrollIntoViewIfNeeded();
  await shot('ai-card-initial');

  // Save endpoint + model + key.
  await endpoint.fill(KEYED);
  await model.fill(MODEL);
  await key.fill(KEY);
  await card.getByRole('button', { name: 'Save', exact: true }).click();
  await page.getByText('AI provider settings saved').waitFor({ timeout: 10000 });
  await card.getByText('Key configured (stored)').waitFor();
  assert.equal(await key.inputValue(), '', 'key field is empty after save');
  assert.equal(await badge.textContent(), 'Configured', 'material change resets the badge to Configured');
  assert.equal(await card.getByTestId('ai-source-api_key').textContent(), 'database');
  await shot('ai-card-saved');
  assert.ok(!(await page.content()).includes(KEY), 'page HTML does not contain the API key after save');
  const afterSave = await api('GET', '/api/v1/settings/ai');
  assert.equal(afterSave.status, 200);
  assert.equal(afterSave.body.model, MODEL);
  assert.equal(afterSave.body.endpoint, KEYED);
  assert.equal(afterSave.body.api_key_configured, true);
  assert.ok(!JSON.stringify(afterSave.body).includes(KEY), 'GET body does not contain the API key');
  note(`saved: ${JSON.stringify(afterSave.body)}`);

  // Test connection.
  await card.getByRole('button', { name: 'Test connection' }).click();
  const okLine = card.getByText(/^Reachable · model /);
  const failLine = card.locator('div.text-red-700');
  await okLine.or(failLine).first().waitFor({ timeout: 20000 });
  const resultText = (await okLine.isVisible()) ? await okLine.textContent() : await failLine.first().textContent();
  note(`test result line: ${resultText}`);
  assert.match(resultText, new RegExp(`^Reachable · model ${MODEL} · streaming yes · \\d+ ms$`));
  await badge.filter({ hasText: 'Verified' }).waitFor({ timeout: 10000 });
  await shot('ai-card-tested');
  await aria('ai-card-tested');
  assert.ok(!(await page.content()).includes(KEY), 'page HTML does not contain the API key after test');

  // No stale test result after a save; helper text says the test uses saved settings.
  await card.getByText('Tests the saved settings, not unsaved edits.').waitFor({ state: 'visible', timeout: 5000 });
  await model.fill(MODEL + '-b');
  await card.getByRole('button', { name: 'Save', exact: true }).click();
  await page.getByText('AI provider settings saved').first().waitFor({ timeout: 10000 });
  await okLine.waitFor({ state: 'detached', timeout: 10000 });
  assert.equal(await okLine.count(), 0, 'Reachable line gone after save');
  assert.equal(await badge.textContent(), 'Configured', 'badge reads Configured after save');
  await card.getByText('Tests the saved settings, not unsaved edits.').waitFor({ state: 'visible' });
  await shot('ai-card-after-save');

  // Endpoint change with a stored key and no new key: the card asks for the key and sends nothing.
  await endpoint.fill(AUTHLESS);
  await card.getByRole('button', { name: 'Save', exact: true }).click();
  await card.getByText('Enter the API key again for the new endpoint, or clear the stored key').waitFor({ timeout: 5000 });
  const unchanged = await api('GET', '/api/v1/settings/ai');
  assert.equal(unchanged.body.endpoint, KEYED, 'endpoint unchanged while the key prompt is shown');
  await shot('ai-card-key-prompt');

  await card.getByLabel('Clear the stored key with this save').check();
  await card.getByRole('button', { name: 'Save', exact: true }).click();
  await page.getByText('AI provider settings saved').first().waitFor({ timeout: 10000 });
  await card.getByText('No key configured').waitFor({ timeout: 10000 });
  assert.equal(await card.getByTestId('ai-source-api_key').textContent(), 'none');
  await card.getByText('A stored endpoint never uses CLOUDPAM_LLM_API_KEY').waitFor();
  const cleared = await api('GET', '/api/v1/settings/ai');
  assert.equal(cleared.body.endpoint, AUTHLESS);
  assert.equal(cleared.body.api_key_configured, false);
  assert.equal(cleared.body.sources.api_key, 'none');
  note(`cleared: ${JSON.stringify(cleared.body)}`);
  await shot('ai-card-cleared');

  note(`console errors after login: ${consoleErrors.length}`);
  assert.deepEqual(consoleErrors, [], 'no console errors after login');
};
