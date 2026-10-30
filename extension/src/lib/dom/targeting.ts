import type { Target } from '../recipe/types';
import { accessibleName, normalize, roleOf } from './accname';
import { isVisible } from './visibility';

export type FindResult =
  | { ok: true; el: Element }
  | { ok: false; code: 'target_not_found' | 'ambiguous_target' | 'invalid_selector'; message: string };

export interface FindOptions {
  /** Default true. File inputs are commonly hidden behind a styled label, so upload passes false. */
  requireVisible?: boolean;
}

export function describeTarget(t: Target): string {
  const parts: string[] = [];
  if (t.role) parts.push(`role=${t.role}`);
  if (t.name_any) parts.push(`name∈${JSON.stringify(t.name_any)}`);
  if (t.text_any) parts.push(`text∈${JSON.stringify(t.text_any)}`);
  if (t.selector) parts.push(`selector=${JSON.stringify(t.selector)}`);
  return parts.join(' ');
}

const lc = (s: string) => normalize(s).toLowerCase();

/**
 * Finds exactly one element for a target. Every given field must match
 * (role AND name AND text AND selector). More than one match is an error:
 * the runner never guesses.
 */
export function findTarget(doc: Document, target: Target, opts: FindOptions = {}): FindResult {
  const requireVisible = opts.requireVisible ?? true;
  let candidates: Element[];
  try {
    candidates = Array.from(target.selector ? doc.querySelectorAll(target.selector) : doc.querySelectorAll('body *'));
  } catch {
    return { ok: false, code: 'invalid_selector', message: `invalid CSS selector ${JSON.stringify(target.selector)}` };
  }
  if (target.role) candidates = candidates.filter((el) => roleOf(el) === target.role);
  if (target.name_any) {
    const names = target.name_any.map(lc);
    candidates = candidates.filter((el) => names.includes(lc(accessibleName(el))));
  }
  if (target.text_any) {
    const texts = target.text_any.map(lc);
    candidates = candidates.filter((el) => {
      const t = lc(el.textContent ?? '');
      return texts.some((x) => t.includes(x));
    });
    // Text alone matches every ancestor too: keep only the innermost matches.
    if (!target.role && !target.selector && !target.name_any) {
      candidates = candidates.filter((el) => !candidates.some((o) => o !== el && el.contains(o)));
    }
  }
  if (requireVisible) candidates = candidates.filter(isVisible);
  if (candidates.length === 1) return { ok: true, el: candidates[0]! };
  if (candidates.length === 0) return { ok: false, code: 'target_not_found', message: `no ${requireVisible ? 'visible ' : ''}element for ${describeTarget(target)}` };
  return { ok: false, code: 'ambiguous_target', message: `${candidates.length} elements match ${describeTarget(target)}` };
}

/** Visible text of the page, used only locally for text_any checks; never sent anywhere. */
export function visibleText(doc: Document): string {
  const body = doc.body as HTMLElement | null;
  if (!body) return '';
  return lc(body.innerText ?? body.textContent ?? '');
}

export function pageHasText(doc: Document, texts: string[]): string | null {
  const page = visibleText(doc);
  for (const t of texts) if (page.includes(lc(t))) return t;
  return null;
}
