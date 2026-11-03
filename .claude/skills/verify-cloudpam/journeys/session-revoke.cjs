// Uses two of the five per-IP login tokens: login() as the admin and the API login of the target user.
const assert = require('node:assert/strict');

const TARGET = { username: 'verify-revoked', password: 'RevokedDev12345!', role: 'viewer' };

// A request helper for a second browser context, mirroring the runner's api().
function requester(ctx, baseURL) {
  return async (method, apiPath, body) => {
    const headers = { 'Content-Type': 'application/json' };
    if (method !== 'GET') {
      let csrf = (await ctx.cookies(baseURL)).find((c) => c.name === 'csrf_token');
      if (!csrf) { await ctx.request.get('/api/v1/pools'); csrf = (await ctx.cookies(baseURL)).find((c) => c.name === 'csrf_token'); }
      if (csrf) headers['X-CSRF-Token'] = csrf.value;
    }
    const res = await ctx.request.fetch(apiPath, { method, headers, data: body === undefined ? undefined : JSON.stringify(body) });
    const text = await res.text();
    let parsed; try { parsed = JSON.parse(text); } catch { parsed = text; }
    return { status: res.status(), body: parsed };
  };
}

module.exports = async ({ page, context, baseURL, username, login, api, shot, aria, note }) => {
  await login();

  const created = await api('POST', '/api/v1/auth/users', { ...TARGET, email: 'verify-revoked@localhost' });
  assert.ok([201, 409].includes(created.status), `create target user: got ${created.status} ${JSON.stringify(created.body)}`);

  const victim = await context.browser().newContext({ baseURL });
  const victimApi = requester(victim, baseURL);
  const loggedIn = await victimApi('POST', '/api/v1/auth/login', { username: TARGET.username, password: TARGET.password });
  assert.equal(loggedIn.status, 200, `target login: ${JSON.stringify(loggedIn.body)}`);
  const before = await victimApi('GET', '/api/v1/auth/me');
  assert.equal(before.status, 200, 'target session is valid before the revoke');
  note(`target ${TARGET.username} holds a live session (auth/me -> ${before.status})`);

  await page.goto('/config/users');
  const row = page.getByRole('row').filter({ hasText: TARGET.username });
  await row.waitFor();
  await shot('00-users-table');
  await row.getByTitle('Sign out everywhere').click();
  await page.getByText(`Signed ${TARGET.username} out everywhere`).waitFor({ timeout: 10000 });
  await shot('01-admin-revoked-toast');
  await aria('01-admin-revoked-toast');
  note('admin row action: confirmation accepted, success toast shown');

  const after = await victimApi('GET', '/api/v1/auth/me');
  assert.equal(after.status, 401, `target session should be gone, got ${after.status}`);
  note(`target session revoked (auth/me -> ${after.status})`);
  await victim.close();

  const audit = await api('GET', '/api/v1/audit?action=revoke_sessions&resource_type=user');
  assert.equal(audit.status, 200);
  const events = audit.body.events ?? audit.body.items ?? [];
  assert.ok(events.length >= 1, `expected a revoke_sessions audit event, got ${JSON.stringify(audit.body).slice(0, 300)}`);
  note(`audit revoke_sessions events: ${events.length}`);

  await page.goto('/profile');
  await page.getByRole('heading', { name: 'Sessions' }).waitFor();
  await shot('02-profile-sessions-card');
  await page.getByRole('button', { name: 'Sign out everywhere' }).click();
  await page.waitForURL((u) => u.pathname === '/login', { timeout: 10000 });
  await page.getByText('Signed out everywhere. Sign in again to continue.').waitFor({ timeout: 10000 });
  await shot('03-login-after-self-revoke');
  await aria('03-login-after-self-revoke');
  note('profile action: landed on /login with the success toast still visible');

  const me = await api('GET', '/api/v1/auth/me');
  assert.equal(me.status, 401, `admin ${username} session should be gone, got ${me.status}`);
  note(`admin session revoked (auth/me -> ${me.status})`);
};
