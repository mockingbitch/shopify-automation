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

