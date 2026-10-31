import { browser } from 'wxt/browser';
import { ensureAlarm, handle, onAlarm, recoverAfterRestart, refreshTasks } from '@/lib/background/controller';
import { MSG_CHANNEL, type Reply, type Request } from '@/lib/messages';

export default defineBackground({
  type: 'module',
  main() {
    void recoverAfterRestart();
    void ensureAlarm();
    browser.runtime.onInstalled.addListener(() => void ensureAlarm().then(refreshTasks));
    browser.runtime.onStartup.addListener(() => void ensureAlarm().then(refreshTasks));
    browser.alarms.onAlarm.addListener((a) => void onAlarm(a.name));

    browser.runtime.onMessage.addListener((msg: unknown, sender, sendResponse: (r: Reply) => void) => {
      const m = msg as { channel?: string; request?: Request };
      // Only our own extension pages (the popup) talk to the service worker; content scripts never send.
      if (m?.channel !== MSG_CHANNEL || sender.id !== browser.runtime.id || !sender.url?.startsWith(browser.runtime.getURL('/popup.html'))) return false;
      handle(m.request!)
        .then(sendResponse)
        .catch((e: unknown) => sendResponse({ ok: false, error: { code: 'internal', message: `Tiện ích gặp lỗi (${String((e as Error)?.message ?? e)}). Hãy đóng rồi mở lại cửa sổ Tiện ích.` } }));
      return true;
    });
  },
});
