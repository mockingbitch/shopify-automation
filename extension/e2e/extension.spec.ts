import { chromium, expect, test as base, type BrowserContext, type Page } from '@playwright/test';
import { EXTENSION_DIR, PAIR_CODE, SELLER_ORIGIN } from './constants';
import { FakeApi } from './fake-api';
import { FixtureSite } from './fixture-site';

const api = new FakeApi();
const site = new FixtureSite();

/** A demo recipe for the local fixture pages only. Not a Seller Centre recipe. */
function demoRecipe() {
  return {
    capability: 'shop.banner.upload',
    version: 1,
    market: 'VN',
    start_url: `${SELLER_ORIGIN}/portal/home`,
    allowed_origins: [SELLER_ORIGIN],
    pace_ms: 200,
    inputs: { banner_image: { type: 'files', required: true } },
    steps: [
      { goto: { url: `${SELLER_ORIGIN}/portal/home` } },
      { assert_shop_id: { target: { selector: '[data-testid=shop-id]' }, pattern: 'Shop ID:\\s*(\\d+)' } },
      { click: { role: 'link', name_any: ['Trang trí shop', 'Shop decoration'] } },
      { wait_for: { role: 'button', name_any: ['Chỉnh sửa', 'Edit'] }, timeout_ms: 10000 },
      { click: { role: 'button', name_any: ['Chỉnh sửa', 'Edit'] } },
      { upload: { target: { selector: 'input[type=file][accept*=image]' }, files: '{{input.banner_image}}' } },
      { click: { role: 'button', name_any: ['Lưu', 'Save'] } },
      { expect: { text_any: ['Đã lưu', 'Saved'] }, timeout_ms: 10000 },
    ],
    verify: [{ expect: { target: { role: 'img', name_any: ['Shop banner'] } } }],
    stop_on: [{ url_matches: '/login|/verify|/captcha' }, { text_any: ['Verify your identity'] }],
  };
}

function addTask(id: string, opts: { tamper?: boolean; shopeeShopId?: string } = {}) {
  api.addTask({
    id,
    capability: 'shop.banner.upload',
    title: 'Change shop banner',
    shop: { id: 'shop-1', shopee_shop_id: opts.shopeeShopId ?? '123456', name: 'Acme VN', region: 'VN' },
    preview: { summary: 'New summer banner', image_url: 'https://cdn.example.test/banner.png' },
    recipeJson: JSON.stringify(demoRecipe()),
    tamper: opts.tamper,
  });
}

const test = base.extend<{ context: BrowserContext; extensionId: string; popup: Page; seller: Page }>({
  // eslint-disable-next-line no-empty-pattern
  context: async ({}, use) => {
    const context = await chromium.launchPersistentContext('', {
      channel: 'chromium',
      headless: !process.env.HEADED,
      args: [`--disable-extensions-except=${EXTENSION_DIR}`, `--load-extension=${EXTENSION_DIR}`],
    });
    await use(context);
    await context.close();
  },
  extensionId: async ({ context }, use) => {
    let [sw] = context.serviceWorkers();
    sw ??= await context.waitForEvent('serviceworker');
    await use(new URL(sw.url()).host);
  },
  seller: async ({ context }, use) => {
    const page = context.pages()[0] ?? (await context.newPage());
    await use(page);
  },
  popup: async ({ context, extensionId }, use) => {
    const page = await context.newPage();
    await page.goto(`chrome-extension://${extensionId}/popup.html`);
    await use(page);
  },
});

test.beforeAll(async () => {
  await api.start();
  await site.start();
});
test.afterAll(async () => {
  await api.stop();
  await site.stop();
});
test.beforeEach(() => {
  api.reset();
  site.reset();
});

async function pair(popup: Page) {
  await popup.getByLabel('Mã ghép nối').fill(PAIR_CODE.toLowerCase().replace('-', ''));
  await popup.getByRole('button', { name: 'Kết nối' }).click();
  await expect(popup.getByTestId('org-name')).toHaveText('Acme Fashion');
  await expect(popup.getByTestId('user-email')).toHaveText('seller@example.test');
}

/** Clicks Run in the popup while the seller's tab is the active tab (as with a real toolbar popup). */
async function clickRun(popup: Page, seller: Page) {
  await seller.bringToFront();
  await popup.getByRole('button', { name: 'Chạy Change shop banner' }).click();
}

const lastResult = () => api.results.at(-1)?.body;

test('pairs, lists tasks grouped by shop and unpairs', async ({ popup }) => {
  addTask('task-1');
  // Wrong code first.
  await popup.getByLabel('Mã ghép nối').fill('ZZZZZ-ZZZZZ');
  await popup.getByRole('button', { name: 'Kết nối' }).click();
  await expect(popup.getByTestId('error')).toContainText('sai, đã hết hạn hoặc đã được dùng');
  await pair(popup);
  const group = popup.getByTestId('shop-group');
  await expect(group).toHaveCount(1);
  await expect(group).toContainText('Acme VN');
  await expect(group).toContainText('Shop ID 123456');
  await expect(group.getByTestId('task')).toContainText('Change shop banner');
  await expect(group.getByTestId('task')).toContainText('shop.banner.upload');
  await expect(group.getByTestId('task-preview')).toContainText('New summer banner');
  await popup.getByRole('button', { name: 'Hủy ghép nối' }).click();
  await expect(popup.getByTestId('pairing')).toBeVisible();
  expect(api.tokens.size).toBe(0);
});

test('a revoked token returns to the pairing screen', async ({ popup }) => {
  await pair(popup);
  api.tokens.clear();
  await popup.getByRole('button', { name: 'Làm mới' }).click();
  await expect(popup.getByTestId('pairing')).toBeVisible();
});

test('runs a two-page recipe with a file upload and reports success', async ({ popup, seller }) => {
  addTask('task-ok');
  await seller.goto('about:blank');
  await pair(popup);
  await clickRun(popup, seller);
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'succeeded', { timeout: 30_000 });

  // The run opened start_url in the same tab, followed the link, uploaded and saved.
  expect(seller.url()).toBe(`${SELLER_ORIGIN}/portal/decoration`);
  await expect(seller.getByRole('status')).toHaveText('Saved');
  await expect(seller.getByRole('img', { name: 'Shop banner' })).toBeVisible();
  expect(site.saved).toEqual([{ name: 'banner.png', size: 70, type: 'image/png' }]);
  expect(site.hits).toEqual(['/portal/home', '/portal/home', '/portal/decoration']);

  const r = lastResult()!;
  expect(r.status).toBe('SUCCEEDED');
  expect((r.steps as { outcome: string }[]).map((s) => s.outcome)).toEqual(Array(9).fill('ok'));
  expect(api.heartbeats.map((h) => h.state)).toContain('RUNNING');
  // Only one tab was ever used.
  expect(seller.context().pages()).toHaveLength(2);
});

test('shop ID mismatch stops before any write', async ({ popup, seller }) => {
  site.shopId = '999999';
  addTask('task-mismatch');
  await seller.goto(`${SELLER_ORIGIN}/portal/home`);
  await pair(popup);
  await clickRun(popup, seller);
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'failed', { timeout: 30_000 });
  await expect(popup.getByTestId('run-error-code')).toHaveText('shop_mismatch');
  await expect(popup.getByTestId('run-message')).toContainText('đang đăng nhập shop 999999');
  const r = lastResult()!;
  expect(r).toMatchObject({ status: 'FAILED', error_code: 'shop_mismatch' });
  expect((r.steps as { outcome: string }[]).map((s) => s.outcome).slice(0, 3)).toEqual(['ok', 'failed', 'skipped']);
  expect(site.hits).not.toContain('/portal/decoration');
  expect(site.saved).toEqual([]);
  console.log(`[info] screenshots uploaded on failure: ${api.screenshots.length}`);
});

test('pauses on a verification page and resumes after the seller continues', async ({ popup, seller }) => {
  site.requireVerification = true;
  addTask('task-verify');
  await seller.goto(`${SELLER_ORIGIN}/`);
  await pair(popup);
  await clickRun(popup, seller);
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'paused', { timeout: 30_000 });
  await expect(popup.getByTestId('run-message')).toContainText('Hãy hoàn tất trong tab, rồi bấm Tiếp tục');
  expect(seller.url()).toContain('/account/verify');
  expect(api.heartbeats.map((h) => h.state)).toContain('WAITING_USER');
  // The runner did not touch the verification page.
  await expect(seller.getByLabel('Code')).toHaveValue('');

  // Continue too early: still paused.
  await popup.getByRole('button', { name: 'Tiếp tục' }).click();
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'paused');

  // The seller finishes verification themselves, then continues.
  await seller.getByRole('button', { name: "I'm done" }).click();
  await seller.waitForURL(`${SELLER_ORIGIN}/portal/home`);
  await popup.getByRole('button', { name: 'Tiếp tục' }).click();
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'succeeded', { timeout: 30_000 });
  expect(lastResult()).toMatchObject({ status: 'SUCCEEDED' });
  const states = api.heartbeats.map((h) => h.state);
  expect(states.lastIndexOf('RUNNING')).toBeGreaterThan(states.indexOf('WAITING_USER'));
  expect(site.saved).toHaveLength(1);
});

test('rejects a tampered recipe without touching the tab', async ({ popup, seller }) => {
  addTask('task-tampered', { tamper: true });
  await seller.goto(`${SELLER_ORIGIN}/`);
  site.hits = [];
  await pair(popup);
  await clickRun(popup, seller);
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'failed', { timeout: 30_000 });
  await expect(popup.getByTestId('run-error-code')).toHaveText('signature_invalid');
  expect(lastResult()).toMatchObject({ status: 'FAILED', error_code: 'signature_invalid' });
  expect(site.hits).toEqual([]);
  expect(seller.url()).toBe(`${SELLER_ORIGIN}/`);
});

test('lease_lost stops silently and says the task was taken over', async ({ popup, seller }) => {
  site.requireVerification = true;
  addTask('task-lease');
  await seller.goto(`${SELLER_ORIGIN}/`);
  await pair(popup);
  await clickRun(popup, seller);
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'paused', { timeout: 30_000 });
  api.leaseStolen = true; // another device claimed it after our lease expired
  await seller.getByRole('button', { name: "I'm done" }).click();
  await seller.waitForURL(`${SELLER_ORIGIN}/portal/home`);
  await popup.getByRole('button', { name: 'Tiếp tục' }).click();
  await expect(popup.getByTestId('run-panel')).toHaveAttribute('data-phase', 'lease_lost', { timeout: 30_000 });
  await expect(popup.getByTestId('run-message')).toHaveText('Việc này đã được một trình duyệt khác nhận chạy. Tiện ích đã dừng và không thao tác thêm.');
  expect(api.results).toEqual([]);
  expect(site.hits).not.toContain('/portal/decoration');
});
