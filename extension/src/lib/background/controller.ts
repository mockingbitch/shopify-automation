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
  notice = 'Trình duyệt này không còn được kết nối. Hãy ghép nối lại bằng mã mới từ trang quản lý.';
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
  const base = browser.runtime.getManifest().name;
  const hint = paused ? 'Chờ bạn xử lý trong tab Kênh Người Bán' : queued > 0 ? `${queued.toLocaleString('vi-VN')} việc đang chờ chạy` : '';
  await browser.action.setTitle({ title: hint ? `${base}: ${hint}` : base });
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
    const view: TasksView = { ...prev, error: e instanceof ApiError ? e.message : `Có lỗi không mong muốn (${String(e)}).` };
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
  task_unavailable: 'Việc này không còn để chạy (có thể một trình duyệt khác đã nhận). Bấm Làm mới để xem danh sách mới.',
  recipe_disabled: 'Chức năng tự động này đang tạm tắt. Hãy thử lại sau hoặc làm thủ công.',
  capability_disabled: 'Chức năng tự động này đang tạm tắt. Hãy thử lại sau hoặc làm thủ công.',
  network_error: 'Không kết nối được máy chủ. Hãy kiểm tra mạng, rồi thử lại.',
};

async function startRun(taskId: string): Promise<Reply> {
  if (activeRun) return err('busy', 'Đang có một việc khác chạy. Hãy bấm Dừng hoặc chờ việc đó xong.');
  const tasks = await getTasks();
  const summary = tasks.items.find((t) => t.id === taskId);
  // The tab the seller has open in the focused window: the run never opens a new tab.
  const [tab] = await browser.tabs.query({ active: true, lastFocusedWindow: true });
  if (!tab?.id || (tab.url ?? '').startsWith(browser.runtime.getURL(''))) {
    return err('no_tab', 'Hãy mở tab Kênh Người Bán của shop, rồi bấm Chạy lại.');
  }
  let claim;
  try {
    claim = await api.claim(taskId);
  } catch (e) {
    await refreshTasks();
    if (e instanceof ApiError) return err(e.code, CLAIM_MESSAGES[e.code] ?? e.message);
    return err('claim_failed', `Không nhận được việc này (${String(e)}). Hãy thử lại.`);
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
      if (!res.ok) throw new Error(`HTTP ${res.status}`); // technical: goes to the API log
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
      if (!code) return err('invalid_code_format', 'Mã gồm 10 chữ cái hoặc chữ số, ví dụ ABCDE-12345.');
      const name = req.deviceName.trim().slice(0, 100) || 'Trình duyệt';
      try {
        const r = await api.pair(code, name, browser.runtime.getManifest().version);
        await local.set({ auth: { token: r.token, expiresAt: r.expires_at, device: r.device, org: r.org, user: r.user, rotatedAt: Date.now() } satisfies StoredAuth });
        notice = undefined;
        await refreshTasks();
        return { ok: true, data: await status() };
      } catch (e) {
        if (e instanceof ApiError && e.code === 'invalid_code') return err('invalid_code', 'Mã này sai, đã hết hạn hoặc đã được dùng. Hãy tạo mã mới trên trang quản lý.');
        if (e instanceof ApiError) return err(e.code, e.code === 'network_error' ? CLAIM_MESSAGES.network_error! : e.message);
        return err('pair_failed', `Không ghép nối được (${String(e)}). Hãy thử lại.`);
      }
    }
    case 'unpair': {
      if (activeRun) return err('busy', 'Hãy dừng việc đang chạy trước.');
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
      message: 'Lượt chạy bị gián đoạn vì trình duyệt đã tạm dừng Tiện ích. Việc sẽ trở lại danh sách chờ trong vòng 10 phút.',
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
