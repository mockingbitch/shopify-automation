import { beforeEach, describe, expect, it } from 'vitest';
import { checkStopOn, execute } from '@/lib/dom/actions';

const html = (s: string) => {
  document.body.innerHTML = s;
};
const href = 'https://seller.example.test/portal/home';

describe('stop_on detection', () => {
  it('matches url, visible text and selector', () => {
    html(`<h1>Verify your identity</h1>`);
    expect(checkStopOn(document, 'https://seller.example.test/account/login?next=/', [{ url_matches: '/login|/verify' }])).toEqual({ ok: true, matched: 0 });
    expect(checkStopOn(document, href, [{ url_matches: '/login' }, { text_any: ['verify YOUR identity'] }])).toEqual({ ok: true, matched: 1 });
    html(`<div class="captcha-box"></div>`);
    expect(checkStopOn(document, href, [{ selector: '.captcha-box' }]).matched).toBe(0);
  });

  it('does not match hidden text or hidden elements', () => {
    html(`<div style="display:none"><h1>Verify your identity</h1><div class="captcha-box">x</div></div><p>Welcome</p>`);
    expect(checkStopOn(document, href, [{ url_matches: '/login' }, { selector: '.captcha-box' }]).matched).toBeUndefined();
  });
});

describe('actions', () => {
  beforeEach(() => html(''));

  it('assert_shop_id passes on the right shop and fails on another', async () => {
    html(`<header><span data-shop-id>Shop ID: 123456</span></header>`);
    const cmd = { op: 'assert_shop_id', target: { selector: '[data-shop-id]' }, pattern: 'Shop ID:\\s*(\\d+)', timeoutMs: 200 } as const;
    expect(await execute(document, href, { ...cmd, expected: '123456' })).toEqual({ ok: true, detail: '123456' });
    expect(await execute(document, href, { ...cmd, expected: '999999' })).toMatchObject({ ok: false, code: 'shop_mismatch' });
    // A prefix is not a match.
    html(`<span data-shop-id>Shop ID: 1234567</span>`);
    expect(await execute(document, href, { ...cmd, expected: '123456' })).toMatchObject({ ok: false, code: 'shop_mismatch' });
  });

  it('click fires on the unique target', async () => {
    let clicks = 0;
    html(`<button>Edit</button>`);
    document.querySelector('button')!.addEventListener('click', () => clicks++);
    expect(await execute(document, href, { op: 'click', target: { role: 'button', name_any: ['Edit'] }, timeoutMs: 200 })).toEqual({ ok: true });
    expect(clicks).toBe(1);
  });

  it('wait_for waits for an element that appears later', async () => {
    setTimeout(() => html(`<button>Edit</button>`), 150);
    expect(await execute(document, href, { op: 'wait_for', target: { role: 'button', name_any: ['Edit'] }, timeoutMs: 1000 })).toEqual({ ok: true });
    expect(await execute(document, href, { op: 'wait_for', target: { role: 'button', name_any: ['Nope'] }, timeoutMs: 150 })).toMatchObject({ code: 'target_not_found' });
  });

  it('fill sets the value and fires input/change', async () => {
    html(`<label for=c>Caption</label><input id=c>`);
    const seen: string[] = [];
    const input = document.querySelector('input')!;
    input.addEventListener('input', () => seen.push('input'));
    input.addEventListener('change', () => seen.push('change'));
    expect(await execute(document, href, { op: 'fill', target: { role: 'textbox', name_any: ['Caption'] }, value: 'Hello', timeoutMs: 200 })).toEqual({ ok: true });
    expect(input.value).toBe('Hello');
    expect(seen).toEqual(['input', 'change']);
  });

  it('select picks an option by value or label', async () => {
    html(`<label for=s>Region</label><select id=s><option value="">-</option><option value="hn">Hà Nội</option><option value="hcm">Hồ Chí Minh</option></select>`);
    const sel = document.querySelector('select')!;
    await execute(document, href, { op: 'select', target: { role: 'combobox', name_any: ['Region'] }, value: 'Hồ Chí Minh', timeoutMs: 200 });
    expect(sel.value).toBe('hcm');
    await execute(document, href, { op: 'select', target: { role: 'combobox', name_any: ['Region'] }, value: 'hn', timeoutMs: 200 });
    expect(sel.value).toBe('hn');
    expect(await execute(document, href, { op: 'select', target: { role: 'combobox' }, value: 'x', timeoutMs: 200 })).toMatchObject({ code: 'option_not_found' });
  });

  it('upload sets files on a hidden file input', async () => {
    html(`<label>Choose<input type=file accept="image/*" style="display:none"></label>`);
    const input = document.querySelector('input')!;
    let changed = 0;
    input.addEventListener('change', () => changed++);
    const b64 = Buffer.from('PNGDATA').toString('base64');
    const r = await execute(document, href, { op: 'upload', target: { selector: 'input[type=file]' }, files: [{ name: 'banner.png', type: 'image/png', b64 }], timeoutMs: 200 });
    expect(r).toMatchObject({ ok: true });
    expect(input.files?.length).toBe(1);
    expect(input.files?.[0]?.name).toBe('banner.png');
    expect(await input.files![0]!.text()).toBe('PNGDATA');
    expect(changed).toBe(1);
  });

  it('expect text_any and target', async () => {
    html(`<p>Đã lưu</p><img alt="Shop banner" src="x.png">`);
    expect(await execute(document, href, { op: 'expect', text_any: ['Saved', 'Đã lưu'], timeoutMs: 100 })).toMatchObject({ ok: true });
    expect(await execute(document, href, { op: 'expect', text_any: ['Nope'], timeoutMs: 100 })).toMatchObject({ code: 'expect_failed' });
    expect(await execute(document, href, { op: 'expect', target: { role: 'img', name_any: ['Shop banner'] }, timeoutMs: 100 })).toMatchObject({ ok: true });
  });
});
