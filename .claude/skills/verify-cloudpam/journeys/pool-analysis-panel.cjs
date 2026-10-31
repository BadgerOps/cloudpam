const assert = require('node:assert/strict');

// Feature: pool detail "Address Analysis" section (#281). Entry points: Address Pools table
// ("View details") and the Dashboard pool tree.
module.exports = async ({ page, login, api, shot, aria, note }) => {
  const consoleErrors = [];
  page.on('console', (m) => {
    if (m.type() === 'error') { consoleErrors.push(m.text()); note(`console error: ${m.text()}`); }
  });
  page.on('pageerror', (e) => { consoleErrors.push(String(e)); note(`pageerror: ${e}`); });

  const fmt = (n) => (n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1000 ? (n / 1000).toFixed(1) + 'K' : String(n));
  const badgeFor = (s) => (s <= 20 ? 'Healthy' : s <= 50 ? 'Attention' : 'Severe');
  const toggle = () => page.getByRole('button', { name: 'Address Analysis' });
  const waitLoaded = async () => {
    await toggle().waitFor();
    await page.getByText('Analyzing free space...').waitFor({ state: 'detached', timeout: 15000 });
    await page.getByText('Scoring fragmentation...').waitFor({ state: 'detached', timeout: 15000 });
  };

  await login();
  const preLogin = consoleErrors.length; // the logged-out /login page probes auth and gets an expected 401
  note(`console errors before login (expected 401 probe): ${preLogin}`);
  const pools = (await api('GET', '/api/v1/pools')).body;
  const sup = pools.find((p) => p.name === 'Corp Supernet');
  const gapsResp = await api('POST', '/api/v1/analysis/gaps', { pool_id: sup.id });
  const fragResp = await api('POST', '/api/v1/analysis/fragmentation', { pool_ids: [sup.id] });
  assert.equal(gapsResp.status, 200);
  assert.equal(fragResp.status, 200);
  const gaps = gapsResp.body, frag = fragResp.body;
  note(`API supernet: free=${gaps.free_addresses} util=${gaps.utilization_percent} blocks=${gaps.available_blocks.length} score=${frag.score} issues=${frag.issues.length} recs=${frag.recommendations.length}`);

  await page.getByRole('link', { name: 'Address Pools' }).click();
  await page.getByRole('heading', { name: 'Address Pools' }).waitFor();
  await page.getByRole('cell', { name: 'Corp Supernet' }).waitFor();
  await page.getByRole('row', { name: /Corp Supernet/ }).getByTitle('View details').click();
  await page.getByRole('heading', { name: 'Corp Supernet' }).waitFor({ timeout: 10000 });
  await waitLoaded();
  note('feature: Address Analysis section rendered on Address Pools');
  await shot('pools-panel-supernet');
  await aria('pools-panel-supernet');

  const section = page.locator('div.border-t', { has: toggle() });
  const sectionText = await section.innerText();
  note(`section text: ${sectionText.replace(/\n+/g, ' | ')}`);
  assert.ok(sectionText.includes(fmt(gaps.free_addresses)), `free ${fmt(gaps.free_addresses)} shown`);
  assert.ok(sectionText.includes(gaps.utilization_percent.toFixed(1) + '%'), 'utilization shown');
  assert.equal(Number(await page.getByTestId('available-block-count').innerText()), gaps.available_blocks.length);
  const ranges = await page.getByTestId('available-range').allInnerTexts();
  assert.ok(ranges.length <= 5, 'at most 5 ranges');
  assert.equal(ranges.length, Math.min(5, gaps.available_blocks.length));
  assert.deepEqual(ranges, gaps.available_blocks.slice(0, 5).map((b) => b.cidr));
  const rest = gaps.available_blocks.length - 5;
  assert.ok(rest > 0, 'seeded data must exceed 5 ranges');
  await section.getByText(`+${rest} more`, { exact: true }).first().waitFor();
  const scoreText = await section.getByText(/\d+ \/ 100/).innerText();
  assert.equal(scoreText.trim(), `${frag.score} / 100`);
  const badge = (await page.getByTestId('fragmentation-badge').innerText()).trim();
  assert.ok(['Healthy', 'Attention', 'Severe'].includes(badge));
  assert.equal(badge, badgeFor(frag.score));
  note(`badge ${badge} for score ${frag.score}`);
  if (frag.issues.length > 3) await section.getByText(`+${frag.issues.length - 3} more`, { exact: true }).first().waitFor();
  for (const r of frag.recommendations.slice(0, 3)) assert.ok(sectionText.includes(r), `recommendation shown: ${r}`);

  // Collapse / expand
  await toggle().click();
  assert.equal(await toggle().getAttribute('aria-expanded'), 'false');
  assert.equal(await page.getByTestId('available-block-count').count(), 0);
  await shot('pools-panel-collapsed');
  await toggle().click();
  assert.equal(await toggle().getAttribute('aria-expanded'), 'true');
  await page.getByTestId('available-block-count').waitFor();
  note('feature: collapse/expand works');

  // Leaf
  await page.getByPlaceholder('Search pools by name, CIDR, or description...').fill('Prod East');
  await page.getByRole('row', { name: /Prod East VPC/ }).getByTitle('View details').click();
  await page.getByRole('heading', { name: 'Prod East VPC' }).waitFor({ timeout: 10000 });
  await waitLoaded();
  await page.getByText('No child allocations yet; the whole range is available').waitFor();
  assert.equal(await page.getByTestId('available-range').count(), 0);
  await shot('pools-panel-leaf');
  await aria('pools-panel-leaf');
  note('feature: no-children state');

  // Dashboard
  await page.getByRole('navigation', { name: 'Main navigation' }).locator('a[href="/"]').click();
  await page.getByRole('heading', { name: 'Pool Hierarchy' }).waitFor();
  await page.getByText('Corp Supernet', { exact: true }).first().waitFor({ timeout: 10000 });
  await aria('dashboard');
  await page.getByText('Corp Supernet', { exact: true }).first().click();
  await toggle().waitFor({ timeout: 10000 });
  await waitLoaded();
  assert.equal(Number(await page.getByTestId('available-block-count').innerText()), gaps.available_blocks.length);
  assert.equal((await page.getByTestId('fragmentation-badge').innerText()).trim(), badgeFor(frag.score));
  await shot('dashboard-panel');
  note('feature: Address Analysis section on Dashboard');

  const after = consoleErrors.slice(preLogin);
  assert.deepEqual(after, [], `console errors after login: ${after.join(" ; ")}`);
};
