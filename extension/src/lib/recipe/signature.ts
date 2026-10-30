import { verifyAsync } from '@noble/ed25519';

export type SignatureResult =
  | { ok: true }
  | { ok: false; code: 'unknown_key' | 'signature_invalid'; message: string };

const B64 = /^[A-Za-z0-9+/]*={0,2}$/;

export function decodeBase64(s: string): Uint8Array | null {
  const clean = s.trim();
  if (!B64.test(clean) || clean.length % 4 !== 0) return null;
  try {
    const bin = atob(clean);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  } catch {
    return null;
  }
}

/**
 * Verifies the Ed25519 signature over the exact UTF-8 bytes of `recipeJson`,
 * as received, before anything parses it. `keys` maps key_id to a base64 raw
 * 32-byte public key (bundled at build time). Strict RFC 8032 verification
 * (zip215: false), which matches Go's crypto/ed25519 for honest signatures.
 */
export async function verifyRecipeSignature(
  recipeJson: string,
  signatureB64: string,
  keyId: string,
  keys: Record<string, string>,
): Promise<SignatureResult> {
  if (typeof keyId !== 'string' || !Object.hasOwn(keys, keyId)) {
    return { ok: false, code: 'unknown_key', message: `recipe signed with unknown key "${String(keyId)}"` };
  }
  const pub = decodeBase64(keys[keyId]!);
  const sig = typeof signatureB64 === 'string' ? decodeBase64(signatureB64) : null;
  if (!pub || pub.length !== 32) return { ok: false, code: 'unknown_key', message: `bundled key "${keyId}" is malformed` };
  if (!sig || sig.length !== 64) return { ok: false, code: 'signature_invalid', message: 'signature is not 64 bytes of base64' };
  if (typeof recipeJson !== 'string') return { ok: false, code: 'signature_invalid', message: 'recipe json is not a string' };
  try {
    const ok = await verifyAsync(sig, new TextEncoder().encode(recipeJson), pub, { zip215: false });
    return ok ? { ok: true } : { ok: false, code: 'signature_invalid', message: 'recipe signature does not verify' };
  } catch {
    return { ok: false, code: 'signature_invalid', message: 'recipe signature does not verify' };
  }
}
