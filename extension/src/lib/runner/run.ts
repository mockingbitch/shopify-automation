import { ApiError, isLeaseLost, type ClaimResponse, type InputFile, type ResultBody, type StepLog } from '../api/types';
import { verifyRecipeSignature } from '../recipe/signature';
import { FILES_REF, render, TemplateError } from '../recipe/templates';
import { DEFAULT_PACE_MS, actionOf, type Recipe, type Step } from '../recipe/types';
import { formatIssues, originOf, validateRecipe } from '../recipe/validate';
import type { Command, CommandResult, WireFile } from './protocol';

/** What the service worker needs from the tab. Real implementation: chrome-tab-driver.ts. */
export interface TabDriver {
  /** URL of the run tab, or null when it cannot be read (other origin, closed). */
  url(): Promise<string | null>;
  /** Navigates the same tab and waits for the load to complete. */
  navigate(url: string, timeoutMs: number): Promise<void>;
  /** Waits until the tab is not loading (after an action that may have navigated). */
  waitIdle(timeoutMs: number): Promise<void>;
  /** Sends a command to the runner script, injecting it if needed. Throws DriverError. */
  send(cmd: Command, timeoutMs: number): Promise<CommandResult>;
  /** PNG of the visible tab, or null if the browser does not allow it. */
  screenshot(): Promise<Blob | null>;
  /** True if the extension holds host permission for the origin. */
  canAccess(origin: string): Promise<boolean>;
}

export class DriverError extends Error {
  constructor(
    /** not_delivered: the command never reached the page. interrupted: the page went away mid-command. */
    readonly kind: 'not_delivered' | 'interrupted' | 'tab_closed' | 'timeout',
    message: string,
  ) {
    super(message);
  }
}

export interface RunApi {
  heartbeat(state: 'RUNNING' | 'WAITING_USER', message?: string): Promise<unknown>;
  screenshot(png: Blob, name: string): Promise<string>;
  result(body: Omit<ResultBody, 'lease_token'>): Promise<void>;
}

export type RunPhase = 'preparing' | 'running' | 'paused' | 'succeeded' | 'failed' | 'lease_lost' | 'stopped';

export interface RunState {
  taskId: string;
  title: string;
  phase: RunPhase;
  stepIndex: number;
  stepCount: number;
  message?: string;
  errorCode?: string;
  pauseReason?: 'verification' | 'unexpected_page';
}

export interface RunDeps {
  driver: TabDriver;
  api: RunApi;
  publicKeys: Record<string, string>;
  allowLocalHttp: boolean;
  fetchFile: (f: InputFile) => Promise<ArrayBuffer>;
  onState: (s: RunState) => void;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
  heartbeatEveryMs?: number;
}

export const DEFAULT_STEP_TIMEOUT_MS = 10_000;
export const NAVIGATION_TIMEOUT_MS = 30_000;
const MAX_FILE_BYTES = 10 * 1024 * 1024;

class RunFailure extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}
class LeaseLost extends Error {}
class StoppedByUser extends Error {}

const READ_OPS = new Set(['wait_for', 'expect', 'assert_shop_id']);

function toBase64(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = '';
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

async function sha256Hex(buf: ArrayBuffer): Promise<string> {
  const d = new Uint8Array(await crypto.subtle.digest('SHA-256', buf));
  return Array.from(d, (b) => b.toString(16).padStart(2, '0')).join('');
}

/** sha256 as lowercase/uppercase hex (expected) or base64 of the digest (tolerated). */
function checksumMatches(expected: string, hex: string): boolean {
  const e = expected.trim();
  if (/^[0-9a-fA-F]{64}$/.test(e)) return e.toLowerCase() === hex;
  const bytes = hex.match(/../g)!.map((h) => parseInt(h, 16));
  return btoa(String.fromCharCode(...bytes)) === e;
}

/**
 * One task run, orchestrated from the service worker. The run state (step
 * index, logs, pause) lives here, so a page navigation, which reloads the
 * runner script, does not lose progress: the driver re-injects the script and
 * the next step continues.
 */
export class TaskRun {
  private state: RunState;
  private stopped = false;
  private resume: ((v: 'continue' | 'stop') => void) | null = null;
  private leaseLost = false;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private readonly sleep: (ms: number) => Promise<void>;
  private readonly now: () => number;
  private logs: StepLog[] = [];
  private screenshotIds: string[] = [];
  private recipe: Recipe | null = null;

  constructor(
    readonly taskId: string,
    title: string,
    private readonly claim: ClaimResponse,
    private readonly deps: RunDeps,
  ) {
    this.sleep = deps.sleep ?? ((ms) => new Promise((r) => setTimeout(r, ms)));
    this.now = deps.now ?? Date.now;
    this.state = { taskId, title, phase: 'preparing', stepIndex: 0, stepCount: 0 };
  }

  get current(): RunState {
    return { ...this.state };
  }

  private set(patch: Partial<RunState>): void {
    this.state = { ...this.state, ...patch };
    if (patch.phase && patch.phase !== 'paused') delete this.state.pauseReason;
    this.deps.onState(this.current);
  }

  /** Seller clicked Stop. */
  stop(): void {
    this.stopped = true;
    this.resume?.('stop');
  }

  /** Seller clicked Continue after finishing the page. */
  continue(): void {
    this.resume?.('continue');
  }

  private async heartbeat(state: 'RUNNING' | 'WAITING_USER', message?: string): Promise<void> {
    try {
      await this.deps.api.heartbeat(state, message);
    } catch (e) {
      if (isLeaseLost(e)) {
        this.leaseLost = true;
        this.resume?.('stop');
        throw new LeaseLost();
      }
      // A missed heartbeat is not fatal; the next one (or the result) will tell.
    }
  }

  private checkAbort(): void {
    if (this.leaseLost) throw new LeaseLost();
    if (this.stopped) throw new StoppedByUser();
  }

  /** Runs the whole task and reports the result. Never throws. */
  async run(): Promise<RunState> {
    this.set({ phase: 'preparing', message: 'Checking the recipe…' });
    const every = this.deps.heartbeatEveryMs ?? 60_000;
    this.heartbeatTimer = setInterval(() => {
      const s = this.state.phase === 'paused' ? 'WAITING_USER' : 'RUNNING';
      this.heartbeat(s).catch(() => undefined);
    }, every);
    try {
      const { recipe, files } = await this.prepare();
      this.recipe = recipe;
      await this.execute(recipe, files);
      await this.finish({ status: 'SUCCEEDED' });
      this.set({ phase: 'succeeded', message: 'Done. The change was applied and verified.' });
    } catch (e) {
      await this.handleError(e);
    } finally {
      if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    }
    return this.current;
  }

  private async handleError(e: unknown): Promise<void> {
    if (e instanceof LeaseLost || this.leaseLost || isLeaseLost(e)) {
      this.set({ phase: 'lease_lost', message: 'This task was taken over.', errorCode: 'lease_lost' });
      return;
    }
    let code = 'step_failed';
    let message = String((e as Error)?.message ?? e);
    if (e instanceof StoppedByUser) {
      code = 'stopped_by_user';
      message = 'Stopped by the seller.';
    } else if (e instanceof RunFailure) {
      code = e.code;
    } else if (e instanceof DriverError) {
      code = e.kind === 'tab_closed' ? 'tab_closed' : 'navigation_interrupted';
    } else if (e instanceof ApiError) {
      code = e.code;
    }
    // Screenshot on failure, only once the recipe was trusted and we are on an allowed page.
    if (this.recipe && code !== 'stopped_by_user') await this.takeScreenshot('failure').catch(() => undefined);
    try {
      await this.finish({ status: 'FAILED', error_code: code, message: message.slice(0, 500) });
    } catch (err) {
      if (isLeaseLost(err)) {
        this.set({ phase: 'lease_lost', message: 'This task was taken over.', errorCode: 'lease_lost' });
        return;
      }
    }
    this.set({ phase: code === 'stopped_by_user' ? 'stopped' : 'failed', message, errorCode: code });
  }

  private async finish(r: { status: 'SUCCEEDED' | 'FAILED'; error_code?: string; message?: string }): Promise<void> {
    await this.deps.api.result({
      ...r,
      steps: this.logs,
      ...(this.screenshotIds.length ? { screenshot_ids: this.screenshotIds } : {}),
    });
  }

  // ---- preparation: signature → parse → schema + rules → task match → inputs

  private async prepare(): Promise<{ recipe: Recipe; files: Map<string, WireFile[]> }> {
    const { recipe: signed, task, inputs } = this.claim;
    const sig = await verifyRecipeSignature(signed?.json, signed?.signature, signed?.key_id, this.deps.publicKeys);
    if (!sig.ok) throw new RunFailure(sig.code, sig.message);
    let parsed: unknown;
    try {
      parsed = JSON.parse(signed.json);
    } catch {
      throw new RunFailure('recipe_invalid', 'recipe is not valid JSON');
    }
    const v = validateRecipe(parsed, { allowLocalHttp: this.deps.allowLocalHttp });
    if (!v.ok) throw new RunFailure('recipe_invalid', formatIssues(v.issues));
    const recipe = v.recipe;
    if (recipe.capability !== task.capability) {
      throw new RunFailure('recipe_mismatch', `recipe is for ${recipe.capability}, task is ${task.capability}`);
    }
    if (recipe.market !== String(task.region).toUpperCase()) {
      throw new RunFailure('recipe_mismatch', `recipe is for market ${recipe.market}, task is ${task.region}`);
    }
    for (const o of recipe.allowed_origins) {
      if (!(await this.deps.driver.canAccess(o))) {
        throw new RunFailure('origin_not_permitted', `this build of the extension has no access to ${o}`);
      }
    }
    // Inputs.
    const files = new Map<string, WireFile[]>();
    for (const [name, decl] of Object.entries(recipe.inputs)) {
      const given = inputs?.[name];
      const required = decl.required ?? true;
      if (!given) {
        if (required) throw new RunFailure('missing_input', `required input "${name}" was not provided`);
        continue;
      }
      if (given.type !== decl.type) throw new RunFailure('missing_input', `input "${name}" should be ${decl.type}`);
      if (given.type === 'files') {
        if (required && given.files.length === 0) throw new RunFailure('missing_input', `required input "${name}" has no files`);
        const wire: WireFile[] = [];
        for (const f of given.files) {
          let buf: ArrayBuffer;
          try {
            buf = await this.deps.fetchFile(f);
          } catch (e) {
            throw new RunFailure('input_fetch_failed', `could not download ${f.name}: ${String((e as Error)?.message ?? e)}`);
          }
          if (buf.byteLength > MAX_FILE_BYTES) throw new RunFailure('input_fetch_failed', `${f.name} is larger than 10 MB`);
          if (f.sha256) {
            const hex = await sha256Hex(buf);
            if (!checksumMatches(f.sha256, hex)) throw new RunFailure('input_checksum_mismatch', `${f.name} does not match its checksum`);
          }
          wire.push({ name: f.name, type: f.content_type || 'application/octet-stream', b64: toBase64(buf) });
        }
        files.set(name, wire);
      }
    }
    return { recipe, files };
  }

  // ---- execution

  private async execute(recipe: Recipe, files: Map<string, WireFile[]>): Promise<void> {
    const all: Step[] = [...recipe.steps, ...(recipe.verify ?? [])];
    this.logs = all.map((s, index) => ({ index, action: actionOf(s), outcome: 'skipped', duration_ms: 0 }));
    this.set({ phase: 'running', stepCount: all.length, stepIndex: 0, message: 'Starting…' });
    await this.heartbeat('RUNNING');

    const pace = recipe.pace_ms ?? DEFAULT_PACE_MS;
    const shopId = String(this.claim.task.shopee_shop_id);
    const text: Record<string, string> = {};
    for (const [k, v] of Object.entries(this.claim.inputs ?? {})) if (v.type === 'text') text[k] = v.value;
    const required = (n: string) => recipe.inputs[n]?.required ?? true;
    const ctx = { shopeeShopId: shopId, text };
    const allowed = new Set(recipe.allowed_origins);

    // Act only in an allowed tab: open start_url in the same tab if needed.
    const startOrigin = originOf((await this.deps.driver.url()) ?? '');
    if (!startOrigin || !allowed.has(startOrigin)) {
      this.set({ message: 'Opening Seller Centre in this tab…' });
      await this.deps.driver.navigate(recipe.start_url, NAVIGATION_TIMEOUT_MS);
    }

    let asserted = false;
    for (let i = 0; i < all.length; i++) {
      const step = all[i]!;
      const action = actionOf(step);
      this.checkAbort();
      await this.beforeAction(recipe, allowed);
      this.set({ phase: 'running', stepIndex: i, message: `Step ${i + 1} of ${all.length}: ${action}` });
      const started = this.now();
      try {
        if ((action === 'click' || action === 'fill' || action === 'select' || action === 'upload') && !asserted) {
          // Validation guarantees this never happens; keep the runtime guard anyway.
          throw new RunFailure('shop_not_asserted', 'refusing to change the page before the shop ID was checked');
        }
        const skipped = await this.runStep(step, ctx, required, files, allowed, shopId);
        if (action === 'assert_shop_id') asserted = true;
        this.logs[i] = { index: i, action, outcome: skipped ? 'skipped' : 'ok', duration_ms: this.now() - started };
      } catch (e) {
        if (!(e instanceof LeaseLost) && !(e instanceof StoppedByUser)) {
          const msg = String((e as Error)?.message ?? e).slice(0, 300);
          this.logs[i] = { index: i, action, outcome: 'failed', duration_ms: this.now() - started, error: msg };
        }
        throw e;
      }
      if (i < all.length - 1) {
        await this.sleep(pace);
        await this.deps.driver.waitIdle(NAVIGATION_TIMEOUT_MS);
      }
    }
  }

  /** Checks for an unexpected page or a stop_on match before every action; pauses until the seller continues. */
  private async beforeAction(recipe: Recipe, allowed: Set<string>): Promise<void> {
    for (;;) {
      this.checkAbort();
      await this.deps.driver.waitIdle(NAVIGATION_TIMEOUT_MS);
      const url = await this.deps.driver.url();
      const origin = url ? originOf(url) : null;
      let reason: RunState['pauseReason'] | null = null;
      let message = '';
      if (!origin || !allowed.has(origin)) {
        reason = 'unexpected_page';
        message = 'This tab left Seller Centre. Finish what the page asks (for example, sign in), return to Seller Centre in this tab, then click Continue.';
      } else {
        const r = await this.sendWithRetry({ op: 'stop_check', stop_on: recipe.stop_on }, DEFAULT_STEP_TIMEOUT_MS, true);
        if (r.ok && r.matched !== undefined) {
          reason = 'verification';
          message = 'Seller Centre is asking for a login or verification. Please finish it in the tab, then click Continue.';
        }
      }
      if (!reason) return;
      await this.pause(reason, message);
    }
  }

  private async pause(reason: NonNullable<RunState['pauseReason']>, message: string): Promise<void> {
    this.set({ phase: 'paused', pauseReason: reason, message });
    await this.heartbeat('WAITING_USER', reason === 'verification' ? 'Waiting for the seller to finish verification' : 'Waiting for the seller to return to Seller Centre');
    const choice = await new Promise<'continue' | 'stop'>((resolve) => {
      this.resume = resolve;
      if (this.stopped || this.leaseLost) resolve('stop');
    });
    this.resume = null;
    this.checkAbort();
    if (choice === 'stop') throw new StoppedByUser();
    this.set({ phase: 'running', message: 'Continuing…' });
    await this.heartbeat('RUNNING');
  }

  private async sendWithRetry(cmd: Command, timeoutMs: number, readOnly: boolean): Promise<CommandResult> {
    const deadline = this.now() + timeoutMs + 5_000;
    let attempts = 0;
    for (;;) {
      try {
        return await this.deps.driver.send(cmd, timeoutMs + 5_000);
      } catch (e) {
        if (!(e instanceof DriverError) || e.kind === 'tab_closed') throw e;
        attempts++;
        const retriable = e.kind === 'not_delivered' || (readOnly && e.kind === 'interrupted');
        if (!retriable || this.now() >= deadline || attempts > 20) throw e;
        // The page is probably navigating: wait for it to settle, then the driver re-injects.
        await this.sleep(250);
        await this.deps.driver.waitIdle(NAVIGATION_TIMEOUT_MS);
      }
    }
  }

  private expectOk(r: CommandResult): CommandResult & { ok: true } {
    if (!r.ok) throw new RunFailure(r.code, r.message);
    return r;
  }

  /** Runs one step. Returns true when the step was skipped (optional input absent). */
  private async runStep(
    step: Step,
    ctx: { shopeeShopId: string; text: Record<string, string> },
    required: (n: string) => boolean,
    files: Map<string, WireFile[]>,
    allowed: Set<string>,
    shopId: string,
  ): Promise<boolean> {
    const timeoutMs = step.timeout_ms ?? DEFAULT_STEP_TIMEOUT_MS;
    const r = (t: string) => {
      try {
        return render(t, ctx, required);
      } catch (e) {
        if (e instanceof TemplateError) throw new RunFailure(e.code, e.message);
        throw e;
      }
    };
    if ('goto' in step) {
      const url = r(step.goto.url);
      const o = originOf(url);
      if (!o || !allowed.has(o)) throw new RunFailure('origin_not_allowed', `refusing to open ${o ?? 'an invalid URL'}`);
      await this.deps.driver.navigate(url, step.timeout_ms ?? NAVIGATION_TIMEOUT_MS);
      return false;
    }
    if ('screenshot' in step) {
      await this.takeScreenshot(step.screenshot.name);
      return false;
    }
    let cmd: Command;
    if ('wait_for' in step) cmd = { op: 'wait_for', target: step.wait_for, timeoutMs };
    else if ('click' in step) cmd = { op: 'click', target: step.click, timeoutMs };
    else if ('fill' in step) cmd = { op: 'fill', target: step.fill.target, value: r(step.fill.value), timeoutMs };
    else if ('select' in step) cmd = { op: 'select', target: step.select.target, value: r(step.select.value), timeoutMs };
    else if ('upload' in step) {
      const name = FILES_REF.exec(step.upload.files)![1]!;
      const list = files.get(name);
      if (!list || list.length === 0) return true; // optional input not given
      cmd = { op: 'upload', target: step.upload.target, files: list, timeoutMs };
    } else if ('expect' in step) {
      cmd = 'text_any' in step.expect ? { op: 'expect', text_any: step.expect.text_any, timeoutMs } : { op: 'expect', target: step.expect.target, timeoutMs };
    } else if ('assert_shop_id' in step) {
      cmd = { op: 'assert_shop_id', target: step.assert_shop_id.target, pattern: step.assert_shop_id.pattern, expected: shopId, timeoutMs };
    } else throw new RunFailure('recipe_invalid', 'unknown action');
    this.expectOk(await this.sendWithRetry(cmd, timeoutMs, READ_OPS.has(cmd.op)));
    return false;
  }

  private async takeScreenshot(name: string): Promise<void> {
    // Only ever capture the run tab while it shows an allowed Seller Centre page.
    const url = await this.deps.driver.url();
    const origin = url ? originOf(url) : null;
    if (!this.recipe || !origin || !this.recipe.allowed_origins.includes(origin)) return;
    const png = await this.deps.driver.screenshot();
    if (!png) return;
    try {
      this.screenshotIds.push(await this.deps.api.screenshot(png, name));
    } catch (e) {
      if (isLeaseLost(e)) {
        this.leaseLost = true;
        throw new LeaseLost();
      }
    }
  }
}
