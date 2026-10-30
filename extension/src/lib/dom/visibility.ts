/** True when the document has real layout (a browser), false in happy-dom. */
function hasLayout(doc: Document): boolean {
  const r = doc.documentElement.getBoundingClientRect();
  return r.width > 0 || r.height > 0;
}

/**
 * An element is visible when it and its ancestors are displayed, not
 * visibility:hidden, not opacity:0, not [hidden], and (in a real browser) it
 * has a non-empty box.
 */
export function isVisible(el: Element): boolean {
  if (!el.isConnected) return false;
  for (let e: Element | null = el; e; e = e.parentElement) {
    if ((e as HTMLElement).hidden) return false;
  }
  if (!el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) return false;
  if (hasLayout(el.ownerDocument)) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) return false;
  }
  return true;
}
