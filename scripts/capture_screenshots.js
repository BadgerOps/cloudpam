// Capture app screenshots with Playwright.
//
// Usage:
//   cd code/cloudpam
//   npm install
//   npx playwright install chromium
//   APP_URL=http://localhost:8080 npm run screenshots
//
// See docs/SCREENSHOTS.md for the full runbook (server setup, auth, seeding).
//
// Environment:
//   APP_URL              base URL of a running CloudPAM instance (default http://localhost:8080)
//   SCREENSHOT_USERNAME  login username (falls back to CLOUDPAM_ADMIN_USERNAME)
//   SCREENSHOT_PASSWORD  login password (falls back to CLOUDPAM_ADMIN_PASSWORD)
//   SCREENSHOT_SEED      "0" disables demo-data seeding (default: seed enabled)
//   SCREENSHOT_ONLY      comma-separated capture group filter: "legacy,discovery"
//
// WARNING: seeding writes accounts, pools and discovered resources into the target
// instance. Only point this script at a throwaway dev server.

const { chromium } = require('playwright');
const path = require('path');
const fs = require('fs');

const baseURL = process.env.APP_URL || 'http://localhost:8080';
const outDir = path.join(process.cwd(), 'photos');

const username = process.env.SCREENSHOT_USERNAME || process.env.CLOUDPAM_ADMIN_USERNAME || '';
const password = process.env.SCREENSHOT_PASSWORD || process.env.CLOUDPAM_ADMIN_PASSWORD || '';
const seedEnabled = process.env.SCREENSHOT_SEED !== '0';

const onlyGroups = (process.env.SCREENSHOT_ONLY || '')
  .split(',')
  .map((s) => s.trim())
  .filter(Boolean);

// Viewports to capture. Adding a viewport is a one-line change: append an entry and
// every capture below is re-run against it, with `suffix` appended to each filename.
//
// Only desktop is captured today. The app has no responsive layout yet (Layout.tsx and
// Header.tsx carry no breakpoint classes and there is no mobile nav), so a narrow capture
// would document a broken layout rather than a reviewable one. Responsive work is tracked
// in issue #83; add the mobile entry below once that lands.
const VIEWPORTS = [
  { name: 'desktop', width: 1280, height: 900, suffix: '' },
  // { name: 'mobile', width: 390, height: 844, suffix: '-mobile' },  // blocked on #83
];

// Taller viewport used for the discovery views (see withViewportHeight below).
const RESOURCE_VIEW_HEIGHT = 1200;
const NETWORK_VIEW_HEIGHT = 1700;
// The conflict review panel is the tallest view: list + full evidence/review sidebar.
const CONFLICT_VIEW_HEIGHT = 2300;

function wants(group) {
  return onlyGroups.length === 0 || onlyGroups.includes(group);
}

/**
 * Screenshot helper. Accepts a Page (captures full page) or a Locator (captures the
 * element). Never throws: a failed capture logs and lets the rest of the run continue.
 */
function makeShot(page, viewport) {
  return async function shot(name, locatorOrPage) {
    const ext = path.extname(name) || '.png';
    const stem = name.slice(0, name.length - ext.length);
    const file = path.join(outDir, `${stem}${viewport.suffix}${ext}`);
    const target = locatorOrPage || page;
    try {
      if (target === page) {
        await page.screenshot({ path: file, fullPage: true });
      } else {
        await target.screenshot({ path: file });
      }
      console.log('Saved', file);
    } catch (e) {
      console.warn('Failed to save', file, e.message);
    }
  };
}

// ---------------------------------------------------------------------------
// Auth + seeding
// ---------------------------------------------------------------------------

/**
 * Runs first-boot setup when the server reports `needs_setup`. A fresh dev server
 * always does: bootstrapping an admin via CLOUDPAM_ADMIN_USERNAME creates the user
 * but leaves `needs_setup` true, so the SPA keeps redirecting to /setup. Creating the
 * admin through this endpoint is what actually clears the flag.
 */
async function setupIfNeeded(context) {
  const res = await context.request.get(`${baseURL}/healthz`);
  if (!res.ok()) return;
  const health = await res.json();
  if (health.needs_setup !== true) return;

  const created = await context.request.post(`${baseURL}/api/v1/auth/setup`, {
    data: { username, password, email: `${username}@localhost` },
  });
  if (created.ok()) {
    console.log('Created first-boot admin account');
  } else {
    console.warn(`Setup failed (${created.status()}): ${await created.text()}`);
  }
}

async function login(context) {
  if (!username || !password) {
    console.warn('No SCREENSHOT_USERNAME/SCREENSHOT_PASSWORD set; continuing unauthenticated.');
    return false;
  }
  await setupIfNeeded(context);
  const res = await context.request.post(`${baseURL}/api/v1/auth/login`, {
    data: { username, password },
  });
  if (!res.ok()) {
    console.warn(`Login failed (${res.status()}); continuing unauthenticated.`);
    return false;
  }
  console.log('Logged in for screenshot capture');
  return true;
}

/** Reads the double-submit CSRF cookie the server sets on any safe request. */
async function csrfToken(context) {
  await context.request.get(`${baseURL}/api/v1/pools`);
  const cookies = await context.cookies(baseURL);
  const cookie = cookies.find((c) => c.name === 'csrf_token');
  return cookie ? cookie.value : '';
}

const SEED_POOLS = [
  { name: 'Corp Supernet', cidr: '10.0.0.0/8' },
  { name: 'Prod East VPC', cidr: '10.20.0.0/16', parent: 'Corp Supernet' },
  { name: 'Edge Transit', cidr: '10.40.0.0/17', parent: 'Corp Supernet' },
];

const SEED_ACCOUNT_NAME = 'prod-platform';

/**
 * Builds the org-ingest payload. `last_seen_at` is deliberately in the future: the
 * server marks anything last seen before the ingest timestamp as stale, and stale
 * resources are not selectable for import.
 */
function seedIngestPayload() {
  const now = new Date().toISOString();
  const fresh = new Date(Date.now() + 60_000).toISOString();
  const res = (o) => ({ provider: 'aws', status: 'active', discovered_at: now, last_seen_at: fresh, ...o });

  return {
    accounts: [
      {
        aws_account_id: '111111111111',
        account_name: SEED_ACCOUNT_NAME,
        provider: 'aws',
        regions: ['us-east-1', 'us-west-2'],
        resources: [
          // Exactly matches the "Prod East VPC" pool -> unlinked_exact_pool conflict.
          res({ region: 'us-east-1', resource_type: 'vpc', resource_id: 'vpc-0a1prod', name: 'prod-east-vpc', cidr: '10.20.0.0/16' }),
          res({ region: 'us-east-1', resource_type: 'subnet', resource_id: 'subnet-0a1', name: 'prod-east-app-a', cidr: '10.20.1.0/24', parent_resource_id: 'vpc-0a1prod' }),
          res({ region: 'us-east-1', resource_type: 'subnet', resource_id: 'subnet-0a2', name: 'prod-east-app-b', cidr: '10.20.2.0/24', parent_resource_id: 'vpc-0a1prod' }),
          // Clean importable VPC + subnet.
          res({ region: 'us-west-2', resource_type: 'vpc', resource_id: 'vpc-0b1prod', name: 'prod-west-vpc', cidr: '10.21.0.0/16' }),
          res({ region: 'us-west-2', resource_type: 'subnet', resource_id: 'subnet-0b1', name: 'prod-west-app-a', cidr: '10.21.1.0/24', parent_resource_id: 'vpc-0b1prod' }),
          // Parent was never discovered -> missing_parent conflict.
          res({ region: 'us-west-2', resource_type: 'subnet', resource_id: 'subnet-0b9', name: 'orphan-analytics', cidr: '10.99.5.0/24', parent_resource_id: 'vpc-deleted-999' }),
          // Not contained by its parent VPC -> invalid_nesting conflict.
          res({ region: 'us-west-2', resource_type: 'subnet', resource_id: 'subnet-0b8', name: 'misnested-dmz', cidr: '172.16.5.0/24', parent_resource_id: 'vpc-0b1prod' }),
          // Exactly matches the "Edge Transit" pool -> unlinked_exact_pool conflict.
          res({ region: 'us-east-1', resource_type: 'vpc', resource_id: 'vpc-0c1edge', name: 'edge-transit-vpc', cidr: '10.40.64.0/17' }),
          // Same CIDR twice in one account -> duplicate_cidr conflict under account policy.
          res({ region: 'us-east-1', resource_type: 'vpc', resource_id: 'vpc-0d1shared', name: 'shared-services-a', cidr: '192.168.10.0/24' }),
          res({ region: 'us-west-2', resource_type: 'vpc', resource_id: 'vpc-0d2shared', name: 'shared-services-b', cidr: '192.168.10.0/24' }),
        ],
      },
      {
        aws_account_id: '222222222222',
        account_name: 'sandbox',
        provider: 'aws',
        regions: ['eu-west-1'],
        resources: [
          res({ region: 'eu-west-1', resource_type: 'vpc', resource_id: 'vpc-0e1sbx', name: 'sandbox-vpc', cidr: '10.60.0.0/16' }),
          res({ region: 'eu-west-1', resource_type: 'subnet', resource_id: 'subnet-0e1', name: 'sandbox-app', cidr: '10.60.1.0/24', parent_resource_id: 'vpc-0e1sbx' }),
        ],
      },
    ],
  };
}

/**
 * Seeds managed pools and discovered cloud resources so the discovery views have
 * something real to show. Idempotent: pools already present by name are left alone,
 * and org ingest upserts discovered resources by (account, resource_id).
 */
async function seed(context) {
  const token = await csrfToken(context);
  const headers = { 'X-CSRF-Token': token, 'Content-Type': 'application/json' };

  const poolsRes = await context.request.get(`${baseURL}/api/v1/pools`);
  if (!poolsRes.ok()) {
    console.warn(`Seed skipped: cannot list pools (${poolsRes.status()}).`);
    return false;
  }
  const body = await poolsRes.json();
  const existing = Array.isArray(body) ? body : body.items || [];
  const byName = new Map(existing.map((p) => [p.name, p]));

  for (const spec of SEED_POOLS) {
    if (byName.has(spec.name)) continue;
    const parent = spec.parent ? byName.get(spec.parent) : null;
    const payload = { name: spec.name, cidr: spec.cidr };
    if (parent) payload.parent_id = parent.id;
    const res = await context.request.post(`${baseURL}/api/v1/pools`, { headers, data: payload });
    if (!res.ok()) {
      console.warn(`Seed pool ${spec.name} failed (${res.status()}): ${await res.text()}`);
      continue;
    }
    byName.set(spec.name, await res.json());
  }

  const ingest = await context.request.post(`${baseURL}/api/v1/discovery/ingest/org`, {
    headers,
    data: seedIngestPayload(),
  });
  if (!ingest.ok()) {
    console.warn(`Seed ingest failed (${ingest.status()}): ${await ingest.text()}`);
    return false;
  }
  const summary = await ingest.json();
  console.log(
    `Seeded ${summary.accounts_processed} accounts / ${summary.total_resources} discovered resources`,
  );
  return true;
}

// ---------------------------------------------------------------------------
// Captures
// ---------------------------------------------------------------------------

async function captureLegacy(page, shot) {
  // Pools overview
  await page.goto(baseURL, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('text=Top-level Pools', { timeout: 5000 }).catch(() => {});
  await shot('pools.png', page);

  // Blocks view (select first pool row if available)
  const viewBtn = page.locator('table >> text=View').first();
  if (await viewBtn.count() > 0) {
    await viewBtn.click().catch(() => {});
    const listBlocks = page.locator('button:has-text("List Blocks")');
    if (await listBlocks.count()) {
      await listBlocks.click().catch(() => {});
      await page.waitForSelector('table thead >> text=Prefix', { timeout: 4000 }).catch(() => {});
    }
  }
  await shot('blocks.png', page);

  // IP Space visualization (capture the card if present)
  const vizCard = page.locator('xpath=//strong[text()="IP Space Visualization"]/ancestor::*[contains(@class,"card")]').first();
  if (await vizCard.count()) {
    await shot('visualization.png', vizCard);
  }

  // Bulk actions in Pools (select a couple and open the menu)
  await page.locator('button:has-text("Pools")').click().catch(() => {});
  const firstTwo = page.locator('table tbody tr input[type="checkbox"]').first();
  if (await firstTwo.count()) {
    await firstTwo.check().catch(() => {});
    const second = page.locator('table tbody tr input[type="checkbox"]').nth(1);
    if (await second.count()) await second.check().catch(() => {});
    const bulkBtn = page.locator('section:has-text("Top-level Pools") button:has-text("⋮")').first();
    if (await bulkBtn.count()) {
      await bulkBtn.click().catch(() => {});
      await shot('bulk-actions-pools.png', page);
    }
  }

  // Accounts page
  await page.locator('button:has-text("Accounts")').click().catch(() => {});
  await page.waitForSelector('text=Accounts', { timeout: 4000 }).catch(() => {});
  await shot('accounts.png', page);

  // Analytics page
  await page.locator('button:has-text("Analytics")').click().catch(() => {});
  await page.waitForSelector('text=All Assigned Blocks', { timeout: 4000 }).catch(() => {});
  await shot('analytics.png', page);
}

/**
 * The Discovery page renders inside a `flex-1 overflow-auto` container, so the document
 * itself never scrolls and Playwright's fullPage option yields exactly one viewport.
 * Growing the viewport is the only way to fit the whole view into one capture.
 */
async function withViewportHeight(page, height, fn) {
  const original = page.viewportSize();
  await page.setViewportSize({ width: original.width, height });
  try {
    await fn();
  } finally {
    await page.setViewportSize(original);
  }
}

/** Hides the release-update banner, which depends on an external version check. */
async function dismissUpdateBanner(page) {
  const dismiss = page.locator('button:has-text("Dismiss")').first();
  if (await dismiss.count()) {
    await dismiss.click().catch(() => {});
    await page.waitForTimeout(200);
  }
}

/** Opens /discovery and pins the account selector to the seeded account. */
async function openDiscovery(page) {
  await page.goto(`${baseURL}/discovery`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('h1:has-text("Cloud Discovery")', { timeout: 15000 });
  await dismissUpdateBanner(page);

  const accountSelect = page.locator('select').first();
  const options = await accountSelect.locator('option').allTextContents();
  const match = options.find((label) => label.startsWith(SEED_ACCOUNT_NAME));
  if (match) {
    await accountSelect.selectOption({ label: match });
  }
  await page.waitForTimeout(700);
}

async function captureDiscovery(page, shot) {
  await openDiscovery(page);

  // --- Checkbox multi-select + import preview -------------------------------
  const rowCheckboxes = page.locator('table tbody tr input[type="checkbox"]:not([disabled])');
  const rowCount = await rowCheckboxes.count();
  if (rowCount === 0) {
    console.warn('Discovery: no selectable resource rows; skipping import captures.');
  } else {
    for (let i = 0; i < Math.min(4, rowCount); i++) {
      await rowCheckboxes.nth(i).check().catch(() => {});
    }
    await page.waitForTimeout(200);
    await withViewportHeight(page, RESOURCE_VIEW_HEIGHT, async () => {
      await shot('discovery-import-selection.png', page);
    });

    await page.locator('button:has-text("Preview Import")').click();
    // The modal panel is the scrollable child of the fixed overlay.
    const modal = page
      .locator('h2:text-is("Import preview")')
      .locator('xpath=ancestor::div[contains(@class,"overflow-auto")][1]');
    await modal.waitFor({ state: 'visible', timeout: 15000 });
    await page.waitForTimeout(400);
    await withViewportHeight(page, RESOURCE_VIEW_HEIGHT, async () => {
      await shot('discovery-import-preview.png', modal);
    });

    await page.locator('button:has-text("Cancel")').last().click().catch(() => {});
    await page.waitForTimeout(200);
  }

  // --- Merged network: hierarchy, flat, conflicts ---------------------------
  await page.locator('button:has-text("Merged Network")').click();
  await page.waitForSelector('button:has-text("Hierarchy")', { timeout: 10000 });

  await withViewportHeight(page, NETWORK_VIEW_HEIGHT, async () => {
    await page.locator('button:has-text("Hierarchy")').click();
    await page.waitForTimeout(900);
    await shot('discovery-network-hierarchy.png', page);

    await page.locator('button:has-text("Flat")').click();
    await page.waitForTimeout(900);
    await shot('discovery-network-flat.png', page);
  });

  await withViewportHeight(page, CONFLICT_VIEW_HEIGHT, async () => {
    await page.locator('button:has-text("Conflicts")').first().click();
    await page.waitForTimeout(900);

    // Select the first conflict so the right-hand review panel is populated.
    const conflictRow = page.locator('button:has-text("Discovered CIDR matches managed pool")').first();
    if (await conflictRow.count()) {
      await conflictRow.click().catch(() => {});
      await page.waitForTimeout(600);
    } else {
      console.warn('Discovery: no conflicts listed; capturing empty conflict list.');
    }
    await shot('discovery-network-conflicts.png', page);
  });
}

// ---------------------------------------------------------------------------

async function main() {
  fs.mkdirSync(outDir, { recursive: true });

  const browser = await chromium.launch();
  try {
    let seeded = false;
    for (const viewport of VIEWPORTS) {
      const context = await browser.newContext({
        viewport: { width: viewport.width, height: viewport.height },
        deviceScaleFactor: 2,
      });
      try {
        await login(context);
        if (seedEnabled && !seeded) {
          seeded = await seed(context);
        }
        const page = await context.newPage();
        const shot = makeShot(page, viewport);
        if (wants('legacy')) await captureLegacy(page, shot);
        if (wants('discovery')) await captureDiscovery(page, shot);
      } finally {
        await context.close();
      }
    }
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});
