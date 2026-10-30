import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import Ajv2020 from 'ajv/dist/2020';
import { describe, expect, it } from 'vitest';
import { SchemaChecker } from '@/lib/recipe/schema-check';
import { validateRecipe } from '@/lib/recipe/validate';
import { bannerRecipe } from './helpers';

const PROD = { allowLocalHttp: false };
const TEST = { allowLocalHttp: true };
const schema = JSON.parse(readFileSync(resolve(__dirname, '../../../internal/browser/recipe.schema.json'), 'utf8'));

const clone = <T>(v: T): T => structuredClone(v);
const codes = (v: unknown, opts = PROD) => {
  const r = validateRecipe(v, opts);
  return r.ok ? [] : r.issues.map((i) => i.code);
};

describe('validateRecipe', () => {
  it('accepts a valid recipe', () => {
    const r = validateRecipe(bannerRecipe(), PROD);
    expect(r).toMatchObject({ ok: true });
  });

  it('rejects an unknown action', () => {
    const r = clone(bannerRecipe()) as any;
    r.steps.splice(1, 0, { eval: { code: 'alert(1)' } });
    expect(codes(r)).toContain('unknown_action');
  });

  it('rejects a step with two actions', () => {
    const r = clone(bannerRecipe()) as any;
    r.steps[0] = { goto: { url: 'https://seller.example.test/a' }, click: { role: 'button' } };
    expect(codes(r)).toContain('schema');
  });

  it('rejects a write before assert_shop_id', () => {
    const r = bannerRecipe();
    r.steps = [r.steps[0]!, r.steps[2]!, r.steps[1]!, ...r.steps.slice(3)];
    expect(codes(r)).toContain('write_before_assert_shop_id');
  });

  it('rejects a recipe with no assert_shop_id but with writes', () => {
    const r = bannerRecipe();
    r.steps = r.steps.filter((s) => !('assert_shop_id' in s));
    expect(codes(r)).toContain('write_before_assert_shop_id');
  });

  it('accepts a read-only recipe without assert_shop_id', () => {
    const r = bannerRecipe({ inputs: {}, steps: [{ goto: { url: 'https://seller.example.test/x' } }, { expect: { text_any: ['Hi'] } }] });
    expect(validateRecipe(r, PROD).ok).toBe(true);
  });

  it('rejects an undeclared input in a template', () => {
    const r = clone(bannerRecipe()) as any;
    r.steps[6].fill.value = '{{input.nope}}';
    expect(codes(r)).toContain('template');
  });

  it('rejects unknown template expressions', () => {
    for (const v of ['{{task.org_id}}', '{{ input.caption | upper }}', '{{input.caption', 'x }} y', '{{constructor}}']) {
      const r = clone(bannerRecipe()) as any;
      r.steps[6].fill.value = v;
      expect(codes(r), v).toContain('template');
    }
  });

  it('allows whitespace inside braces', () => {
    const r = clone(bannerRecipe()) as any;
    r.steps[6].fill.value = '{{ task.shopee_shop_id }}-{{ input.caption }}';
    expect(validateRecipe(r, PROD).ok).toBe(true);
  });

  it('rejects a files input used as text and a text input used for upload', () => {
    const a = clone(bannerRecipe()) as any;
    a.steps[6].fill.value = '{{input.banner_image}}';
    expect(codes(a)).toContain('template');
    const b = clone(bannerRecipe()) as any;
    b.steps[5].upload.files = '{{input.caption}}';
    expect(codes(b)).toContain('template');
    const c = clone(bannerRecipe()) as any;
    c.steps[5].upload.files = '{{input.missing}}';
    expect(codes(c)).toContain('template');
  });

  it('rejects templates outside template fields', () => {
    const r = clone(bannerRecipe()) as any;
    r.steps[2].click.name_any = ['{{input.caption}}'];
    expect(codes(r)).toContain('template');
  });

  it('rejects http origins in production and accepts localhost only in TEST builds', () => {
    const r = bannerRecipe({
      start_url: 'http://localhost:4710/portal/home',
      allowed_origins: ['http://localhost:4710'],
    });
    r.steps[0] = { goto: { url: 'http://localhost:4710/portal/home' } };
    expect(codes(r, PROD)).toContain('schema');
    expect(validateRecipe(r, TEST).ok).toBe(true);
    const remote = clone(r);
    remote.allowed_origins = ['http://evil.example'];
    remote.start_url = 'http://evil.example/';
    expect(validateRecipe(remote, TEST).ok).toBe(false);
  });

  it('requires start_url and literal goto urls on allowed origins', () => {
    expect(codes(bannerRecipe({ start_url: 'https://other.example/x' }))).toContain('origin');
    const r = clone(bannerRecipe()) as any;
    r.steps[0].goto.url = 'https://other.example/x';
    expect(codes(r)).toContain('origin');
    const t = clone(bannerRecipe()) as any;
    t.steps[0].goto.url = '{{input.caption}}';
    expect(codes(t)).toContain('origin');
  });

  it('rejects writes in verify', () => {
    const r = bannerRecipe({ verify: [{ click: { role: 'button', name_any: ['Save'] } }] });
    expect(codes(r)).toContain('verify_write');
  });

  it('requires a capture group in assert_shop_id.pattern', () => {
    const r = clone(bannerRecipe()) as any;
    r.steps[1].assert_shop_id.pattern = 'Shop ID: \\d+';
    expect(codes(r)).toContain('regex');
    r.steps[1].assert_shop_id.pattern = '(';
    expect(codes(r)).toContain('regex');
  });

  it('rejects unknown top-level properties, bad markets and pace out of range', () => {
    expect(codes({ ...bannerRecipe(), script: 'x' })).toContain('schema');
    expect(codes({ ...bannerRecipe(), market: 'US' })).toContain('schema');
    expect(codes({ ...bannerRecipe(), pace_ms: 0 })).toContain('schema');
    expect(codes({ ...bannerRecipe(), steps: [] })).toContain('schema');
    expect(codes(null)).toContain('schema');
  });
});

describe('SchemaChecker mirrors Ajv (JSON Schema 2020-12) on the real schema', () => {
  const ajv = new Ajv2020({ strict: false, allErrors: true });
  const ajvValidate = ajv.compile(schema);
  const ours = new SchemaChecker(schema);
  const base = bannerRecipe();
  const mutations: Array<(r: any) => void> = [
    () => {},
    (r) => delete r.stop_on,
    (r) => (r.stop_on = []),
    (r) => (r.stop_on = [{ url_matches: 'a', text_any: ['b'] }]),
    (r) => (r.stop_on = [{}]),
    (r) => (r.capability = 'Shop.Banner'),
    (r) => (r.capability = 'shop'),
    (r) => (r.version = 0),
    (r) => (r.version = 1.5),
    (r) => (r.version = '1'),
    (r) => (r.start_url = 'https://x'),
    (r) => (r.allowed_origins = []),
    (r) => (r.allowed_origins = ['https://A.example']),
    (r) => (r.allowed_origins = ['https://a.example:8443']),
    (r) => (r.allowed_origins = Array(6).fill('https://a.example')),
    (r) => (r.pace_ms = 5001),
    (r) => (r.inputs = { Bad: { type: 'text' } }),
    (r) => (r.inputs = { ok: { type: 'number' } }),
    (r) => (r.inputs = { ok: { type: 'text', extra: 1 } }),
    (r) => (r.inputs = { ok: {} }),
    (r) => (r.inputs = Object.fromEntries(Array.from({ length: 21 }, (_, i) => [`i${i}`, { type: 'text' }]))),
    (r) => (r.steps = [{ timeout_ms: 100 }]),
    (r) => (r.steps = [{ click: {} }]),
    (r) => (r.steps = [{ click: { role: 'banner' } }]),
    (r) => (r.steps = [{ click: { name_any: [] } }]),
    (r) => (r.steps = [{ click: { name_any: [''] } }]),
    (r) => (r.steps = [{ click: { text_any: ['é'.repeat(200)] } }]),
    (r) => (r.steps = [{ click: { text_any: ['é'.repeat(201)] } }]),
    (r) => (r.steps = [{ click: { text_any: ['😀'.repeat(200)] } }]),
    (r) => (r.steps = [{ click: { role: 'button' }, timeout_ms: 99 }]),
    (r) => (r.steps = [{ click: { role: 'button' }, timeout_ms: 60000 }]),
    (r) => (r.steps = [{ upload: { target: { selector: 'x' }, files: '{{input.a}} ' } }]),
    (r) => (r.steps = [{ upload: { target: { selector: 'x' }, files: '{{ input.a }}' } }]),
    (r) => (r.steps = [{ expect: { text_any: ['a'], target: { role: 'img' } } }]),
    (r) => (r.steps = [{ expect: {} }]),
    (r) => (r.steps = [{ assert_shop_id: { target: { selector: 'x' } } }]),
    (r) => (r.steps = [{ screenshot: { name: 'Bad Name' } }]),
    (r) => (r.steps = [{ screenshot: { name: 'ok_name-1' } }]),
    (r) => (r.steps = [{ goto: { url: 'x'.repeat(2001) } }]),
    (r) => (r.steps = Array(61).fill({ wait_for: { role: 'button' } })),
    (r) => (r.verify = Array(11).fill({ wait_for: { role: 'button' } })),
    (r) => (r.extra = true),
    (r) => (r.steps = 'nope'),
    (r) => (r.steps = [null]),
  ];
  mutations.forEach((m, i) => {
    it(`mutation ${i}`, () => {
      const r = structuredClone(base) as any;
      m(r);
      expect(ours.check(r).length === 0).toBe(ajvValidate(r));
    });
  });

  it('throws on schema keywords it does not implement', () => {
    expect(() => new SchemaChecker({ type: 'object', if: { required: ['a'] } })).toThrow(/unsupported/);
  });
});
