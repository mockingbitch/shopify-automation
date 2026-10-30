import { generateKeyPairSync, sign } from 'node:crypto';
import type { Recipe } from '@/lib/recipe/types';

export function testKeys() {
  const { publicKey, privateKey } = generateKeyPairSync('ed25519');
  const jwk = publicKey.export({ format: 'jwk' }) as { x: string };
  const pubB64 = Buffer.from(jwk.x, 'base64url').toString('base64');
  return {
    keys: { 'test-1': pubB64 } as Record<string, string>,
    sign: (json: string) => sign(null, Buffer.from(json, 'utf8'), privateKey).toString('base64'),
  };
}

export const ORIGIN = 'https://seller.example.test';

/** A recipe that imitates a banner change on a fictional seller centre. Not a real Seller Centre recipe. */
export function bannerRecipe(overrides: Partial<Recipe> = {}): Recipe {
  return {
    capability: 'shop.banner.upload',
    version: 1,
    market: 'VN',
    start_url: `${ORIGIN}/portal/home`,
    allowed_origins: [ORIGIN],
    pace_ms: 200,
    inputs: { banner_image: { type: 'files', required: true }, caption: { type: 'text', required: false } },
    steps: [
      { goto: { url: `${ORIGIN}/portal/home` } },
      { assert_shop_id: { target: { selector: '[data-shop-id]' }, pattern: 'Shop ID:\\s*(\\d+)' } },
      { click: { role: 'link', name_any: ['Shop decoration'] } },
      { wait_for: { role: 'button', name_any: ['Edit'] }, timeout_ms: 2000 },
      { click: { role: 'button', name_any: ['Edit'] } },
      { upload: { target: { selector: 'input[type=file]' }, files: '{{input.banner_image}}' } },
      { fill: { target: { role: 'textbox', name_any: ['Caption'] }, value: 'Banner for {{task.shopee_shop_id}} {{input.caption}}' } },
      { click: { role: 'button', name_any: ['Save'] } },
      { expect: { text_any: ['Saved'] }, timeout_ms: 2000 },
    ],
    verify: [{ expect: { target: { role: 'img', name_any: ['Shop banner'] } } }],
    stop_on: [{ url_matches: '/login|/verify' }, { text_any: ['Verify your identity'] }],
    ...overrides,
  };
}
