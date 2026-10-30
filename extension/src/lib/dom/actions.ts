import type { StopOn, Target } from '../recipe/types';
import type { Command, CommandResult, WireFile } from '../runner/protocol';
import { normalize } from './accname';
import { findTarget, pageHasText, type FindOptions, type FindResult } from './targeting';
import { isVisible } from './visibility';

const POLL_MS = 100;

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** Polls until a unique target is found or the timeout passes; returns the last result. */
export async function waitForTarget(doc: Document, target: Target, timeoutMs: number, opts?: FindOptions): Promise<FindResult> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const r = findTarget(doc, target, opts);
    if (r.ok || r.code === 'invalid_selector' || Date.now() >= deadline) return r;
    await sleep(POLL_MS);
  }
}

async function waitUntil<T>(fn: () => T | null, timeoutMs: number): Promise<T | null> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const v = fn();
    if (v !== null || Date.now() >= deadline) return v;
    await sleep(POLL_MS);
  }
}

function isDisabled(el: Element): boolean {
  return (el as HTMLButtonElement).disabled === true || el.getAttribute('aria-disabled') === 'true';
}

const fail = (code: string, message: string): CommandResult => ({ ok: false, code, message });

/** Sets a form control's value the way typing would, so framework-controlled inputs notice. */
function setNativeValue(el: HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement, value: string): void {
  const view = el.ownerDocument.defaultView!;
  const proto =
    el instanceof view.HTMLTextAreaElement
      ? view.HTMLTextAreaElement.prototype
      : el instanceof view.HTMLSelectElement
        ? view.HTMLSelectElement.prototype
        : view.HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
  if (setter) setter.call(el, value);
  else el.value = value;
}

function fire(el: Element, type: string): void {
  const view = el.ownerDocument.defaultView!;
  const ev = type === 'input' ? new view.InputEvent('input', { bubbles: true }) : new view.Event(type, { bubbles: true });
  el.dispatchEvent(ev);
}

function decodeFile(doc: Document, f: WireFile): File {
  const bin = atob(f.b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const view = doc.defaultView!;
  return new view.File([bytes], f.name, { type: f.type });
}

export function checkStopOn(doc: Document, href: string, stopOn: StopOn[]): { ok: true; matched?: number } {
  for (let i = 0; i < stopOn.length; i++) {
    const c = stopOn[i]!;
    let hit = false;
    if ('url_matches' in c) hit = new RegExp(c.url_matches, 'u').test(href);
    else if ('text_any' in c) hit = pageHasText(doc, c.text_any) !== null;
    else if ('selector' in c) {
      try {
        hit = Array.from(doc.querySelectorAll(c.selector)).some(isVisible);
      } catch {
        hit = false;
      }
    }
    if (hit) return { ok: true, matched: i };
  }
  return { ok: true };
}

/** Executes one rendered command against the document. Reads only the targeted element. */
export async function execute(doc: Document, href: string, cmd: Command): Promise<CommandResult> {
  switch (cmd.op) {
    case 'ping':
      return { ok: true, href };
    case 'stop_check':
      return checkStopOn(doc, href, cmd.stop_on);
    case 'wait_for': {
      const r = await waitForTarget(doc, cmd.target, cmd.timeoutMs);
      return r.ok ? { ok: true } : fail(r.code, r.message);
    }
    case 'click': {
      const r = await waitForTarget(doc, cmd.target, cmd.timeoutMs);
      if (!r.ok) return fail(r.code, r.message);
      const el = r.el as HTMLElement;
      const enabled = await waitUntil(() => (isDisabled(el) ? null : true), Math.min(cmd.timeoutMs, 5000));
      if (!enabled) return fail('target_disabled', 'the element is disabled');
      el.scrollIntoView?.({ block: 'center', inline: 'center' });
      el.focus?.();
      const view = doc.defaultView!;
      for (const t of ['pointerdown', 'mousedown', 'pointerup', 'mouseup']) {
        const Ctor = t.startsWith('pointer') && view.PointerEvent ? view.PointerEvent : view.MouseEvent;
        el.dispatchEvent(new Ctor(t, { bubbles: true, cancelable: true, composed: true }));
      }
      el.click();
      return { ok: true };
    }
    case 'fill': {
      const r = await waitForTarget(doc, cmd.target, cmd.timeoutMs);
      if (!r.ok) return fail(r.code, r.message);
      const el = r.el;
      const view = doc.defaultView!;
      if (el instanceof view.HTMLInputElement || el instanceof view.HTMLTextAreaElement) {
        if (el.disabled || el.readOnly) return fail('target_disabled', 'the field is disabled or read-only');
        el.focus();
        setNativeValue(el, cmd.value);
        fire(el, 'input');
        fire(el, 'change');
        return { ok: true };
      }
      if ((el as HTMLElement).isContentEditable || el.getAttribute('contenteditable') === 'true') {
        (el as HTMLElement).focus();
        el.textContent = cmd.value;
        fire(el, 'input');
        return { ok: true };
      }
      return fail('unsupported_target', 'fill needs an <input>, <textarea> or contenteditable element');
    }
    case 'select': {
      const r = await waitForTarget(doc, cmd.target, cmd.timeoutMs);
      if (!r.ok) return fail(r.code, r.message);
      const view = doc.defaultView!;
      const el = r.el;
      if (!(el instanceof view.HTMLSelectElement)) return fail('unsupported_target', 'select needs a <select> element');
      const want = normalize(cmd.value);
      const options = Array.from(el.options);
      const byValue = options.filter((o) => o.value === cmd.value);
      const byLabel = options.filter((o) => normalize(o.textContent).toLowerCase() === want.toLowerCase());
      const matches = byValue.length ? byValue : byLabel;
      if (matches.length === 0) return fail('option_not_found', `no option ${JSON.stringify(cmd.value)}`);
      if (matches.length > 1) return fail('ambiguous_target', `${matches.length} options match ${JSON.stringify(cmd.value)}`);
      el.focus();
      setNativeValue(el, matches[0]!.value);
      fire(el, 'input');
      fire(el, 'change');
      return { ok: true };
    }
    case 'upload': {
      const r = await waitForTarget(doc, cmd.target, cmd.timeoutMs, { requireVisible: false });
      if (!r.ok) return fail(r.code, r.message);
      const view = doc.defaultView!;
      const el = r.el;
      if (!(el instanceof view.HTMLInputElement) || el.type !== 'file') return fail('unsupported_target', 'upload needs an <input type=file>');
      if (el.disabled) return fail('target_disabled', 'the file input is disabled');
      if (cmd.files.length > 1 && !el.multiple) return fail('too_many_files', 'the file input accepts one file');
      const dt = new view.DataTransfer();
      for (const f of cmd.files) dt.items.add(decodeFile(doc, f));
      el.files = dt.files;
      fire(el, 'input');
      fire(el, 'change');
      return { ok: true, detail: `${cmd.files.length} file(s)` };
    }
    case 'expect': {
      if (cmd.text_any) {
        const texts = cmd.text_any;
        const found = await waitUntil(() => pageHasText(doc, texts), cmd.timeoutMs);
        return found ? { ok: true, detail: found } : fail('expect_failed', `none of ${JSON.stringify(texts)} appeared`);
      }
      const r = await waitForTarget(doc, cmd.target!, cmd.timeoutMs);
      return r.ok ? { ok: true } : fail(r.code === 'target_not_found' ? 'expect_failed' : r.code, r.message);
    }
    case 'assert_shop_id': {
      const r = await waitForTarget(doc, cmd.target, cmd.timeoutMs);
      if (!r.ok) return fail(r.code === 'target_not_found' ? 'shop_id_not_found' : r.code, r.message);
      const text = normalize(r.el.textContent);
      const m = new RegExp(cmd.pattern, 'u').exec(text);
      const found = m?.[1];
      if (!found) return fail('shop_id_not_found', 'the shop ID pattern did not match the targeted element');
      if (found !== cmd.expected) {
        return fail('shop_mismatch', `This tab is logged in to shop ${found}, but the task is for shop ${cmd.expected}`);
      }
      return { ok: true, detail: found };
    }
  }
}
