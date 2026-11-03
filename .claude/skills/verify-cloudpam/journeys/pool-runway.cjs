const assert = require('node:assert/strict');

// Precondition: pool 2 (Prod East VPC) has utilization snapshots older than 7 days
// and at least one child allocation, so the fit reports a runway.
module.exports = async ({ page, login, api, shot, aria, note }) => {
  await login();

  const stats = await api('GET', '/api/v1/pools/2/stats');
  assert.equal(stats.status, 200);
  const projection = stats.body.projection;
  assert.ok(projection, 'GET /pools/2/stats carries a projection');
  note(`projection via API: ${JSON.stringify(projection)}`);
  assert.equal(projection.status, 'ok');
  assert.ok(projection.runway_days > 0);
  assert.equal(projection.forecast.length, 3);

  await page.getByRole('link', { name: 'Address Pools' }).click();
  await page.getByRole('heading', { name: 'Address Pools' }).waitFor();
  await page.getByPlaceholder('Search pools by name, CIDR, or description...').fill('Prod East');
  await page.getByRole('cell', { name: 'Prod East VPC', exact: true }).waitFor();
  await page.getByTitle('View details').first().click();
  await page.getByRole('heading', { level: 3, name: 'Prod East VPC' }).waitFor();

  const runway = page.getByTestId('runway');
  await runway.waitFor({ timeout: 10000 });
  const runwayText = await runway.textContent();
  note(`panel runway: ${runwayText}`);
  assert.match(runwayText, /\d+ (days|months|years)/);
  const forecast = page.getByTestId('growth-forecast');
  await forecast.waitFor();
  const forecastText = await forecast.textContent();
  note(`panel forecast: ${forecastText}`);
  assert.ok(forecastText.includes('3 mo') && forecastText.includes('12 mo'));

  await shot('01-pool-detail-runway');
  await aria('01-pool-detail-runway');
};
