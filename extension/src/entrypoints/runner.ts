import { execute } from '@/lib/dom/actions';
import { browser } from 'wxt/browser';
import { RUNNER_CHANNEL, type RunnerMessage } from '@/lib/runner/protocol';

/**
 * Injected into the Seller Centre tab (isolated world) by the service worker
 * with chrome.scripting.executeScript, once per document. It executes one
 * command at a time and holds no run state: the service worker does.
 */
export default defineUnlistedScript(() => {
  const flag = '__shopeeAutomationRunner';
  const g = globalThis as unknown as Record<string, boolean>;
  if (g[flag]) return;
  g[flag] = true;

  browser.runtime.onMessage.addListener((msg: unknown, sender, sendResponse: (r: unknown) => void) => {
    if (sender.id !== browser.runtime.id || (msg as RunnerMessage)?.channel !== RUNNER_CHANNEL) return false;
    execute(document, location.href, (msg as RunnerMessage).command)
      .then(sendResponse)
      .catch((e: unknown) => sendResponse({ ok: false, code: 'step_failed', message: String((e as Error)?.message ?? e) }));
    return true;
  });
});
