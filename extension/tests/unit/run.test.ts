import { describe, expect, it } from 'vitest';
import type { ClaimResponse, ResultBody } from '@/lib/api/types';
import { ApiError } from '@/lib/api/types';
import { execute } from '@/lib/dom/actions';
import type { Command } from '@/lib/runner/protocol';
import { TaskRun, type RunState, type TabDriver } from '@/lib/runner/run';
import { ORIGIN, bannerRecipe, testKeys } from './helpers';

type Page = (doc: Document, nav: (path: string) => void) => void;

/** A fake seller centre rendered into happy-dom, one "page" at a time. */
function fakeSite(shopId: string, opts: { verifyFirst?: boolean } = {}) {
  const hits: string[] = [];
  let verified = !opts.verifyFirst;
  const pages: Record<string, Page> = {
    '/portal/home': (doc, nav) => {
      if (!verified) return nav('/account/verify');
      doc.body.innerHTML = `<header><span data-shop-id>Shop ID: ${shopId}</span></header><nav><a href="/portal/decoration">Shop decoration</a></nav>`;
      doc.querySelector('a')!.addEventListener('click', (e) => {
        e.preventDefault();
        setTimeout(() => nav('/portal/decoration'), 30);
      });
    },
    '/portal/decoration': (doc) => {
      doc.body.innerHTML = `<h1>Banner</h1><div data-banner></div><button>Edit</button><form hidden>
        <input type=file accept="image/*"><label for=cap>Caption</label><input id=cap><button type=button>Save</button></form><p id=msg></p>`;
      const form = doc.querySelector('form')!;
      doc.querySelector('button')!.addEventListener('click', () => (form.hidden = false));
      form.querySelector('button')!.addEventListener('click', () => {
        const f = (form.querySelector('input[type=file]') as HTMLInputElement).files?.[0];
        if (!f) return;
        doc.querySelector('[data-banner]')!.innerHTML = `<img alt="Shop banner" src="blob:x">`;
        doc.querySelector('#msg')!.textContent = `Saved ${f.name}`;
      });
    },
    '/account/verify': (doc) => {
      doc.body.innerHTML = `<h1>Verify your identity</h1><button>Done</button>`;
    },
  };
  let url = 'about:blank';
  const nav = (path: string) => {
    hits.push(path);
    url = `${ORIGIN}${path}`;
    document.body.innerHTML = '';
    pages[path]?.(document, nav);
  };
  const driver: TabDriver = {
    url: async () => url,
    navigate: async (u) => nav(new URL(u).pathname),
    waitIdle: async () => {},
    send: (cmd: Command) => execute(document, url, cmd),
    screenshot: async () => new Blob(['png'], { type: 'image/png' }),
    canAccess: async () => true,
  };
  return { driver, hits, finishVerification: () => { verified = true; nav('/portal/home'); }, url: () => url };
}

function fakeApi(opts: { leaseLostOnHeartbeat?: (state: string) => boolean } = {}) {
  const calls = { heartbeats: [] as string[], results: [] as Omit<ResultBody, 'lease_token'>[], screenshots: [] as string[] };
  return {
    calls,
    api: {
      heartbeat: async (state: 'RUNNING' | 'WAITING_USER') => {
        calls.heartbeats.push(state);
        if (opts.leaseLostOnHeartbeat?.(state)) throw new ApiError(409, 'lease_lost', { code: 'lease_lost' });
      },
      screenshot: async (_png: Blob, name: string) => {
        calls.screenshots.push(name);
        return `shot-${calls.screenshots.length}`;
      },
      result: async (b: Omit<ResultBody, 'lease_token'>) => {
        calls.results.push(b);
      },
    },
  };
}

const { keys, sign } = testKeys();

function claimFor(json: string, shopId = '123456', sig = sign(json)): ClaimResponse {
  return {
    lease_token: 'lease-1',
    lease_expires_at: new Date().toISOString(),
    recipe: { json, signature: sig, key_id: 'test-1' },
    inputs: {
      banner_image: { type: 'files', files: [{ name: 'banner.png', url: 'https://files.example.test/banner.png', content_type: 'image/png' }] },
      caption: { type: 'text', value: 'Summer' },
    },
    task: { shopee_shop_id: shopId, region: 'VN', capability: 'shop.banner.upload' },
  };
}

function makeRun(claim: ClaimResponse, site: ReturnType<typeof fakeSite>, api: ReturnType<typeof fakeApi>, heartbeatEveryMs?: number) {
  const states: RunState[] = [];
  const run = new TaskRun('task-1', 'Change banner', claim, {
    driver: site.driver,
    api: api.api,
    publicKeys: keys,
    allowLocalHttp: false,
    fetchFile: async () => new TextEncoder().encode('PNGDATA').buffer as ArrayBuffer,
    onState: (s) => states.push(s),
    sleep: () => new Promise((r) => setTimeout(r, 5)),
    heartbeatEveryMs,
  });
  return { run, states };
}

const waitFor = async (cond: () => boolean, ms = 3000) => {
  const end = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > end) throw new Error('timed out');
    await new Promise((r) => setTimeout(r, 10));
  }
};

describe('TaskRun against a fake DOM', () => {
  const json = JSON.stringify(bannerRecipe());

  it('runs a multi-page recipe with an upload and reports SUCCEEDED', async () => {
    const site = fakeSite('123456');
    const api = fakeApi();
    const { run } = makeRun(claimFor(json), site, api);
    const final = await run.run();
    expect(final.phase).toBe('succeeded');
    expect(api.calls.results).toHaveLength(1);
    const res = api.calls.results[0]!;
    expect(res.status).toBe('SUCCEEDED');
    expect(res.steps.map((s) => s.outcome)).toEqual(Array(10).fill('ok'));
    expect(res.steps.map((s) => s.action)).toEqual(['goto', 'assert_shop_id', 'click', 'wait_for', 'click', 'upload', 'fill', 'click', 'expect', 'expect']);
    expect(document.querySelector('#msg')!.textContent).toBe('Saved banner.png');
    expect((document.querySelector('#cap') as HTMLInputElement).value).toBe('Banner for 123456 Summer');
    // start_url (tab was not on Seller Centre), then the goto step, then the link click.
    expect(site.hits).toEqual(['/portal/home', '/portal/home', '/portal/decoration']);
    expect(api.calls.heartbeats[0]).toBe('RUNNING');
  });

  it('stops with shop_mismatch before any write', async () => {
    const site = fakeSite('999999');
    const api = fakeApi();
    const { run } = makeRun(claimFor(json), site, api);
    const final = await run.run();
    expect(final).toMatchObject({ phase: 'failed', errorCode: 'shop_mismatch' });
    const res = api.calls.results[0]!;
    expect(res).toMatchObject({ status: 'FAILED', error_code: 'shop_mismatch' });
    expect(res.steps.slice(0, 3).map((s) => s.outcome)).toEqual(['ok', 'failed', 'skipped']);
    expect(site.hits).not.toContain('/portal/decoration'); // never reached the settings page
    expect(api.calls.screenshots).toEqual(['failure']);
  });

  it('pauses on a stop_on page, waits for Continue, then resumes', async () => {
    const site = fakeSite('123456', { verifyFirst: true });
    const api = fakeApi();
    const { run, states } = makeRun(claimFor(json), site, api);
    const done = run.run();
    await waitFor(() => states.at(-1)?.phase === 'paused');
    expect(states.at(-1)?.pauseReason).toBe('verification');
    expect(api.calls.heartbeats).toContain('WAITING_USER');
    // Continue while still on the verification page: stays paused.
    run.continue();
    await new Promise((r) => setTimeout(r, 50));
    expect(states.at(-1)?.phase).toBe('paused');
    site.finishVerification();
    run.continue();
    const final = await done;
    expect(final.phase).toBe('succeeded');
    expect(api.calls.heartbeats.filter((h) => h === 'WAITING_USER').length).toBe(2);
  });

  it('rejects a tampered recipe before parsing or touching the page', async () => {
    const site = fakeSite('123456');
    const api = fakeApi();
    const sig = sign(json);
    const tampered = json.replace('Shop decoration', 'Shop decoratioN');
    const { run } = makeRun(claimFor(tampered, '123456', sig), site, api);
    const final = await run.run();
    expect(final).toMatchObject({ phase: 'failed', errorCode: 'signature_invalid' });
    expect(api.calls.results[0]).toMatchObject({ status: 'FAILED', error_code: 'signature_invalid' });
    expect(site.hits).toEqual([]);
    expect(api.calls.screenshots).toEqual([]);
  });

  it('rejects a recipe for another capability or market', async () => {
    const other = JSON.stringify(bannerRecipe({ market: 'SG' }));
    const api = fakeApi();
    const { run } = makeRun(claimFor(other), fakeSite('123456'), api);
    expect(await run.run()).toMatchObject({ errorCode: 'recipe_mismatch' });
  });

  it('rejects a validly signed recipe that breaks the rules', async () => {
    const r = bannerRecipe();
    r.steps.splice(1, 1); // no assert_shop_id
    const bad = JSON.stringify(r);
    const api = fakeApi();
    const site = fakeSite('123456');
    const { run } = makeRun(claimFor(bad), site, api);
    expect(await run.run()).toMatchObject({ errorCode: 'recipe_invalid' });
    expect(site.hits).toEqual([]);
  });

  it('stops silently on lease_lost and posts no result', async () => {
    const site = fakeSite('123456', { verifyFirst: true });
    const api = fakeApi({ leaseLostOnHeartbeat: (s) => s === 'RUNNING' && site.url().endsWith('/portal/home') });
    const { run, states } = makeRun(claimFor(json), site, api);
    const done = run.run();
    await waitFor(() => states.at(-1)?.phase === 'paused');
    site.finishVerification();
    run.continue();
    const final = await done;
    expect(final).toMatchObject({ phase: 'lease_lost', message: 'This task was taken over.' });
    expect(api.calls.results).toEqual([]);
    expect(site.hits).not.toContain('/portal/decoration');
  });

  it('Stop while paused reports stopped_by_user', async () => {
    const site = fakeSite('123456', { verifyFirst: true });
    const api = fakeApi();
    const { run, states } = makeRun(claimFor(json), site, api);
    const done = run.run();
    await waitFor(() => states.at(-1)?.phase === 'paused');
    run.stop();
    expect(await done).toMatchObject({ phase: 'stopped', errorCode: 'stopped_by_user' });
    expect(api.calls.results[0]).toMatchObject({ status: 'FAILED', error_code: 'stopped_by_user' });
  });

  it('fails with missing_input when a required input is absent', async () => {
    const claim = claimFor(json);
    delete (claim.inputs as Record<string, unknown>).banner_image;
    const api = fakeApi();
    const { run } = makeRun(claim, fakeSite('123456'), api);
    expect(await run.run()).toMatchObject({ errorCode: 'missing_input' });
  });

  it('checks the input file checksum', async () => {
    const claim = claimFor(json);
    (claim.inputs.banner_image as { files: { sha256?: string }[] }).files[0]!.sha256 = '0'.repeat(64);
    const api = fakeApi();
    const { run } = makeRun(claim, fakeSite('123456'), api);
    expect(await run.run()).toMatchObject({ errorCode: 'input_checksum_mismatch' });
  });

  it('keeps sending heartbeats while paused', async () => {
    const site = fakeSite('123456', { verifyFirst: true });
    const api = fakeApi();
    const { run, states } = makeRun(claimFor(json), site, api, 20);
    const done = run.run();
    await waitFor(() => states.at(-1)?.phase === 'paused');
    await new Promise((r) => setTimeout(r, 120));
    expect(api.calls.heartbeats.filter((h) => h === 'WAITING_USER').length).toBeGreaterThanOrEqual(3);
    run.stop();
    await done;
  });
});
