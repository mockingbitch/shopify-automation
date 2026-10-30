import { browser } from 'wxt/browser';
import { MSG_CHANNEL, type Reply, type Request } from './messages';

export async function send<T>(request: Request): Promise<T> {
  const r = (await browser.runtime.sendMessage({ channel: MSG_CHANNEL, request })) as Reply<T> | undefined;
  if (!r) throw Object.assign(new Error('The extension did not answer. Close and reopen the popup.'), { code: 'no_reply' });
  if (!r.ok) throw Object.assign(new Error(r.error.message), { code: r.error.code });
  return r.data;
}
