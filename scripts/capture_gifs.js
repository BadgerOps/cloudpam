// Record short product-demo GIFs with Playwright and encode them with host ffmpeg.
//
// Usage (from the repo root, inside `nix develop`, against a throwaway server):
//   APP_URL=http://localhost:8196 \
//     SCREENSHOT_USERNAME=screenshot-admin \
//     SCREENSHOT_PASSWORD='ScreenshotDev12345!' \
//     npm run gifs
//
// `just gifs` builds the UI, starts that server, and runs this script.
// See docs/SCREENSHOTS.md. Auth and seeding live in scripts/lib/capture_helpers.js.
//
// ffmpeg is a host requirement (not part of the Nix dev shell). Set KEEP_GIF_RAW=1
// to retain the intermediate WebM files under photos/gifs/raw/.
//
// WARNING: seeding writes accounts, pools and discovered resources into the target
// instance. Only point this script at a throwaway dev server.

const { chromium } = require('playwright');
const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const {
  baseURL,
  username,
  password,
  login,
  seed,
  dismissUpdateBanner,
  SEED_ACCOUNT_NAME,
} = require('./lib/capture_helpers');

const gifDir = path.join(process.cwd(), 'photos', 'gifs');
const rawDir = path.join(gifDir, 'raw');
const VIEW = { width: 1280, height: 720 };
const TYPE_DELAY_MS = 18;

// Clean discovered rows. Their parent VPC is selected with the subnet, so the
// preview stays importable. Conflict fixtures (exact-pool, missing parent, bad
// nesting, duplicate CIDR) are left unchecked.
const IMPORT_NAMES = ['prod-west-vpc', 'prod-west-app-a'];

function runFfmpeg(args) {
  const result = spawnSync('ffmpeg', args, { stdio: 'inherit' });
  if (result.error) {
    throw new Error(`ffmpeg not found on PATH (${result.error.message}). Install it on the host; do not add it to the Nix flake.`);
  }
  if (result.status !== 0) {
    throw new Error(`ffmpeg exited ${result.status}`);
  }
}

function webmToGif(webmPath, gifPath) {
  const palette = `${webmPath}.png`;
  const vf = 'fps=12,scale=1280:-1:flags=lanczos';
  // ffmpeg 9's image2 muxer warns unless a single-image output sets -update.
  runFfmpeg(['-y', '-i', webmPath, '-vf', `${vf},palettegen=stats_mode=full`, '-update', '1', '-frames:v', '1', palette]);
  runFfmpeg(['-y', '-i', webmPath, '-i', palette, '-lavfi', `${vf}[x];[x][1:v]paletteuse=dither=bayer`, gifPath]);
  fs.unlinkSync(palette);
}

function logDuration(gifPath) {
  const probe = spawnSync(
    'ffprobe',
    ['-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', gifPath],
    { encoding: 'utf8' },
  );
  if (probe.status === 0) {
    const seconds = Number(probe.stdout.trim());
    console.log(`Duration ${path.basename(gifPath)}: ${seconds.toFixed(2)}s`);
    if (seconds < 8 || seconds > 12) {
      console.warn(`Duration outside the 8–12s target: ${path.basename(gifPath)}`);
    }
  }
}

async function typeInto(locator, value) {
  await locator.click();
  await locator.fill('');
  await locator.pressSequentially(value, { delay: TYPE_DELAY_MS });
}

async function hold(page, ms) {
  await page.waitForTimeout(ms);
}

async function recordScenario(browser, name, drive, storageState) {
  const context = await browser.newContext({
    ...(storageState ? { storageState } : {}),
    viewport: VIEW,
    recordVideo: { dir: rawDir, size: VIEW },
  });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  page.setDefaultNavigationTimeout(20000);
  const video = page.video();
  if (!video) throw new Error('Playwright did not attach a video to the page');
  const webmPath = await video.path();
  try {
    await drive(page);
    await page.close();
    await context.close();
  } catch (err) {
    await page.close().catch(() => {});
    await context.close().catch(() => {});
    console.error(`Kept failed recording at ${webmPath}`);
    throw err;
  }

  const gifPath = path.join(gifDir, `${name}.gif`);
  webmToGif(webmPath, gifPath);
  if (process.env.KEEP_GIF_RAW === '1') {
    console.log('Kept raw', webmPath);
  } else {
    fs.unlinkSync(webmPath);
  }
  console.log('Saved', gifPath);
  logDuration(gifPath);
}

async function setupLogin(page) {
  await page.goto(`${baseURL}/setup`, { waitUntil: 'domcontentloaded' });
  const create = page.getByRole('button', { name: 'Create Admin Account' });
  await create.waitFor({ state: 'visible' });
  await hold(page, 900);

  const form = page.locator('form');
  await typeInto(form.locator('input[type="text"]'), username);
  await typeInto(form.locator('input[type="email"]'), `${username}@localhost`);
  const passwords = form.locator('input[type="password"]');
  await typeInto(passwords.nth(0), password);
  await typeInto(passwords.nth(1), password);
  await hold(page, 700);
  await create.click();

  const signIn = page.getByRole('button', { name: 'Sign In' });
  await signIn.waitFor({ state: 'visible' });
  await hold(page, 600);
  const loginForm = page.locator('form');
  await typeInto(loginForm.locator('input[type="text"]'), username);
  await typeInto(loginForm.locator('input[type="password"]'), password);
  await hold(page, 900);
  await signIn.click();

  await page.getByText('Total Pools').waitFor({ state: 'visible' });
  await hold(page, 1400);

  await page.goto(`${baseURL}/pools`, { waitUntil: 'domcontentloaded' });
  await page.locator('h1:has-text("Address Pools")').waitFor({ state: 'visible' });
  // The release check can land during this first authenticated page. Click it
  // away when it is already up; don't burn the whole 8s helper wait on camera.
  const dismiss = page.locator('button:has-text("Dismiss")').first();
  try {
    await dismiss.waitFor({ state: 'visible', timeout: 2000 });
    await dismiss.click();
  } catch {
    // Banner did not arrive before the closing hold.
  }
  await hold(page, 1400);
}

// Login, seed, and dismiss the release banner off camera. Dismissal is stored in
// localStorage, so later recording contexts reuse storageState and do not spend
// the banner's network wait inside the GIF.
async function prepareDemoData(browser) {
  const context = await browser.newContext({ viewport: VIEW });
  try {
    if (!(await login(context))) throw new Error('API login failed after the setup GIF');
    if (!(await seed(context))) throw new Error('demo seed failed');
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    await page.goto(`${baseURL}/pools`, { waitUntil: 'domcontentloaded' });
    await page.locator('h1:has-text("Address Pools")').waitFor({ state: 'visible' });
    await dismissUpdateBanner(page);
    const dismissed = await page.evaluate(() => localStorage.getItem('cloudpam_dismissed_update_version'));
    if (!dismissed) {
      const dismiss = page.locator('button:has-text("Dismiss")').first();
      try {
        await dismiss.waitFor({ state: 'visible', timeout: 5000 });
        await dismiss.click();
      } catch {
        console.warn('Update banner did not appear; a later GIF may include it if the version check is slow.');
      }
    }
    const storageState = await context.storageState();
    await page.close();
    return storageState;
  } finally {
    await context.close();
  }
}

async function chooseParent(page) {
  const parent = page.locator('#create-pool-parent');
  await parent.locator('option').nth(1).waitFor({ state: 'attached' });
  const labels = (await parent.locator('option').allTextContents()).map((text) => text.trim());
  const label = labels.find((text) => text.startsWith('Prod East VPC'))
    || labels.find((text) => text.startsWith('Corp Supernet'));
  if (!label) throw new Error(`parent pool missing from select: ${labels.join(' | ')}`);
  await parent.selectOption({ label });
  return label;
}

async function createPool(page) {
  await page.goto(`${baseURL}/pools`, { waitUntil: 'domcontentloaded' });
  await page.locator('h1:has-text("Address Pools")').waitFor({ state: 'visible' });
  await hold(page, 2200);

  await page.getByRole('button', { name: 'New Pool' }).click();
  const name = page.locator('#create-pool-name');
  await name.waitFor({ state: 'visible' });
  await typeInto(name, 'Demo App Tier');
  await typeInto(page.locator('#create-pool-cidr'), '10.20.10.0/24');
  const parentLabel = await chooseParent(page);
  console.log('Selected parent', parentLabel);
  await hold(page, 1400);

  await page.getByRole('button', { name: 'Create Pool' }).click();
  await page.getByText('Pool created').waitFor({ state: 'visible' });
  await hold(page, 1100);

  // The table lists root pools until a search is active, so a child of Prod East
  // stays hidden until we search for it.
  const search = page.getByPlaceholder('Search pools by name, CIDR, or description...');
  await typeInto(search, 'Demo App Tier');
  await page.locator('td', { hasText: 'Demo App Tier' }).first().waitFor({ state: 'visible' });
  await hold(page, 2800);
}

async function discoveryImport(page) {
  await page.goto(`${baseURL}/discovery`, { waitUntil: 'domcontentloaded' });
  await page.locator('h1:has-text("Cloud Discovery")').waitFor({ state: 'visible' });
  await hold(page, 800);

  const accountSelect = page.locator('select').first();
  await accountSelect.locator('option', { hasText: SEED_ACCOUNT_NAME }).first().waitFor({ state: 'attached' });
  const options = (await accountSelect.locator('option').allTextContents()).map((text) => text.trim());
  const match = options.find((label) => label.startsWith(SEED_ACCOUNT_NAME));
  if (!match) throw new Error(`account ${SEED_ACCOUNT_NAME} missing from discovery select`);
  await accountSelect.selectOption({ label: match });
  await hold(page, 900);

  let selected = 0;
  for (const resourceName of IMPORT_NAMES) {
    const box = page.getByRole('checkbox', { name: `Select ${resourceName}`, exact: true });
    try {
      await box.first().waitFor({ state: 'visible', timeout: selected === 0 ? 15000 : 3000 });
    } catch {
      console.warn(`Discovery: row not visible: ${resourceName}`);
      continue;
    }
    await box.first().check();
    selected += 1;
    await hold(page, 450);
  }
  if (selected < 2) throw new Error(`only selected ${selected} discovery rows`);
  await page.getByText(`${selected} selected`).waitFor({ state: 'visible' });
  await hold(page, 1100);

  await page.getByRole('button', { name: 'Preview Import' }).click();
  await page.locator('h2:text-is("Import preview")').waitFor({ state: 'visible' });
  await hold(page, 2200);

  // ImportPreviewModal labels onApply "Apply import". click() waits until the
  // button is enabled, which requires at least one importable row.
  await page.getByRole('button', { name: 'Apply import' }).click();
  const summary = page.locator('h2:has-text("Import apply summary")');
  await summary.waitFor({ state: 'visible' });
  await summary.scrollIntoViewIfNeeded();
  await hold(page, 2800);
}

async function main() {
  if (!username || !password) {
    throw new Error('Set SCREENSHOT_USERNAME and SCREENSHOT_PASSWORD (password must meet the 12-character policy).');
  }
  fs.mkdirSync(rawDir, { recursive: true });

  const browser = await chromium.launch();
  try {
    await recordScenario(browser, 'setup-login', setupLogin);
    const storageState = await prepareDemoData(browser);
    await recordScenario(browser, 'create-pool', createPool, storageState);
    await recordScenario(browser, 'discovery-import', discoveryImport, storageState);
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
