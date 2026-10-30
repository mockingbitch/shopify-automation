/** The subset of JSON Schema (draft 2020-12) used by template sections. */
export interface JsonSchema {
  type?: 'object' | 'array' | 'string' | 'integer' | 'number' | 'boolean'
  title?: string
  description?: string
  properties?: Record<string, JsonSchema>
  required?: string[]
  items?: JsonSchema
  enum?: (string | number)[]
  minimum?: number
  maximum?: number
  exclusiveMinimum?: number
  maxLength?: number
  maxItems?: number
  format?: string
  pattern?: string
  'x-capability'?: string
  'x-sensitive'?: boolean
  'x-multiline'?: boolean
  'x-applies-to'?: 'shop' | 'products' | 'defaults'
}

export type JsonObject = Record<string, unknown>

/** Human label for a property. */
export function labelOf(name: string, s: JsonSchema): string {
  if (s.title) return s.title
  const words = name.replace(/_/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/**
 * Removes empty values so optional fields are omitted rather than sent as
 * "" or empty objects, which the schemas would reject.
 */
export function prune(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(prune)
  if (v && typeof v === 'object') {
    const out: JsonObject = {}
    for (const [k, val] of Object.entries(v as JsonObject)) {
      const p = prune(val)
      if (p === '' || p === undefined || p === null) continue
      if (p && typeof p === 'object' && !Array.isArray(p) && Object.keys(p).length === 0) continue
      out[k] = p
    }
    return out
  }
  return v
}

/** Field errors from the API keyed by path ("sections.general.shop_name"). */
export type FieldErrors = Record<string, string>
