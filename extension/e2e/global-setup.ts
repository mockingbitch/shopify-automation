import { execFileSync } from 'node:child_process';
import { generateKeyPairSync } from 'node:crypto';
import { mkdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { API_ORIGIN, KEY_FILE, KEY_ID, SELLER_ORIGIN, TMP_DIR } from './constants';

/**
 * Generates a fresh Ed25519 test key, then builds the TEST extension with its
 * public key bundled. The fake API signs recipes with the private key.
 */
export default function globalSetup() {
  mkdirSync(TMP_DIR, { recursive: true });
  const { publicKey, privateKey } = generateKeyPairSync('ed25519');
  writeFileSync(KEY_FILE, privateKey.export({ format: 'pem', type: 'pkcs8' }));
  const x = (publicKey.export({ format: 'jwk' }) as { x: string }).x;
  const pub = Buffer.from(x, 'base64url').toString('base64');
  execFileSync('npx', ['wxt', 'build', '--mode', 'test'], {
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    stdio: 'inherit',
    env: {
      ...process.env,
      WXT_API_ORIGIN: API_ORIGIN,
      WXT_SELLER_ORIGINS: SELLER_ORIGIN,
      WXT_RECIPE_PUBLIC_KEYS: JSON.stringify({ [KEY_ID]: pub }),
    },
  });
}
