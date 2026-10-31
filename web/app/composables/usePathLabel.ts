import type { SectionSchema } from '~/composables/useTemplateSchemas'
import { labelOf } from '~/utils/schema'

/**
 * usePathLabel turns a change path ("general.shop_name",
 * "logistics.channels.50011") into the labels of the template schema
 * ("Thông tin chung › Tên shop"), so sellers do not see technical keys.
 */
export function usePathLabel() {
  const schemas = ref<SectionSchema[]>([])
  useTemplateSchemas().then((s) => (schemas.value = s)).catch(() => { /* fall back to the path */ })
  return (path: string): string => {
    const [section, field, ...rest] = path.split('.')
    const sec = schemas.value.find((x) => x.key === section)
    if (!sec || !field) return path
    const f = sec.schema.properties?.[field]
    return [labelOf(section!, sec.schema), f ? labelOf(field, f) : field, ...rest].join(' › ')
  }
}
