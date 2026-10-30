import type { JsonSchema } from '~/utils/schema'

export interface SectionSchema {
  key: string
  applies_to: 'shop' | 'products' | 'defaults'
  schema: JsonSchema
}

let cache: Promise<SectionSchema[]> | undefined

/** useTemplateSchemas loads the section JSON Schemas once per page load. */
export function useTemplateSchemas(): Promise<SectionSchema[]> {
  if (!cache) {
    const api = useApi()
    cache = api.GET('/template-sections').then((res) => unwrap(res).data as SectionSchema[])
    cache.catch(() => { cache = undefined })
  }
  return cache
}
