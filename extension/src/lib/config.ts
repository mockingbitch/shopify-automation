// Values baked in at build time (see build/env.ts). In unit tests, where the
// define is absent, safe defaults apply.
const raw: typeof __BUILD_CONFIG__ =
  typeof __BUILD_CONFIG__ !== 'undefined'
    ? __BUILD_CONFIG__
    : { apiOrigin: 'http://localhost:8080', sellerOrigins: [], fileOrigins: [], recipePublicKeys: {}, testBuild: false };

export const config = Object.freeze({
  ...raw,
  apiBase: `${raw.apiOrigin}/api/v1/ext`,
});
