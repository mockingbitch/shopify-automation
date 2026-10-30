import { browser } from 'wxt/browser';
import { ExtApi } from '../api/client';
import { ApiError, type Device, type Org, type TaskSummary, type User } from '../api/types';
import { config } from '../config';
import type { AuthView, Reply, Request, Status, TasksView } from '../messages';
import { normalizePairingCode } from '../pairing';
import { ChromeTabDriver } from '../runner/chrome-tab-driver';
import { TaskRun, type RunState } from '../runner/run';

interface StoredAuth {
  token: string;
  expiresAt: string;
  device: Device;
  org: Org;
  user: User;
  rotatedAt: number;
}

const ROTATE_EVERY_MS = 24 * 60 * 60 * 1000;
const POLL_ALARM = 'poll-tasks';

const local = browser.storage.local;
const session = browser.storage.session;

async function getAuth(): Promise<StoredAuth | null> {
  const r = await local.get('auth');
  return (r.auth as StoredAuth | undefined) ?? null;
}

let activeRun: TaskRun | null = null;
let keepAlive: ReturnType<typeof setInterval> | null = null;
let notice: string | undefined;

async function clearAuth(): Promise<void> {
  await local.remove('auth');
  await session.set({ tasks: { items: [], fetchedAt: null } satisfies TasksView });
  notice = 'This browser is no longer connected. Pair it again from the dashboard.';
  await updateBadge([]);
}

export const api = new ExtApi({
  baseUrl: config.apiBase,
  getToken: async () => (await getAuth())?.token ?? null,
  onUnauthorized: clearAuth,
});

async function updateBadge(items: TaskSummary[], run?: RunState | null): Promise<void> {
  const queued = items.filter((t) => t.status === 'QUEUED').length;
  const paused = run?.phase === 'paused';
  await browser.action.setBadgeBackgroundColor({ color: paused ? '#b45309' : '#c2410c' });
  await browser.action.setBadgeText({ text: paused ? '!' : queued > 0 ? String(queued) : '' });
}

async function getTasks(): Promise<TasksView> {
  const r = await session.get('tasks');
  return (r.tasks as TasksView | undefined) ?? { items: [], fetchedAt: null };
}

async function getRun(): Promise<RunState | null> {
  const r = await session.get('run');
  return (r.run as RunState | undefined) ?? null;
}

async function setRun(run: RunState | null): Promise<void> {
  await session.set({ run });
  await updateBadge((await getTasks()).items, run);
}

export async function refreshTasks(): Promise<TasksView> {
  if (!(await getAuth())) return { items: [], fetchedAt: null };
  try {
    const items = await api.tasks();
    const view: TasksView = { items, fetchedAt: Date.now() };
    await session.set({ tasks: view });
    await updateBadge(items, await getRun());
    return view;
  } catch (e) {
    const prev = await getTasks();
    const view: TasksView = { ...prev, error: e instanceof ApiError ? e.message : String(e) };
    if (await getAuth()) await session.set({ tasks: view });
    return view;
  }
}

async function rotateIfDue(): Promise<void> {
  const auth = await getAuth();
  // Never rotate during a run: in-flight calls would still carry the old token.
  if (!auth || activeRun || Date.now() - auth.rotatedAt < ROTATE_EVERY_MS) return;
  try {
    const r = await api.rotate();
    await local.set({ auth: { ...auth, token: r.token, expiresAt: r.expires_at, rotatedAt: Date.now() } satisfies StoredAuth });
  } catch {
    /* try again on the next alarm */
  }
}

async function status(): Promise<Status> {
  const auth = await getAuth();
  const view: AuthView | null = auth ? { org: auth.org, user: auth.user, deviceName: auth.device?.name ?? '' } : null;
  return { auth: view, tasks: await getTasks(), run: await getRun(), ...(notice ? { notice } : {}) };
}

function err(code: string, message: string): Reply<never> {
  return { ok: false, error: { code, message } };
}

const CLAIM_MESSAGES: Record<string, string> = {
  task_unavailable: 'This task is no longer available (another browser may have taken it).',
  recipe_disabled: 'This automation is switched off right now. Try again later or do it manually.',
  capability_disabled: 'This automation is switched off right now. Try again later or do it manually.',
};

async function startRun(taskId: string): Promise<Reply> {
  if (activeRun) return err('busy', 'Another task is running. Stop it or wait for it to finish.');
  const tasks = await getTasks();
  const summary = tasks.items.find((t) => t.id === taskId);
  // The tab the seller has open in the focused window: the run never opens a new tab.
  const [tab] = await browser.tabs.query({ active: true, lastFocusedWindow: true });
  if (!tab?.id || (tab.url ?? '').startsWith(browser.runtime.getURL(''))) {
    return err('no_tab', 'Open your Seller Centre tab, then click Run again.');
  }
  let claim;
  try {
    claim = await api.claim(taskId);
  } catch (e) {
    await refreshTasks();
    if (e instanceof ApiError) return err(e.code, CLAIM_MESSAGES[e.code] ?? e.message);
    return err('claim_failed', String(e));
  }
  const lease = claim.lease_token;
  const run = new TaskRun(taskId, summary?.title ?? taskId, claim, {
    driver: new ChromeTabDriver(tab.id),
    publicKeys: config.recipePublicKeys,
    allowLocalHttp: config.testBuild,
    api: {
      heartbeat: (state, message) => api.heartbeat(taskId, lease, state, message),
      screenshot: (png, name) => api.screenshot(taskId, lease, png, name),
      result: (body) => api.result(taskId, { lease_token: lease, ...body }),
      release: () => api.release(taskId, lease),
    },
    fetchFile: async (f) => {
      const res = await fetch(f.url, { credentials: 'omit', cache: 'no-store' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return res.arrayBuffer();
    },
    onState: (s) => void setRun(s),
  });
  activeRun = run;
  notice = undefined;
  // Keep the service worker alive while a run (including a pause) is in progress.
  keepAlive = setInterval(() => void browser.runtime.getPlatformInfo(), 20_000);
  void run.run().finally(async () => {
    if (keepAlive) clearInterval(keepAlive);
    keepAlive = null;
    activeRun = null;
    await setRun(run.current);
    await refreshTasks();
  });
  return { ok: true, data: run.current };
}

export async function handle(req: Request): Promise<Reply> {
  switch (req.type) {
    case 'status':
      return { ok: true, data: await status() };
    case 'pair': {
      const code = normalizePairingCode(req.code);
      if (!code) return err('invalid_code_format', 'The code has 10 letters or digits, like ABCDE-12345.');
      const name = req.deviceName.trim().slice(0, 100) || 'Browser';
      try {
        const r = await api.pair(code, name, browser.runtime.getManifest().version);
        await local.set({ auth: { token: r.token, expiresAt: r.expires_at, device: r.device, org: r.org, user: r.user, rotatedAt: Date.now() } satisfies StoredAuth });
        notice = undefined;
        await refreshTasks();
        return { ok: true, data: await status() };
      } catch (e) {
        if (e instanceof ApiError && e.code === 'invalid_code') return err('invalid_code', 'This code is wrong, expired or already used. Create a new one in the dashboard.');
        if (e instanceof ApiError) return err(e.code, e.message);
        return err('pair_failed', String(e));
      }
    }
    case 'unpair': {
      if (activeRun) return err('busy', 'Stop the running task first.');
      try {
        await api.unpair();
      } catch {
        /* forget locally even if the server call fails */
      }
      await clearAuth();
      notice = undefined;
      await session.set({ run: null });
      return { ok: true, data: await status() };
    }
    case 'refresh':
      await refreshTasks();
      return { ok: true, data: await status() };
    case 'run':
      return startRun(req.taskId);
    case 'stop':
      activeRun?.stop();
      return { ok: true, data: null };
    case 'continue':
      activeRun?.continue();
      return { ok: true, data: null };
    case 'dismiss':
      if (!activeRun) await setRun(null);
      return { ok: true, data: null };
  }
}

/** On a fresh service worker: a run that was in progress cannot be resumed safely. */
export async function recoverAfterRestart(): Promise<void> {
  const run = await getRun();
  if (run && !activeRun && ['preparing', 'running', 'paused'].includes(run.phase)) {
    await setRun({
      ...run,
      phase: 'failed',
      errorCode: 'interrupted',
      message: 'The run was interrupted (the browser stopped the extension). The task goes back to the queue within 10 minutes.',
    });
  }
}

export async function onAlarm(name: string): Promise<void> {
  if (name !== POLL_ALARM) return;
  await refreshTasks();
  await rotateIfDue();
}

export async function ensureAlarm(): Promise<void> {
  const a = await browser.alarms.get(POLL_ALARM);
  if (!a) await browser.alarms.create(POLL_ALARM, { periodInMinutes: 1, delayInMinutes: 1 });
}
