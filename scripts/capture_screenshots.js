// Capture app screenshots with Playwright.
//
// Usage (from the repo root, inside `nix develop`):
//   APP_URL=http://localhost:8195 \
//     SCREENSHOT_USERNAME=screenshot-admin \
//     SCREENSHOT_PASSWORD='ScreenshotDev12345!' \
//     npm run screenshots
//
// See docs/SCREENSHOTS.md for the full runbook (server setup, auth, seeding).
// Auth, seeding, and screenshot helpers live in scripts/lib/capture_helpers.js.
//
// WARNING: seeding writes accounts, pools and discovered resources into the target
// instance. Only point this script at a throwaway dev server.

const { chromium } = require('playwright');
const fs = require('fs');
const {
  baseURL,
  outDir,
  seedEnabled,
  wants,
  login,
  seed,
  dismissUpdateBanner,
  VIEWPORTS,
  SEED_ACCOUNT_NAME,
  makeShot,
} = require('./lib/capture_helpers');

// Taller viewport used for the discovery views (see withViewportHeight below).
const RESOURCE_VIEW_HEIGHT = 1200;
const NETWORK_VIEW_HEIGHT = 1700;
// The conflict review panel is the tallest view: list + full evidence/review sidebar.
const CONFLICT_VIEW_HEIGHT = 2300;

// ---------------------------------------------------------------------------
// Captures
// ---------------------------------------------------------------------------

async function captureLegacy(page, shot) {
  await page.goto(`${baseURL}/pools`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('h1:has-text("Address Pools")', { timeout: 15000 });
  await dismissUpdateBanner(page);
  await shot('pools.png', page);

  await page.goto(`${baseURL}/blocks`, { waitUntil: 'domcontentloaded' });
  // Sidebar.tsx also renders an h1 ("CloudPAM"), so a bare `h1` matches before
  // this page. BlocksPage.tsx heading is "Allocated Blocks".
  await page.waitForSelector('h1:has-text("Allocated Blocks")', { timeout: 15000 });
  await dismissUpdateBanner(page);
  await shot('blocks.png', page);

  await page.goto(`${baseURL}/accounts`, { waitUntil: 'domcontentloaded' });
  // AccountsPage.tsx heading is "Cloud Accounts".
  await page.waitForSelector('h1:has-text("Cloud Accounts")', { timeout: 15000 });
  await dismissUpdateBanner(page);
  await shot('accounts.png', page);

  // visualization.png, bulk-actions-pools.png, and analytics.png are omitted.
  // navigation.ts has no Analytics route; those three are not part of the curated set.
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
