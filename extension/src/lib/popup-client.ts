import { browser } from 'wxt/browser';
import { MSG_CHANNEL, type Reply, type Request } from './messages';

export async function send<T>(request: Request): Promise<T> {
  const r = (await browser.runtime.sendMessage({ channel: MSG_CHANNEL, request })) as Reply<T> | undefined;
  if (!r) throw Object.assign(new Error('Tiện ích không phản hồi. Hãy đóng rồi mở lại cửa sổ Tiện ích.'), { code: 'no_reply' });
  if (!r.ok) throw Object.assign(new Error(r.error.message), { code: r.error.code });
  return r.data;
}
