import type { Recipe } from './types';

/**
 * Templates: only {{task.shopee_shop_id}} and {{input.<name>}} are allowed,
 * and only in goto.url, fill.value and select.value (plus upload.files, which
 * must be exactly one {{input.<files input>}}). No expressions, no filters.
 */

const TEMPLATE = /\{\{\s*([^{}]*?)\s*\}\}/g;
const INPUT_REF = /^input\.([a-z][a-z0-9_]{0,39})$/;
export const FILES_REF = /^\{\{input\.([a-z][a-z0-9_]*)\}\}$/;

export type TemplateRef = { kind: 'task_shop_id' } | { kind: 'input'; name: string };

export interface TemplateIssue {
  message: string;
}

/** Parses a template string into its references, or returns issues. */
export function parseTemplate(text: string): { refs: TemplateRef[]; issues: TemplateIssue[] } {
  const refs: TemplateRef[] = [];
  const issues: TemplateIssue[] = [];
  const stripped = text.replace(TEMPLATE, (_m, expr: string) => {
    if (expr === 'task.shopee_shop_id') refs.push({ kind: 'task_shop_id' });
    else {
      const m = INPUT_REF.exec(expr);
      if (m) refs.push({ kind: 'input', name: m[1]! });
      else issues.push({ message: `unknown template "{{${expr}}}" (allowed: {{task.shopee_shop_id}}, {{input.<name>}})` });
    }
    return '';
  });
  if (stripped.includes('{{') || stripped.includes('}}')) issues.push({ message: 'unbalanced "{{" or "}}"' });
  return { refs, issues };
}

/** Checks that every reference is a declared input of the given type. */
export function checkRefs(recipe: Pick<Recipe, 'inputs'>, text: string, want: 'text'): string[] {
  const { refs, issues } = parseTemplate(text);
  const out = issues.map((i) => i.message);
  for (const r of refs) {
    if (r.kind !== 'input') continue;
    const decl = recipe.inputs[r.name];
    if (!decl) out.push(`template references undeclared input "${r.name}"`);
    else if (decl.type !== want) out.push(`input "${r.name}" is a ${decl.type} input; only ${want} inputs can be used here`);
  }
  return out;
}

export interface RenderContext {
  shopeeShopId: string;
  /** Text input values by name. A missing optional input renders as "". */
  text: Record<string, string>;
}

export class TemplateError extends Error {
  constructor(message: string, readonly code = 'missing_input') {
    super(message);
  }
}

/** Renders a validated template. Throws TemplateError on anything unexpected. */
export function render(text: string, ctx: RenderContext, required: (name: string) => boolean): string {
  return text.replace(TEMPLATE, (_m, expr: string) => {
    if (expr === 'task.shopee_shop_id') return ctx.shopeeShopId;
    const m = INPUT_REF.exec(expr);
    if (!m) throw new TemplateError(`unknown template {{${expr}}}`, 'recipe_invalid');
    const name = m[1]!;
    const v = ctx.text[name];
    if (v === undefined) {
      if (required(name)) throw new TemplateError(`required input "${name}" was not provided`);
      return '';
    }
    return v;
  });
}

/** True if a string contains anything that looks like a template. */
export function hasTemplateSyntax(s: string): boolean {
  return s.includes('{{') || s.includes('}}');
}
