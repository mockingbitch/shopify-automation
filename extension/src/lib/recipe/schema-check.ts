/**
 * A small JSON Schema (2020-12) checker for exactly the keywords the recipe
 * schema uses. It interprets the schema as data; it never generates code, so
 * it runs under the MV3 extension CSP (no eval / new Function), unlike Ajv.
 *
 * Any keyword it does not know makes it throw, so a schema change that needs
 * new support fails loudly in unit tests instead of being silently ignored.
 */

export interface SchemaError {
  path: string;
  message: string;
}

type Schema = Record<string, unknown> | boolean;

const ANNOTATIONS = new Set(['$schema', '$id', 'title', 'description', '$defs', 'default', '$comment', 'examples']);
const SUPPORTED = new Set([
  'type', 'enum', 'const', 'pattern', 'minLength', 'maxLength', 'minimum', 'maximum',
  'items', 'minItems', 'maxItems', 'properties', 'required', 'additionalProperties',
  'propertyNames', 'minProperties', 'maxProperties', 'oneOf', '$ref',
]);

export function assertSupported(schema: unknown, at = '#'): void {
  if (typeof schema === 'boolean') return;
  if (typeof schema !== 'object' || schema === null) throw new Error(`schema at ${at} is not an object`);
  for (const [k, v] of Object.entries(schema)) {
    if (ANNOTATIONS.has(k)) {
      if (k === '$defs') for (const [n, s] of Object.entries(v as object)) assertSupported(s, `${at}/$defs/${n}`);
      continue;
    }
    if (!SUPPORTED.has(k)) throw new Error(`unsupported schema keyword "${k}" at ${at}`);
    if (k === 'properties') for (const [n, s] of Object.entries(v as object)) assertSupported(s, `${at}/properties/${n}`);
    if (k === 'items' || k === 'additionalProperties' || k === 'propertyNames') assertSupported(v, `${at}/${k}`);
    if (k === 'oneOf') (v as unknown[]).forEach((s, i) => assertSupported(s, `${at}/oneOf/${i}`));
  }
}

function typeOf(v: unknown): string {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'array';
  if (typeof v === 'number') return Number.isInteger(v) ? 'integer' : 'number';
  return typeof v;
}

function matchesType(v: unknown, t: string): boolean {
  const actual = typeOf(v);
  if (t === 'number') return actual === 'number' || actual === 'integer';
  return actual === t;
}

/** JSON Schema string length counts code points, not UTF-16 units. */
function strLen(s: string): number {
  let n = 0;
  for (const _ of s) n++;
  return n;
}

function deepEqual(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

const regexCache = new Map<string, RegExp>();
function re(p: string): RegExp {
  let r = regexCache.get(p);
  if (!r) {
    r = new RegExp(p, 'u');
    regexCache.set(p, r);
  }
  return r;
}

export class SchemaChecker {
  constructor(private readonly root: Record<string, unknown>) {
    assertSupported(root);
  }

  check(value: unknown): SchemaError[] {
    const errors: SchemaError[] = [];
    this.walk(this.root, value, '', errors);
    return errors;
  }

  private resolve(ref: string): Schema {
    if (!ref.startsWith('#/')) throw new Error(`unsupported $ref ${ref}`);
    let cur: unknown = this.root;
    for (const part of ref.slice(2).split('/')) {
      cur = (cur as Record<string, unknown>)[part.replace(/~1/g, '/').replace(/~0/g, '~')];
      if (cur === undefined) throw new Error(`unresolved $ref ${ref}`);
    }
    return cur as Schema;
  }

  private walk(schema: Schema, v: unknown, path: string, errors: SchemaError[]): void {
    if (schema === true) return;
    if (schema === false) {
      errors.push({ path, message: 'is not allowed' });
      return;
    }
    const s = schema as Record<string, unknown>;
    const at = path || '/';
    const err = (message: string) => errors.push({ path: at, message });

    if (typeof s.$ref === 'string') this.walk(this.resolve(s.$ref), v, path, errors);

    if (typeof s.type === 'string' && !matchesType(v, s.type)) {
      err(`must be ${s.type}`);
      return;
    }
    if (Array.isArray(s.enum) && !s.enum.some((e) => deepEqual(e, v))) err(`must be one of ${s.enum.join(', ')}`);
    if ('const' in s && !deepEqual(s.const, v)) err(`must equal ${JSON.stringify(s.const)}`);

    if (typeof v === 'string') {
      if (typeof s.minLength === 'number' && strLen(v) < s.minLength) err(`must have at least ${s.minLength} characters`);
      if (typeof s.maxLength === 'number' && strLen(v) > s.maxLength) err(`must have at most ${s.maxLength} characters`);
      if (typeof s.pattern === 'string' && !re(s.pattern).test(v)) err(`must match ${s.pattern}`);
    }
    if (typeof v === 'number') {
      if (typeof s.minimum === 'number' && v < s.minimum) err(`must be >= ${s.minimum}`);
      if (typeof s.maximum === 'number' && v > s.maximum) err(`must be <= ${s.maximum}`);
    }
    if (Array.isArray(v)) {
      if (typeof s.minItems === 'number' && v.length < s.minItems) err(`must have at least ${s.minItems} items`);
      if (typeof s.maxItems === 'number' && v.length > s.maxItems) err(`must have at most ${s.maxItems} items`);
      if (s.items !== undefined) v.forEach((item, i) => this.walk(s.items as Schema, item, `${path}/${i}`, errors));
    }
    if (typeOf(v) === 'object') {
      const obj = v as Record<string, unknown>;
      const keys = Object.keys(obj);
      if (typeof s.minProperties === 'number' && keys.length < s.minProperties) err(`must have at least ${s.minProperties} properties`);
      if (typeof s.maxProperties === 'number' && keys.length > s.maxProperties) err(`must have at most ${s.maxProperties} properties`);
      if (Array.isArray(s.required)) {
        for (const r of s.required as string[]) if (!(r in obj)) err(`must have property "${r}"`);
      }
      const props = (s.properties ?? {}) as Record<string, Schema>;
      for (const k of keys) {
        const child = `${path}/${k.replace(/~/g, '~0').replace(/\//g, '~1')}`;
        if (s.propertyNames !== undefined) {
          const sub: SchemaError[] = [];
          this.walk(s.propertyNames as Schema, k, child, sub);
          if (sub.length) errors.push({ path: child, message: `property name "${k}" is invalid` });
        }
        if (Object.hasOwn(props, k)) this.walk(props[k]!, obj[k], child, errors);
        else if (s.additionalProperties !== undefined) {
          if (s.additionalProperties === false) errors.push({ path: child, message: `unknown property "${k}"` });
          else this.walk(s.additionalProperties as Schema, obj[k], child, errors);
        }
      }
    }
    if (Array.isArray(s.oneOf)) {
      let passing = 0;
      for (const sub of s.oneOf as Schema[]) {
        const subErrors: SchemaError[] = [];
        this.walk(sub, v, path, subErrors);
        if (subErrors.length === 0) passing++;
      }
      if (passing !== 1) err(passing === 0 ? 'must match exactly one alternative (none matched)' : 'must match exactly one alternative (several matched)');
    }
  }
}
