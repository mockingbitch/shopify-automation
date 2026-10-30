import { describe, expect, it } from 'vitest';
import { verifyRecipeSignature } from '@/lib/recipe/signature';
import { testKeys } from './helpers';

describe('verifyRecipeSignature', () => {
  const { keys, sign } = testKeys();
  const json = '{"capability":"shop.banner.upload","note":"Đã lưu ✓"}';

  it('accepts a good signature over the exact bytes', async () => {
    expect(await verifyRecipeSignature(json, sign(json), 'test-1', keys)).toEqual({ ok: true });
  });

  it('rejects a single tampered byte', async () => {
    const sig = sign(json);
    const tampered = json.replace('banner', 'bannes');
    expect(await verifyRecipeSignature(tampered, sig, 'test-1', keys)).toMatchObject({ ok: false, code: 'signature_invalid' });
    // Re-serialized JSON (same data, different bytes) also fails: no canonicalization.
    const reserialized = JSON.stringify(JSON.parse(json), null, 1);
    expect(await verifyRecipeSignature(reserialized, sig, 'test-1', keys)).toMatchObject({ ok: false, code: 'signature_invalid' });
  });

  it('rejects a tampered signature', async () => {
    const bytes = Buffer.from(sign(json), 'base64');
    bytes[10]! ^= 1;
    expect(await verifyRecipeSignature(json, bytes.toString('base64'), 'test-1', keys)).toMatchObject({ ok: false, code: 'signature_invalid' });
  });

  it('rejects an unknown key_id', async () => {
    expect(await verifyRecipeSignature(json, sign(json), 'other', keys)).toMatchObject({ ok: false, code: 'unknown_key' });
    expect(await verifyRecipeSignature(json, sign(json), '__proto__', keys)).toMatchObject({ ok: false, code: 'unknown_key' });
  });

  it('rejects a signature from another key', async () => {
    const other = testKeys();
    expect(await verifyRecipeSignature(json, other.sign(json), 'test-1', keys)).toMatchObject({ ok: false, code: 'signature_invalid' });
  });

  it('rejects malformed signatures', async () => {
    for (const s of ['', 'not base64!', 'AAAA', Buffer.alloc(65).toString('base64')]) {
      expect(await verifyRecipeSignature(json, s, 'test-1', keys)).toMatchObject({ ok: false, code: 'signature_invalid' });
    }
  });
});
