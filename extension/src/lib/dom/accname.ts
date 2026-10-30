import type { Role } from '../recipe/types';

/**
 * A practical approximation of ARIA roles and accessible names. It covers the
 * explicit role attribute and the implicit roles of common HTML elements; the
 * name comes from aria-labelledby, aria-label, <label>, alt, value of input
 * buttons, text content (for roles that take their name from content), title
 * and placeholder, in that order.
 */

export function normalize(s: string | null | undefined): string {
  return (s ?? '').replace(/\s+/g, ' ').trim();
}

const TEXTBOX_TYPES = new Set(['', 'text', 'email', 'tel', 'url', 'search', 'password', 'number']);

export function implicitRole(el: Element): Role | null {
  const tag = el.tagName.toLowerCase();
  switch (tag) {
    case 'button':
      return 'button';
    case 'a':
    case 'area':
      return el.hasAttribute('href') ? 'link' : null;
    case 'textarea':
      return 'textbox';
    case 'select': {
      const s = el as HTMLSelectElement;
      return s.multiple || s.size > 1 ? null : 'combobox';
    }
    case 'option':
      return 'option';
    case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6':
      return 'heading';
    case 'img':
      return el.getAttribute('alt') === '' ? null : 'img';
    case 'dialog':
      return 'dialog';
    case 'input': {
      const type = (el.getAttribute('type') ?? '').toLowerCase();
      if (['button', 'submit', 'reset', 'image'].includes(type)) return 'button';
      if (type === 'checkbox') return 'checkbox';
      if (type === 'radio') return 'radio';
      if (TEXTBOX_TYPES.has(type)) return el.hasAttribute('list') ? 'combobox' : 'textbox';
      return null;
    }
    default:
      return null;
  }
}

export function roleOf(el: Element): string | null {
  const explicit = el.getAttribute('role');
  if (explicit) return explicit.trim().split(/\s+/)[0]!.toLowerCase();
  return implicitRole(el);
}

const NAME_FROM_CONTENT = new Set(['button', 'link', 'heading', 'tab', 'menuitem', 'option', 'checkbox', 'radio']);

function textOf(el: Element): string {
  // textContent of the target only (never the page): includes img alt via a small walk.
  let out = '';
  for (const n of Array.from(el.childNodes)) {
    if (n.nodeType === 3) out += n.textContent ?? '';
    else if (n.nodeType === 1) {
      const c = n as Element;
      if ((c as HTMLElement).hidden || c.getAttribute('aria-hidden') === 'true') continue;
      if (c.tagName === 'IMG') out += ` ${c.getAttribute('alt') ?? ''} `;
      else out += ` ${textOf(c)} `;
    }
  }
  return out;
}

function labelsOf(el: Element): string {
  const doc = el.ownerDocument;
  const parts: string[] = [];
  const id = el.getAttribute('id');
  if (id) {
    for (const l of Array.from(doc.querySelectorAll('label'))) {
      if (l.getAttribute('for') === id) parts.push(textOf(l));
    }
  }
  const wrap = el.closest('label');
  if (wrap) parts.push(textOf(wrap));
  return normalize(parts.join(' '));
}

export function accessibleName(el: Element): string {
  const doc = el.ownerDocument;
  const labelledby = el.getAttribute('aria-labelledby');
  if (labelledby) {
    const t = labelledby
      .split(/\s+/)
      .map((id) => doc.getElementById(id))
      .filter((e): e is HTMLElement => !!e)
      .map((e) => textOf(e))
      .join(' ');
    if (normalize(t)) return normalize(t);
  }
  const aria = normalize(el.getAttribute('aria-label'));
  if (aria) return aria;
  const tag = el.tagName.toLowerCase();
  if (['input', 'textarea', 'select'].includes(tag)) {
    const type = (el.getAttribute('type') ?? '').toLowerCase();
    if (tag === 'input' && ['button', 'submit', 'reset'].includes(type)) {
      const v = normalize((el as HTMLInputElement).value);
      if (v) return v;
    }
    if (tag === 'input' && type === 'image') {
      const alt = normalize(el.getAttribute('alt'));
      if (alt) return alt;
    }
    const label = labelsOf(el);
    if (label) return label;
  }
  if (tag === 'img' || tag === 'area') {
    const alt = normalize(el.getAttribute('alt'));
    if (alt) return alt;
  }
  const role = roleOf(el);
  if (role && NAME_FROM_CONTENT.has(role)) {
    const t = normalize(textOf(el));
    if (t) return t;
  }
  return normalize(el.getAttribute('title')) || normalize(el.getAttribute('placeholder'));
}
