/**
 * Build-time configuration. Everything here is read from environment variables
 * (or .env files, which WXT loads) when the extension is built, validated, and
 * baked into the bundle. Nothing is configurable at run time.
 */

export interface BuildConfig {
  /** Origin of the Go API, e.g. https://api.example.com. */
  apiOrigin: string;
  /** Seller Centre origins the extension may act on (host permissions). */
  sellerOrigins: string[];
  /** Extra origins input files are downloaded from (presigned S3 URLs). */
  fileOrigins: string[];
  /** key_id -> base64 raw 32-byte Ed25519 public key. */
  recipePublicKeys: Record<string, string>;
  /** TEST builds also accept http://localhost:* and http://127.0.0.1:* recipe origins. */
  testBuild: boolean;
}

const LOCAL_HTTP = /^http:\/\/(localhost|127\.0\.0\.1)(:\d{1,5})?$/;
const HTTPS_ORIGIN = /^https:\/\/[a-z0-9.-]+(:\d{1,5})?$/;

function parseOrigin(raw: string, name: string, testBuild: boolean, allowLocalInProd: boolean): string {
  const value = raw.trim().replace(/\/+$/, '').toLowerCase();
  if (HTTPS_ORIGIN.test(value)) return value;
  if (LOCAL_HTTP.test(value) && (testBuild || allowLocalInProd)) return value;
  throw new Error(
    `${name}: "${raw}" is not an allowed origin (expected https://host[:port]` +
      (testBuild ? ' or http://localhost[:port]' : '') +
      ')',
  );
}

function parseList(raw: string | undefined, name: string, testBuild: boolean): string[] {
  if (!raw || !raw.trim()) return [];
  return raw
    .split(/[\s,]+/)
    .filter(Boolean)
    .map((o) => parseOrigin(o, name, testBuild, false));
}

function parseKeys(raw: string | undefined): Record<string, string> {
  if (!raw || !raw.trim()) return {};
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error('WXT_RECIPE_PUBLIC_KEYS must be JSON: {"<key_id>":"<base64 32-byte key>"}');
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    throw new Error('WXT_RECIPE_PUBLIC_KEYS must be a JSON object');
  }
  const out: Record<string, string> = {};
  for (const [id, key] of Object.entries(parsed)) {
    if (!/^[A-Za-z0-9._-]{1,64}$/.test(id)) throw new Error(`WXT_RECIPE_PUBLIC_KEYS: bad key_id "${id}"`);
    if (typeof key !== 'string' || Buffer.from(key, 'base64').length !== 32) {
      throw new Error(`WXT_RECIPE_PUBLIC_KEYS: key "${id}" must be base64 of 32 raw bytes`);
    }
    out[id] = key;
  }
  return out;
}

export function readBuildConfig(mode: string, env: NodeJS.ProcessEnv = process.env): BuildConfig {
  const testBuild = mode === 'test';
  // The API origin may be http://localhost in any build so local development works.
  const apiOrigin = parseOrigin(env.WXT_API_ORIGIN || 'http://localhost:8080', 'WXT_API_ORIGIN', testBuild, true);
  return {
    apiOrigin,
    sellerOrigins: parseList(env.WXT_SELLER_ORIGINS, 'WXT_SELLER_ORIGINS', testBuild),
    fileOrigins: parseList(env.WXT_FILE_ORIGINS, 'WXT_FILE_ORIGINS', testBuild),
    recipePublicKeys: parseKeys(env.WXT_RECIPE_PUBLIC_KEYS),
    testBuild,
  };
}

/** Chrome match pattern for an origin. */
export function matchPattern(origin: string): string {
  return `${origin}/*`;
}
