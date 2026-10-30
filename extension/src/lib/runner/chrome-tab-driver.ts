import { browser } from 'wxt/browser';
import { RUNNER_CHANNEL, type Command, type CommandResult, type RunnerMessage } from './protocol';
import { DriverError, type TabDriver } from './run';

const RUNNER_FILE = '/runner.js';

function isNoReceiver(msg: string): boolean {
  return /Receiving end does not exist|Could not establish connection|No tab with id|Frame with ID 0 was removed/i.test(msg);
}
function isPortClosed(msg: string): boolean {
  return /message port closed|message channel closed|page was unloaded|back\/forward cache/i.test(msg);
}

/** Drives one tab: same tab for the whole run, never opens a new one. */
export class ChromeTabDriver implements TabDriver {
  constructor(readonly tabId: number) {}

  private async tab() {
    try {
      return await browser.tabs.get(this.tabId);
    } catch {
      throw new DriverError('tab_closed', 'The Seller Centre tab was closed.');
    }
  }

  async url(): Promise<string | null> {
    const t = await this.tab();
    // tab.url is only visible for origins we hold host permission for (no "tabs" permission).
    return t.url || null;
  }

  async waitIdle(timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      const t = await this.tab();
      if (t.status === 'complete') return;
      if (Date.now() > deadline) throw new DriverError('timeout', 'The page did not finish loading.');
      await new Promise((r) => setTimeout(r, 150));
    }
  }

  async navigate(url: string, timeoutMs: number): Promise<void> {
    await this.tab();
    const done = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        cleanup();
        reject(new DriverError('timeout', 'The page did not finish loading.'));
      }, timeoutMs);
      let sawLoading = false;
      const onUpdated = (id: number, info: { status?: string }) => {
        if (id !== this.tabId) return;
        if (info.status === 'loading') sawLoading = true;
        if (info.status === 'complete' && sawLoading) {
          cleanup();
          resolve();
        }
      };
      const onRemoved = (id: number) => {
        if (id !== this.tabId) return;
        cleanup();
        reject(new DriverError('tab_closed', 'The Seller Centre tab was closed.'));
      };
      const cleanup = () => {
        clearTimeout(timer);
        browser.tabs.onUpdated.removeListener(onUpdated);
        browser.tabs.onRemoved.removeListener(onRemoved);
      };
      browser.tabs.onUpdated.addListener(onUpdated);
      browser.tabs.onRemoved.addListener(onRemoved);
    });
    await browser.tabs.update(this.tabId, { url });
    await done;
  }

  private async rawSend(cmd: Command, timeoutMs: number): Promise<CommandResult> {
    const msg: RunnerMessage = { channel: RUNNER_CHANNEL, command: cmd };
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      return await Promise.race([
        browser.tabs.sendMessage(this.tabId, msg, { frameId: 0 }) as Promise<CommandResult>,
        new Promise<never>((_, reject) => {
          timer = setTimeout(() => reject(new DriverError('timeout', 'The page did not answer in time.')), timeoutMs);
        }),
      ]).then((r) => {
        if (!r || typeof r !== 'object') throw new DriverError('not_delivered', 'no runner in the page');
        return r;
      });
    } catch (e) {
      if (e instanceof DriverError) throw e;
      const m = String((e as Error)?.message ?? e);
      if (isNoReceiver(m)) throw new DriverError('not_delivered', m);
      if (isPortClosed(m)) throw new DriverError('interrupted', m);
      throw new DriverError('interrupted', m);
    } finally {
      clearTimeout(timer);
    }
  }

  private async inject(): Promise<void> {
    try {
      await browser.scripting.executeScript({ target: { tabId: this.tabId, frameIds: [0] }, files: [RUNNER_FILE] });
    } catch (e) {
      throw new DriverError('not_delivered', `cannot run in this page: ${String((e as Error)?.message ?? e)}`);
    }
  }

  async send(cmd: Command, timeoutMs: number): Promise<CommandResult> {
    await this.tab();
    // Make sure the runner script is present in the current document (it is gone after every navigation).
    try {
      await this.rawSend({ op: 'ping' }, 1500);
    } catch (e) {
      if (e instanceof DriverError && e.kind === 'tab_closed') throw e;
      await this.inject();
      await this.rawSend({ op: 'ping' }, 1500);
    }
    return this.rawSend(cmd, timeoutMs);
  }

  async screenshot(): Promise<Blob | null> {
    const t = await this.tab();
    if (!t.active) return null; // captureVisibleTab would capture some other tab
    try {
      const dataUrl = await browser.tabs.captureVisibleTab(t.windowId, { format: 'png' });
      const res = await fetch(dataUrl);
      return await res.blob();
    } catch {
      // Needs activeTab (granted when the seller opens the popup from the toolbar) or <all_urls>.
      return null;
    }
  }

  async canAccess(origin: string): Promise<boolean> {
    try {
      return await browser.permissions.contains({ origins: [`${origin}/*`] });
    } catch {
      return false;
    }
  }
}
