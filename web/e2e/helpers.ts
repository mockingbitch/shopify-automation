/// <reference types="node" />
import type { APIRequestContext } from '@playwright/test'

const mailpit = process.env.MAILPIT_URL ?? 'http://localhost:8025'

// latestLink returns the first link with `path` in the newest email to `to`.
export async function latestLink(request: APIRequestContext, to: string, path: string): Promise<string> {
  for (let i = 0; i < 20; i++) {
    const search = await (await request.get(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:${to}`)}`)).json()
    for (const m of search.messages ?? []) {
      const msg = await (await request.get(`${mailpit}/api/v1/message/${m.ID}`)).json()
      const match = (msg.Text as string).match(new RegExp(`https?://[^\\s]*${path}[^\\s]*`))
      if (match) return match[0]
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`no email with ${path} to ${to}`)
}


// totp returns the current RFC 6238 code for a base32 secret (as an
// authenticator app would).
export async function totp(secretB32: string, now = Date.now()): Promise<string> {
  const { createHmac } = await import('node:crypto')
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const c of secretB32.replace(/\s|=/g, '').toUpperCase()) bits += alphabet.indexOf(c).toString(2).padStart(5, '0')
  const key = Buffer.from((bits.match(/.{8}/g) ?? []).map((b) => parseInt(b, 2)))
  const msg = Buffer.alloc(8)
  msg.writeBigUInt64BE(BigInt(Math.floor(now / 1000 / 30)))
  const h = createHmac('sha1', key).update(msg).digest()
  const off = h[h.length - 1]! & 0x0f
  return String((h.readUInt32BE(off) & 0x7fffffff) % 1_000_000).padStart(6, '0')
}
