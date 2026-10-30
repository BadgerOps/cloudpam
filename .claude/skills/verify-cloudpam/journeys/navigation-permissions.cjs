// Uses two of the five per-IP login tokens: login() as the admin and the viewer's form login.
const assert = require('node:assert/strict');

const VIEWER = { username: 'verify-viewer', password: 'ViewerDev12345!', role: 'viewer' };

const VIEWER_LINKS = [
  'Dashboard', 'Address Pools', 'Allocated Blocks', 'Cloud Accounts',
  'Discovery', 'Drift Detection', 'Schema Planner', 'Recommendations', 'AI Planner', 'Release Notes',
];
const ADMIN_ONLY_LINKS = ['Audit Log', 'API Keys', 'Identity', 'Configuration', 'Log Destinations'];
const GATED_ROUTES = [
  '/audit', '/config/api-keys', '/identity', '/config',
  '/config/log-destinations', '/config/updates', '/config/security', '/config/users',
];

module.exports = async ({ page, context, login, api, shot, aria, note }) => {
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  const sidebar = async () => Promise.all((await nav.getByRole('link').all()).map(async (link) => ({
    name: (await link.innerText()).trim(),
    href: await link.getAttribute('href'),
  })));

  await login();
  const adminLinks = await sidebar();
  note(`admin sidebar: ${adminLinks.map((l) => l.name).join(', ')}`);
  for (const name of [...VIEWER_LINKS, ...ADMIN_ONLY_LINKS]) {
    assert.ok(adminLinks.some((l) => l.name === name), `admin sidebar shows ${name}`);
  }
  await shot('00-admin-sidebar');

  const created = await api('POST', '/api/v1/auth/users', { ...VIEWER, email: 'verify-viewer@localhost' });
  assert.ok([201, 409].includes(created.status), `create viewer user: got ${created.status} ${JSON.stringify(created.body)}`);
  note(`POST /api/v1/auth/users ${VIEWER.username} role=${VIEWER.role} -> ${created.status}`);

  await context.clearCookies();
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.getByPlaceholder('admin').fill(VIEWER.username);
  await page.locator('input[autocomplete="current-password"]').fill(VIEWER.password);
  await page.getByRole('button', { name: 'Sign In' }).click();
  await nav.waitFor({ timeout: 15000 });
  const me = await api('GET', '/api/v1/auth/me');
  assert.equal(me.body.user.username, VIEWER.username, 'auth/me names the viewer');
  note(`logged in as ${VIEWER.username}; permissions=${JSON.stringify(me.body.permissions)}`);

  const viewerLinks = await sidebar();
  note(`viewer sidebar: ${viewerLinks.map((l) => `${l.name} (${l.href})`).join(', ')}`);
  assert.deepEqual(viewerLinks.map((l) => l.name), VIEWER_LINKS, 'viewer sidebar lists exactly the ungated pages');
  await shot('01-viewer-sidebar');
  await aria('01-viewer-sidebar');

  for (const { name, href } of viewerLinks) {
    await page.goto(href);
    await nav.waitFor();
    await page.waitForLoadState('networkidle');
    assert.equal(new URL(page.url()).pathname, href, `viewer opens ${name} at ${href}`);
    note(`open ${href} -> stays on ${href}`);
  }

  for (const route of GATED_ROUTES) {
    await page.goto(route);
    await page.waitForURL((u) => u.pathname === '/', { timeout: 10000 });
    note(`gated ${route} -> redirected to /`);
  }

  await page.getByRole('button', { name: VIEWER.username }).click();
  await page.getByRole('button', { name: 'Profile' }).waitFor();
  assert.equal(await page.getByRole('button', { name: 'My API Keys' }).count(), 0, 'viewer user menu has no My API Keys entry');
  await shot('02-viewer-user-menu');
  note('viewer user menu lists Profile and no My API Keys');

  for (const [path, expected] of [['/api/v1/audit', 403], ['/api/v1/auth/keys', 403], ['/api/v1/settings/security', 403], ['/api/v1/pools', 200]]) {
    const res = await api('GET', path);
    assert.equal(res.status, expected, `viewer GET ${path}`);
  }
};
