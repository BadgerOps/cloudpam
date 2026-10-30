// journey.cjs: the Playwright runner behind `cloudpam-verify browser`.
//
// A journey is a CommonJS module exporting an async function:
//   module.exports = async ({ page, context, baseURL, username, password, login, api, shot, aria, note, evidenceDir }) => { ... }
//   login()            drive the real /login form and wait for the app shell (the Main navigation landmark)
//   api(method, path, body?) authenticated request through the browser context (session cookie + CSRF); returns { status, body }
//   shot(name)         full-page screenshot -> evidence/<label>/<name>.png
//   aria(name)         ARIA snapshot of <body> -> evidence/<label>/<name>.aria.txt
//   note(text)         append a line to evidence/<label>/journey.log
// Any thrown error fails the journey (exit 1) and leaves evidence/<label>/failure.png.
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');

async function main() {
  const journeyPath = process.argv[2];
  if (!journeyPath) throw new Error('usage: node journey.cjs JOURNEY.cjs');
  const env = {};
  for (const name of ['BASE_URL', 'EVIDENCE', 'APP_VERSION', 'USERNAME', 'PASSWORD']) {
    env[name] = process.env[`CLOUDPAM_VERIFY_${name}`];
    if (!env[name]) throw new Error(`CLOUDPAM_VERIFY_${name} is not set; run this through cloudpam-verify browser`);
  }
  const label = process.env.CLOUDPAM_VERIFY_LABEL || path.basename(journeyPath, '.cjs');
  // The label names a directory under the evidence directory; one safe path component, never a path.
  if (label === '.' || label === '..' || !/^[A-Za-z0-9._-]+$/.test(label)) throw new Error(`refusing label '${label}': it must match ^[A-Za-z0-9._-]+$ and not be . or ..`);
  const baseURL = env.BASE_URL;
  const evidenceDir = path.join(env.EVIDENCE, label);
  fs.mkdirSync(evidenceDir, { recursive: true });
  const logFile = path.join(evidenceDir, 'journey.log');
  fs.writeFileSync(logFile, `journey: ${journeyPath}\nbase_url: ${baseURL}\napp_version: ${env.APP_VERSION}\nstarted: ${new Date().toISOString()}\n`);
  const note = (text) => { const line = `${new Date().toISOString()} ${text}`; fs.appendFileSync(logFile, line + '\n'); console.log(line); };

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ baseURL, viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  page.on('dialog', async (d) => { note(`dialog "${d.message()}" -> accept`); await d.accept(); });
  page.on('pageerror', (e) => note(`pageerror ${e.message}`));

  const shot = async (name) => { const file = path.join(evidenceDir, `${name}.png`); await page.screenshot({ path: file, fullPage: true }); note(`screenshot ${file}`); return file; };
  const aria = async (name) => { const file = path.join(evidenceDir, `${name}.aria.txt`); fs.writeFileSync(file, await page.locator('body').ariaSnapshot()); note(`aria ${file}`); return file; };

  const login = async () => {
    const nav = page.getByRole('navigation', { name: 'Main navigation' });
    for (let attempt = 1; ; attempt++) {
      await page.goto('/login', { waitUntil: 'domcontentloaded' });
      // The login inputs have no label association; placeholder + autocomplete are the stable handles.
      await page.getByPlaceholder('admin').fill(env.USERNAME);
      await page.locator('input[autocomplete="current-password"]').fill(env.PASSWORD);
      await page.getByRole('button', { name: 'Sign In' }).click();
      const limited = page.getByText('too many login attempts');
      await nav.or(limited).first().waitFor({ timeout: 15000 });
      if (await nav.isVisible()) break;
      if (attempt > 1) throw new Error('login still rate limited after waiting 60s');
      // The server allows 5 logins per minute per IP (one more every 12s), whatever RATE_LIMIT_RPS says.
      note('login rate limited (429 too many login attempts); waiting 60s, the Retry-After');
      await page.waitForTimeout(60000);
    }
    note(`logged in as ${env.USERNAME} at ${page.url()}`);
  };

  const api = async (method, apiPath, body) => {
    const headers = { Accept: 'application/json' };
    if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
      let csrf = (await context.cookies(baseURL)).find((c) => c.name === 'csrf_token');
      if (!csrf) { await context.request.get('/api/v1/pools'); csrf = (await context.cookies(baseURL)).find((c) => c.name === 'csrf_token'); }
      headers['X-CSRF-Token'] = csrf ? csrf.value : '';
      headers['Content-Type'] = 'application/json';
    }
    const res = await context.request.fetch(apiPath, { method, headers, data: body === undefined ? undefined : JSON.stringify(body) });
    const text = await res.text();
    let parsed; try { parsed = JSON.parse(text); } catch { parsed = text; }
    note(`api ${method} ${apiPath} -> ${res.status()}`);
    return { status: res.status(), body: parsed };
  };

  const journey = require(path.resolve(journeyPath));
  try {
    await journey({ page, context, baseURL, username: env.USERNAME, password: env.PASSWORD, login, api, shot, aria, note, evidenceDir });
    note('RESULT: PASS');
  } catch (err) {
    note(`RESULT: FAIL ${err.stack || err}`);
    try { await page.screenshot({ path: path.join(evidenceDir, 'failure.png'), fullPage: true }); } catch {}
    process.exitCode = 1;
  } finally {
    await browser.close();
  }
}

main().catch((e) => { console.error(e); process.exit(1); });
