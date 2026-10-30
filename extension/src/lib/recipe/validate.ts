import schemaJson from '../../../../internal/browser/recipe.schema.json';
import { SchemaChecker, type SchemaError } from './schema-check';
import { FILES_REF, checkRefs, hasTemplateSyntax } from './templates';
import { ACTIONS, WRITE_ACTIONS, actionOf, type Recipe, type Step, type Target } from './types';

export interface ValidateOptions {
  /** TEST builds only: also accept http://localhost[:port] and http://127.0.0.1[:port]. */
  allowLocalHttp: boolean;
}

export interface ValidationIssue {
  path: string;
  message: string;
  code: 'schema' | 'unknown_action' | 'write_before_assert_shop_id' | 'template' | 'origin' | 'regex' | 'verify_write';
}

export type ValidationResult = { ok: true; recipe: Recipe } | { ok: false; issues: ValidationIssue[] };

const LOCAL_HTTP_ORIGIN = '^(https://[a-z0-9.-]+(:[0-9]+)?|http://(localhost|127\\.0\\.0\\.1)(:[0-9]+)?)$';
const LOCAL_HTTP_START = '^(https://[^/]+/|http://(localhost|127\\.0\\.0\\.1)(:[0-9]+)?/)';
const HTTPS_ORIGIN = /^https:\/\/[a-z0-9.-]+(:[0-9]+)?$/;
const LOCAL_ORIGIN = /^http:\/\/(localhost|127\.0\.0\.1)(:[0-9]+)?$/;

function buildChecker(allowLocalHttp: boolean): SchemaChecker {
  const schema = structuredClone(schemaJson) as unknown as Record<string, any>;
  if (allowLocalHttp) {
    schema.properties.allowed_origins.items.pattern = LOCAL_HTTP_ORIGIN;
    schema.properties.start_url.pattern = LOCAL_HTTP_START;
  }
  return new SchemaChecker(schema);
}

const checkers = new Map<boolean, SchemaChecker>();
function checker(allowLocalHttp: boolean): SchemaChecker {
  let c = checkers.get(allowLocalHttp);
  if (!c) checkers.set(allowLocalHttp, (c = buildChecker(allowLocalHttp)));
  return c;
}

export function originOf(url: string): string | null {
  try {
    const u = new URL(url);
    if (u.protocol !== 'https:' && u.protocol !== 'http:') return null;
    return u.origin;
  } catch {
    return null;
  }
}

export function originAllowed(origin: string, opts: ValidateOptions): boolean {
  return HTTPS_ORIGIN.test(origin) || (opts.allowLocalHttp && LOCAL_ORIGIN.test(origin));
}

/** Counts capture groups by matching the empty alternative. */
function captureGroups(pattern: string): number {
  return (new RegExp(`${pattern}|`, 'u').exec('')?.length ?? 1) - 1;
}

function targetStrings(t: Target): string[] {
  return [t.selector ?? '', ...(t.name_any ?? []), ...(t.text_any ?? [])];
}

/** Validates an already parsed recipe: schema first, then semantic rules. */
export function validateRecipe(value: unknown, opts: ValidateOptions): ValidationResult {
  const schemaErrors: SchemaError[] = checker(opts.allowLocalHttp).check(value);
  if (schemaErrors.length) {
    const issues: ValidationIssue[] = schemaErrors.map((e) => ({ ...e, code: 'schema' }));
    // Name unknown actions explicitly: they are the most likely tampering/authoring mistake.
    const v = value as { steps?: unknown; verify?: unknown };
    for (const list of ['steps', 'verify'] as const) {
      const arr = v?.[list];
      if (!Array.isArray(arr)) continue;
      arr.forEach((s, i) => {
        if (s && typeof s === 'object') {
          for (const k of Object.keys(s)) {
            if (k !== 'timeout_ms' && !(ACTIONS as readonly string[]).includes(k)) {
              issues.unshift({ path: `/${list}/${i}`, message: `unknown action "${k}"`, code: 'unknown_action' });
            }
          }
        }
      });
    }
    return { ok: false, issues };
  }
  const recipe = value as Recipe;
  const issues: ValidationIssue[] = [];
  const add = (path: string, code: ValidationIssue['code'], message: string) => issues.push({ path, code, message });

  // Origins: https only (TEST builds also local http); start_url must be on an allowed origin.
  recipe.allowed_origins.forEach((o, i) => {
    if (!originAllowed(o, opts)) add(`/allowed_origins/${i}`, 'origin', `origin "${o}" is not allowed (https only)`);
  });
  const startOrigin = originOf(recipe.start_url);
  if (!startOrigin || !recipe.allowed_origins.includes(startOrigin)) {
    add('/start_url', 'origin', 'start_url must be on one of allowed_origins');
  }
  if (hasTemplateSyntax(recipe.start_url)) add('/start_url', 'template', 'templates are not allowed in start_url');

  recipe.stop_on.forEach((c, i) => {
    if ('url_matches' in c) {
      try {
        new RegExp(c.url_matches, 'u');
      } catch {
        add(`/stop_on/${i}/url_matches`, 'regex', 'invalid regular expression');
      }
    }
  });

  const checkStep = (step: Step, path: string, phase: 'steps' | 'verify') => {
    const action = actionOf(step);
    if (phase === 'verify' && WRITE_ACTIONS.has(action)) {
      add(path, 'verify_write', `"${action}" is not allowed in verify (verify steps only read)`);
    }
    const tmpl = (p: string, text: string) => {
      for (const m of checkRefs(recipe, text, 'text')) add(p, 'template', m);
    };
    const literal = (p: string, t: Target) => {
      if (targetStrings(t).some(hasTemplateSyntax)) add(p, 'template', 'templates are only allowed in goto.url, fill.value, select.value and upload.files');
    };
    switch (action) {
      case 'goto': {
        const s = step as Extract<Step, { goto: unknown }>;
        tmpl(`${path}/goto/url`, s.goto.url);
        // A literal origin can be checked now; templated URLs are checked again at run time.
        if (!s.goto.url.startsWith('{{')) {
          const o = originOf(s.goto.url);
          if (!o || !recipe.allowed_origins.includes(o)) add(`${path}/goto/url`, 'origin', 'goto url must be on one of allowed_origins');
        } else add(`${path}/goto/url`, 'origin', 'goto url must start with a literal allowed origin');
        break;
      }
      case 'fill':
      case 'select': {
        const s = (step as unknown as Record<string, { target: Target; value: string }>)[action]!;
        literal(`${path}/${action}/target`, s.target);
        tmpl(`${path}/${action}/value`, s.value);
        break;
      }
      case 'upload': {
        const s = step as Extract<Step, { upload: unknown }>;
        literal(`${path}/upload/target`, s.upload.target);
        const m = FILES_REF.exec(s.upload.files);
        const decl = m ? recipe.inputs[m[1]!] : undefined;
        if (!m || !decl) add(`${path}/upload/files`, 'template', `upload.files references undeclared input "${m?.[1] ?? s.upload.files}"`);
        else if (decl.type !== 'files') add(`${path}/upload/files`, 'template', `upload.files must reference a files input; "${m[1]}" is ${decl.type}`);
        break;
      }
      case 'assert_shop_id': {
        const s = step as Extract<Step, { assert_shop_id: unknown }>;
        literal(`${path}/assert_shop_id/target`, s.assert_shop_id.target);
        try {
          if (captureGroups(s.assert_shop_id.pattern) < 1) add(`${path}/assert_shop_id/pattern`, 'regex', 'pattern needs one capture group');
        } catch {
          add(`${path}/assert_shop_id/pattern`, 'regex', 'invalid regular expression');
        }
        break;
      }
      case 'wait_for':
      case 'click':
        literal(`${path}/${action}`, (step as unknown as Record<string, Target>)[action]!);
        break;
      case 'expect': {
        const s = step as { expect: { text_any?: string[]; target?: Target } };
        if (s.expect.target) literal(`${path}/expect/target`, s.expect.target);
        if (s.expect.text_any?.some(hasTemplateSyntax)) add(`${path}/expect/text_any`, 'template', 'templates are not allowed in expect');
        break;
      }
      case 'screenshot':
        break;
    }
  };

  let asserted = false;
  recipe.steps.forEach((step, i) => {
    const action = actionOf(step);
    if (action === 'assert_shop_id') asserted = true;
    if (WRITE_ACTIONS.has(action) && !asserted) {
      add(`/steps/${i}`, 'write_before_assert_shop_id', `"${action}" comes before any assert_shop_id step`);
    }
    checkStep(step, `/steps/${i}`, 'steps');
  });
  (recipe.verify ?? []).forEach((step, i) => checkStep(step, `/verify/${i}`, 'verify'));

  return issues.length ? { ok: false, issues } : { ok: true, recipe };
}

export function formatIssues(issues: ValidationIssue[], max = 5): string {
  const shown = issues.slice(0, max).map((i) => `${i.path}: ${i.message}`);
  if (issues.length > max) shown.push(`…and ${issues.length - max} more`);
  return shown.join('; ');
}
