import { beforeEach, describe, expect, it } from 'vitest';
import { accessibleName, roleOf } from '@/lib/dom/accname';
import { findTarget } from '@/lib/dom/targeting';

const html = (s: string) => {
  document.body.innerHTML = s;
};

describe('roles and accessible names', () => {
  it('computes implicit roles', () => {
    html(`<button id=a>x</button><a id=b href="#">y</a><a id=c>z</a><input id=d><input id=e type=checkbox><select id=f></select><h2 id=g>t</h2><img id=h alt="Logo"><img id=i alt=""><div id=j role="tab">k</div><input id=k type=submit value="Go">`);
    const r = (id: string) => roleOf(document.getElementById(id)!);
    expect([r('a'), r('b'), r('c'), r('d'), r('e'), r('f'), r('g'), r('h'), r('i'), r('j'), r('k')]).toEqual([
      'button', 'link', null, 'textbox', 'checkbox', 'combobox', 'heading', 'img', null, 'tab', 'button',
    ]);
  });

  it('computes names from aria-labelledby, aria-label, label, content, alt, title, placeholder', () => {
    html(`
      <span id=l1>Shop</span><span id=l2>name</span>
      <input id=a aria-labelledby="l1 l2">
      <button id=b aria-label="Close dialog">×</button>
      <label for=c>Caption</label><input id=c>
      <label>Phone <input id=d></label>
      <button id=e>  Save
        changes </button>
      <button id=f><img alt="Edit"></button>
      <input id=g title="Search shops">
      <input id=h placeholder="Your email">
      <input id=i type=submit value="Send">`);
    const n = (id: string) => accessibleName(document.getElementById(id)!);
    expect([n('a'), n('b'), n('c'), n('d'), n('e'), n('f'), n('g'), n('h'), n('i')]).toEqual([
      'Shop name', 'Close dialog', 'Caption', 'Phone', 'Save changes', 'Edit', 'Search shops', 'Your email', 'Send',
    ]);
  });
});

describe('findTarget', () => {
  beforeEach(() => html(''));

  it('finds by role + name (case and whitespace insensitive)', () => {
    html(`<button>Cancel</button><button> EDIT </button><a href="#">Edit</a>`);
    const r = findTarget(document, { role: 'button', name_any: ['Chỉnh sửa', 'Edit'] });
    expect(r.ok && r.el.tagName).toBe('BUTTON');
  });

  it('fails on ambiguous matches instead of guessing', () => {
    html(`<button>Save</button><button>Save</button>`);
    expect(findTarget(document, { role: 'button', name_any: ['Save'] })).toMatchObject({ ok: false, code: 'ambiguous_target' });
  });

  it('ignores invisible elements', () => {
    html(`
      <button style="display:none">Save</button>
      <div style="display:none"><button>Save</button></div>
      <button hidden>Save</button>
      <button style="visibility:hidden">Save</button>
      <button style="opacity:0">Save</button>
      <button>Save</button>`);
    const r = findTarget(document, { role: 'button', name_any: ['Save'] });
    expect(r.ok).toBe(true);
    html(`<button style="display:none">Save</button>`);
    expect(findTarget(document, { role: 'button', name_any: ['Save'] })).toMatchObject({ ok: false, code: 'target_not_found' });
    expect(findTarget(document, { role: 'button', name_any: ['Save'] }, { requireVisible: false }).ok).toBe(true);
  });

  it('combines selector, role and text and keeps the innermost text match', () => {
    html(`<div class=card><p>Shop ID: <b data-x>123456</b></p></div><div class=card><p>Other</p></div>`);
    const r = findTarget(document, { text_any: ['Shop ID'] });
    expect(r.ok && r.el.tagName).toBe('P');
    const s = findTarget(document, { selector: '.card', text_any: ['Other'] });
    expect(s.ok).toBe(true);
    expect(findTarget(document, { selector: '.card' })).toMatchObject({ code: 'ambiguous_target' });
  });

  it('reports invalid selectors', () => {
    expect(findTarget(document, { selector: '[[' })).toMatchObject({ ok: false, code: 'invalid_selector' });
  });
});
