// Uses two of the five per-IP login tokens: the wrong password and login().
const assert = require('node:assert/strict');

module.exports = async ({ page, username, login, api, shot, aria, note }) => {
  await page.goto('/setup');
  await page.waitForURL((u) => u.pathname === '/login');
  await page.getByRole('heading', { name: 'CloudPAM', exact: true }).waitFor();
  const signIn = page.getByRole('button', { name: 'Sign In' });
  assert.ok(await signIn.isDisabled(), 'Sign In disabled while both fields are empty');
  await page.getByPlaceholder('admin').fill(username);
  assert.ok(await signIn.isDisabled(), 'Sign In disabled while the password is empty');
  await page.locator('input[autocomplete="current-password"]').fill('wrong-password-123');
  assert.ok(await signIn.isEnabled(), 'Sign In enabled once both fields have text');
  await signIn.click();
  await page.getByText('invalid credentials').waitFor({ timeout: 10000 });
  assert.equal(new URL(page.url()).pathname, '/login', 'still on /login after a wrong password');
  await shot('01-rejected');

  await login();
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  for (const name of ['Dashboard', 'Address Pools', 'Cloud Accounts']) await nav.getByRole('link', { name }).waitFor();
  await shot('02-signed-in');
  await aria('02-signed-in');

  const me = await api('GET', '/api/v1/auth/me');
  assert.equal(me.status, 200);
  assert.equal(me.body.user.username, username, 'auth/me names the signed-in user');
  note(`auth/me auth_type=${me.body.auth_type} user=${me.body.user.username}`);

  await page.goto('/login');
  await page.waitForURL((u) => u.pathname === '/');
  note('signed-in visit to /login lands on /');
};
