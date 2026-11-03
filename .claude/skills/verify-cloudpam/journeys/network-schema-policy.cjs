const assert = require('node:assert/strict');

// Feature network-schema-policy (#313): the stored policy is shown and changed on Configuration,
// and Discovery > Merged Network starts from it until the user picks a request-scoped one.
const POLICY_PATH = '/api/v1/settings/network-schema-policy';

module.exports = async ({ page, login, api, shot, aria, note }) => {
  const consoleErrors = [];
  const networkRequests = [];
  let loggedIn = false;
  page.on('console', (m) => {
    if (m.type() !== 'error') return;
    note(`console.error ${m.text()}`);
    if (loggedIn) consoleErrors.push(m.text());
  });
  page.on('request', (r) => {
    if (r.url().includes('/api/v1/network/')) networkRequests.push(r.url());
  });

  const openDiscoveryNetwork = async () => {
    await page.getByRole('link', { name: 'Discovery' }).click();
    await page.getByRole('heading', { name: 'Discovery' }).first().waitFor();
    await page.getByRole('button', { name: 'Merged Network' }).click();
    await page.getByLabel('Schema policy').waitFor({ timeout: 10000 });
  };
  const openConfiguration = async () => {
    await page.getByRole('link', { name: 'Configuration', exact: true }).click();
    await page.getByRole('heading', { name: 'Configuration', level: 1 }).waitFor();
    const card = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Network Schema Policy' }) });
    await card.getByLabel('Policy', { exact: true }).waitFor({ timeout: 10000 });
    return card;
  };
  const policySummary = () => page.getByText(/^(account|region|global|manual) policy, (stored default|request-scoped)$/);

  await login();
  loggedIn = true;

  const initial = await api('GET', POLICY_PATH);
  assert.equal(initial.status, 200);
  assert.equal(initial.body.name, 'account_level', 'fresh instance stores the account policy');

  // 1. Discovery starts from the stored policy and asks the server for it by sending none.
  await openDiscoveryNetwork();
  await policySummary().waitFor({ timeout: 10000 });
  assert.equal(await page.getByLabel('Schema policy').inputValue(), 'account_level');
  assert.equal(await policySummary().textContent(), 'account policy, stored default');
  const firstViewRequests = networkRequests.filter((u) => /\/api\/v1\/network\/(hierarchy|flat|conflicts)/.test(u));
  assert.ok(firstViewRequests.length > 0, 'a network view was requested');
  assert.ok(firstViewRequests.every((u) => !u.includes('schema_policy=')), `no schema_policy sent by default: ${firstViewRequests.join(' ')}`);
  await shot('01-discovery-stored-account');

  // 2. Configuration shows the stored policy and saves a new one.
  let card = await openConfiguration();
  const policySelect = card.getByLabel('Policy', { exact: true });
  const save = card.getByRole('button', { name: 'Save', exact: true });
  assert.equal(await policySelect.inputValue(), 'account_level');
  assert.equal(await save.isDisabled(), true, 'Save is disabled with no edit');
  await card.scrollIntoViewIfNeeded();
  await shot('02-config-card-initial');
  await policySelect.selectOption('region_level');
  assert.equal(await save.isDisabled(), false, 'Save enables on an edit');
  await shot('03-config-card-region-selected');
  await save.click();
  await page.getByText('Network schema policy saved').waitFor({ timeout: 10000 });
  await shot('04-config-card-region-saved');
  await aria('04-config-card-region-saved');
  const saved = await api('GET', POLICY_PATH);
  assert.equal(saved.status, 200);
  assert.equal(saved.body.name, 'region_level');
  assert.equal(saved.body.ownership_strategy, 'region');
  note(`stored after save: ${JSON.stringify(saved.body)}`);
  const audit = await api('GET', '/api/v1/audit?action=update&resource_type=settings&limit=50');
  assert.equal(audit.status, 200);
  const events = Array.isArray(audit.body) ? audit.body : audit.body.items || audit.body.events || [];
  assert.ok(events.some((e) => JSON.stringify(e).includes('network_schema_policy')), 'audit event for the policy update exists');

  // 3. Discovery now starts from the region policy, still without naming it in the request.
  networkRequests.length = 0;
  await openDiscoveryNetwork();
  await page.getByText('region policy, stored default').waitFor({ timeout: 10000 });
  assert.equal(await page.getByLabel('Schema policy').inputValue(), 'region_level');
  const secondViewRequests = networkRequests.filter((u) => /\/api\/v1\/network\/(hierarchy|flat|conflicts)/.test(u));
  assert.ok(secondViewRequests.every((u) => !u.includes('schema_policy=')), `no schema_policy sent by default: ${secondViewRequests.join(' ')}`);
  const served = await api('GET', '/api/v1/network/hierarchy');
  assert.equal(served.status, 200);
  assert.equal(served.body.schema_policy.name, 'region_level', 'server applies the stored policy to a request naming none');
  await shot('05-discovery-stored-region');
  await aria('05-discovery-stored-region');

  // 4. Picking a policy in Discovery is request-scoped and leaves the stored one alone.
  const overrideRequest = page.waitForRequest((r) => r.url().includes('/api/v1/network/') && r.url().includes('schema_policy=global'), { timeout: 10000 });
  await page.getByLabel('Schema policy').selectOption('global');
  const sent = await overrideRequest;
  note(`request-scoped pick sent ${sent.url()}`);
  await page.getByText('global policy, request-scoped').waitFor({ timeout: 10000 });
  await shot('06-discovery-global-override');
  const untouched = await api('GET', POLICY_PATH);
  assert.equal(untouched.body.name, 'region_level', 'request-scoped pick does not change the stored policy');

  // 5. The manual policy keeps its duplicate scope through a save.
  card = await openConfiguration();
  assert.equal(await card.getByLabel('Policy', { exact: true }).inputValue(), 'region_level');
  await card.getByLabel('Policy', { exact: true }).selectOption('manual');
  const scope = card.getByLabel('Duplicate CIDRs', { exact: true });
  await scope.waitFor();
  await scope.selectOption('global');
  await shot('07-config-card-manual-global');
  await card.getByRole('button', { name: 'Save', exact: true }).click();
  await page.getByText('Network schema policy saved').first().waitFor({ timeout: 10000 });
  await page.reload();
  card = await openConfiguration();
  assert.equal(await card.getByLabel('Policy', { exact: true }).inputValue(), 'manual');
  assert.equal(await card.getByLabel('Duplicate CIDRs', { exact: true }).inputValue(), 'global');
  await shot('08-config-card-manual-reloaded');
  const manual = await api('GET', POLICY_PATH);
  assert.equal(manual.body.name, 'manual');
  assert.equal(manual.body.duplicate_scope, 'global');
  assert.equal(manual.body.manual_relationships, true);
  note(`stored manual policy: ${JSON.stringify(manual.body)}`);

  note(`console errors after login: ${consoleErrors.length}`);
  assert.deepEqual(consoleErrors, [], 'no console errors after login');
};
