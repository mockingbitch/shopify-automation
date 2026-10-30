// Prints the Content-Security-Policy for the built dashboard. Nuxt inlines a
// small config script whose content changes every build, so its SHA-256 hash
// is computed here instead of allowing 'unsafe-inline'.
// Usage: node scripts/csp.mjs [.output/public/index.html]
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'

const file = process.argv[2] ?? '.output/public/index.html'
const html = readFileSync(file, 'utf8')
const hashes = [...html.matchAll(/<script(?![^>]*\bsrc=)(?![^>]*type="application\/json")[^>]*>([\s\S]*?)<\/script>/g)]
  .map((m) => `'sha256-${createHash('sha256').update(m[1]).digest('base64')}'`)

const policy = [
  "default-src 'self'",
  `script-src 'self' ${hashes.join(' ')}`.trim(),
  "style-src 'self' 'unsafe-inline'",
  // Shop logos come from Shopee CDNs whose hosts differ per market; images
  // cannot run code, and they are loaded with referrerpolicy=no-referrer.
  "img-src 'self' data: https:",
  "connect-src 'self'",
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
].join('; ')
process.stdout.write(policy + '\n')
