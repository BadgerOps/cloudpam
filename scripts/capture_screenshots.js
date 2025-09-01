// Capture app screenshots with Playwright.
// Usage:
//   cd code/cloudpam
//   npm install
//   npx playwright install chromium
//   APP_URL=http://localhost:8080 npm run screenshots

const { chromium } = require('playwright');
const path = require('path');
const fs = require('fs');

(async () => {
  const baseURL = process.env.APP_URL || 'http://localhost:8080';
  const outDir = path.join(process.cwd(), 'photos');
  fs.mkdirSync(outDir, { recursive: true });

  const browser = await chromium.launch();
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 2 });
  const page = await context.newPage();

  const safeShot = async (name, locatorOrPage) => {
    const file = path.join(outDir, name);
    try {
      if (locatorOrPage?.screenshot) {
        await locatorOrPage.screenshot({ path: file, fullPage: locatorOrPage === page });
      } else {
        await page.screenshot({ path: file, fullPage: true });
      }
      console.log('Saved', file);
    } catch (e) { console.warn('Failed to save', file, e.message); }
  };

  // Pools overview
  await page.goto(baseURL, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('text=Top-level Pools', { timeout: 5000 }).catch(() => {});
  await safeShot('pools.png', page);

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
  await safeShot('blocks.png', page);

  // IP Space visualization (capture the card if present)
  const vizCard = page.locator('xpath=//strong[text()="IP Space Visualization"]/ancestor::*[contains(@class,"card")]').first();
  if (await vizCard.count()) {
    await safeShot('visualization.png', vizCard);
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
      await safeShot('bulk-actions-pools.png', page);
    }
  }

  // Accounts page
  await page.locator('button:has-text("Accounts")').click().catch(() => {});
  await page.waitForSelector('text=Accounts', { timeout: 4000 }).catch(() => {});
  await safeShot('accounts.png', page);

  // Analytics page
  await page.locator('button:has-text("Analytics")').click().catch(() => {});
  await page.waitForSelector('text=All Assigned Blocks', { timeout: 4000 }).catch(() => {});
  await safeShot('analytics.png', page);

  await browser.close();
})();

