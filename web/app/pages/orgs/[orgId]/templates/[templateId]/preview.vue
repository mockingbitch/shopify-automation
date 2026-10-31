<script setup lang="ts">
import { labelOf, type JsonObject } from '~/utils/schema'
import type { SectionSchema } from '~/composables/useTemplateSchemas'
import type { components } from '~/types/api'
import { modeLabels, reasonText, statusLabels, type Shop } from '~/utils/shops'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Xem trước mẫu shop' })
const versionStatusLabels: Record<string, string> = { DRAFT: 'Bản nháp', PUBLISHED: 'Đã phát hành', ARCHIVED: 'Đã lưu trữ' }

type Detail = { template: components['schemas']['Template']; versions: components['schemas']['TemplateVersion'][] }
type Preview = components['schemas']['ShopPreview']
type Change = components['schemas']['Change']

const api = useApi()
const pathLabel = usePathLabel()
const route = useRoute()
const { orgId, can } = useOrg()
const templateId = computed(() => String(route.params.templateId))
const path = computed(() => ({ orgID: orgId.value, templateID: templateId.value }))

const detail = ref<Detail>()
const shops = ref<Shop[]>([])
const chosen = ref<string[]>([])
const versionId = ref('')
const previews = ref<Preview[]>([])
const error = ref('')
const busy = ref(false)

// Overrides editor for one shop at a time: a form per section (generated
// from the section schema), or raw JSON for advanced edits.
const overrideShop = ref<string>('')
const overrideText = ref('')
const overrideErrors = ref<string[]>([])
const overrideFields = ref<Record<string, string>>({})
const overrideNotice = ref('')
const overrideData = ref<Record<string, JsonObject>>({})
const overrideSection = ref('')
const overrideMode = ref<'form' | 'json'>('form')
const schemas = ref<SectionSchema[]>([])
onMounted(async () => {
  try {
    schemas.value = (await useTemplateSchemas()).filter((x) => x.applies_to === 'shop')
  } catch { /* JSON mode still works */ }
})
const sectionSchema = computed(() => schemas.value.find((x) => x.key === overrideSection.value)?.schema)

// clean drops empty values so an override only holds what was set.
function clean(v: unknown): unknown {
  if (Array.isArray(v)) return v.length ? v : undefined
  if (v && typeof v === 'object') {
    const out: Record<string, unknown> = {}
    for (const [k, x] of Object.entries(v)) {
      const c = clean(x)
      if (c !== undefined) out[k] = c
    }
    return Object.keys(out).length ? out : undefined
  }
  return v === '' || v === null ? undefined : v
}
function switchMode(m: 'form' | 'json') {
  if (m === 'json') overrideText.value = JSON.stringify(clean(overrideData.value) ?? {}, null, 2)
  else {
    try {
      overrideData.value = JSON.parse(overrideText.value || '{}')
    } catch {
      overrideErrors.value = ['Hãy sửa JSON trước khi quay lại biểu mẫu.']
      return
    }
  }
  overrideMode.value = m
}

onMounted(async () => {
  try {
    const [d, s] = await Promise.all([
      api.GET('/orgs/{orgID}/templates/{templateID}', { params: { path: path.value } }),
      api.GET('/orgs/{orgID}/shops', { params: { path: { orgID: orgId.value } } }),
    ])
    detail.value = unwrap(d).data
    shops.value = unwrap(s).data.filter((x) => x.connection_status !== 'DISCONNECTED')
    const q = typeof route.query.version === 'string' ? route.query.version : ''
    versionId.value = q || detail.value.template.current_published_version_id || detail.value.versions[0]?.id || ''
  } catch (e) {
    error.value = toApiError(e).message
  }
})

async function run() {
  busy.value = true
  error.value = ''
  try {
    previews.value = unwrap(await api.POST('/orgs/{orgID}/templates/{templateID}/versions/{versionID}/preview', {
      params: { path: { ...path.value, versionID: versionId.value } }, body: { shop_ids: chosen.value },
    })).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    busy.value = false
  }
}

const selectedVersion = computed(() => detail.value?.versions.find((v) => v.id === versionId.value))
const canApply = computed(() => selectedVersion.value?.status === 'PUBLISHED' && can('workflows.run'))
// One key per confirmation dialog: a double click or a retried request
// returns the same runs instead of starting new ones.
const applyKey = ref(crypto.randomUUID())

async function apply() {
  busy.value = true
  error.value = ''
  try {
    const res = unwrap(await api.POST('/orgs/{orgID}/templates/{templateID}/versions/{versionID}/apply', {
      params: { path: { ...path.value, versionID: versionId.value }, header: { 'Idempotency-Key': applyKey.value } },
      body: { shop_ids: chosen.value },
    }))
    applyKey.value = crypto.randomUUID()
    await navigateTo(`/orgs/${orgId.value}/workflows?batch=${res.data.batch_id}`)
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    busy.value = false
  }
}

async function editOverrides(shopId: string) {
  overrideErrors.value = []
  overrideNotice.value = ''
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/templates/{templateID}/shops/{shopID}/overrides', {
      params: { path: { ...path.value, shopID: shopId } },
    })).data
    overrideShop.value = shopId
    overrideData.value = (res.overrides ?? {}) as Record<string, JsonObject>
    overrideText.value = JSON.stringify(res.overrides, null, 2)
    overrideSection.value = Object.keys(overrideData.value)[0] ?? schemas.value[0]?.key ?? ''
    overrideMode.value = 'form'
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function saveOverrides() {
  overrideErrors.value = []
  overrideNotice.value = ''
  overrideFields.value = {}
  let parsed: Record<string, Record<string, unknown>>
  if (overrideMode.value === 'form') {
    parsed = (clean(overrideData.value) ?? {}) as Record<string, Record<string, unknown>>
  } else {
    try {
      parsed = JSON.parse(overrideText.value || '{}')
    } catch {
      overrideErrors.value = ['Nội dung này không phải JSON hợp lệ.']
      return
    }
  }
  try {
    unwrap(await api.PUT('/orgs/{orgID}/templates/{templateID}/shops/{shopID}/overrides', {
      params: { path: { ...path.value, shopID: overrideShop.value } }, body: { overrides: parsed },
    }))
    overrideNotice.value = 'Đã lưu giá trị riêng cho shop.'
    if (previews.value.length) await run()
  } catch (e) {
    const err = toApiError(e)
    overrideFields.value = err.fields
    overrideErrors.value = Object.keys(err.fields).length ? Object.entries(err.fields).map(([f, m]) => `${f}: ${m}`) : [err.message]
  }
}

const shopName = (id: string) => {
  const s = shops.value.find((x) => x.id === id)
  return s?.shop_name ?? (s ? `Shop ${s.shopee_shop_id}` : id)
}

function show(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'string') return v
  if (typeof v === 'boolean') return v ? 'Có' : 'Không'
  if (Array.isArray(v)) return v.map(show).join(', ')
  if (typeof v === 'object') {
    return Object.entries(v as Record<string, unknown>).map(([k, x]) => `${k.replace(/_/g, ' ')}: ${show(x)}`).join('; ')
  }
  return String(v)
}
function rowNote(c: Change): string {
  const parts: string[] = []
  if (c.reason) parts.push(reasonText[c.reason] ?? c.reason)
  if (c.note) parts.push(c.note)
  return parts.join(' ')
}
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <NuxtLink :to="`/orgs/${orgId}/templates/${templateId}`" class="text-sm text-muted-foreground hover:underline">← Quay lại mẫu shop</NuxtLink>
    <PageHeader :title="`Xem trước: ${detail?.template.name ?? ''}`"
                description="Những gì phiên bản này sẽ thay đổi trên từng shop, so với dữ liệu shop đồng bộ gần nhất. Không có gì thay đổi trên Shopee cho đến khi bạn áp dụng và duyệt." />
    <FormError :message="error" />

    <form v-if="detail" class="space-y-4 rounded-lg border bg-background p-4" @submit.prevent="run">
      <div class="flex flex-wrap items-center gap-3 text-sm">
        <Label for="pv-version">Phiên bản</Label>
        <select id="pv-version" v-model="versionId" class="h-9 rounded-md border bg-background px-3">
          <option v-for="v in detail.versions" :key="v.id" :value="v.id">v{{ v.version_no }} · {{ (versionStatusLabels[v.status] ?? v.status).toLowerCase() }}</option>
        </select>
      </div>
      <fieldset>
        <legend class="mb-2 text-sm font-medium">Shop (đã chọn {{ chosen.length }}, tối đa 50)</legend>
        <p v-if="!shops.length" class="text-sm text-muted-foreground">Chưa có shop nào được kết nối. Hãy kết nối shop trước.</p>
        <div class="grid gap-1 sm:grid-cols-2">
          <label v-for="s in shops" :key="s.id" class="flex items-center gap-2 rounded p-1.5 text-sm hover:bg-muted">
            <input v-model="chosen" type="checkbox" :value="s.id" class="size-4" :disabled="!chosen.includes(s.id) && chosen.length >= 50">
            {{ s.shop_name ?? `Shop ${s.shopee_shop_id}` }}
            <span class="text-xs text-muted-foreground">{{ s.region ?? '' }} · {{ statusLabels[s.connection_status] }}</span>
          </label>
        </div>
      </fieldset>
      <div class="flex flex-wrap items-center gap-2">
        <Button type="submit" :disabled="busy || !chosen.length || !versionId">{{ busy ? 'Đang xử lý…' : 'Xem trước thay đổi' }}</Button>
        <ConfirmDialog v-if="canApply" :title="`Áp dụng cho ${chosen.length} shop?`"
                       description="Mỗi shop sẽ có một lượt chạy thiết lập. Mỗi lượt chạy sẽ đọc lại shop và chờ duyệt trước khi thay đổi bất cứ gì trên Shopee."
                       confirm-label="Bắt đầu thiết lập" @confirm="apply">
          <template #trigger><Button type="button" variant="outline" :disabled="busy || !chosen.length">Áp dụng cho các shop đã chọn</Button></template>
        </ConfirmDialog>
        <span v-else-if="selectedVersion && selectedVersion.status !== 'PUBLISHED'" class="text-sm text-muted-foreground">Chỉ có thể áp dụng phiên bản đã phát hành.</span>
      </div>
    </form>

    <section v-for="p in previews" :key="p.shop_id" class="space-y-3 rounded-lg border bg-background p-4" :aria-labelledby="`pv-${p.shop_id}`">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 :id="`pv-${p.shop_id}`" class="text-base font-semibold">
          {{ p.shop_name ?? shopName(p.shop_id) }}
          <Badge v-if="p.has_overrides" variant="outline" class="ml-1">Có giá trị riêng</Badge>
        </h2>
        <Button v-if="can('templates.write')" variant="outline" size="sm" @click="editOverrides(p.shop_id)">Sửa giá trị riêng</Button>
      </div>
      <ul v-if="p.errors.length || p.warnings.length" class="list-disc pl-5 text-sm">
        <li v-for="e in p.errors" :key="e.field + e.code" class="text-destructive">{{ e.message }}</li>
        <li v-for="w in p.warnings" :key="w.field + w.code" class="text-amber-700">{{ w.message }}</li>
      </ul>
      <p v-if="!p.errors.length && !p.changes.length && !p.blocked.length" class="text-sm text-muted-foreground">Không có thay đổi: shop đã khớp với mẫu.</p>

      <div v-if="p.changes.length || p.blocked.length" class="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Trường</TableHead>
              <TableHead>Hiện tại</TableHead>
              <TableHead>Theo mẫu</TableHead>
              <TableHead>Thực hiện qua</TableHead>
              <TableHead>Chọn sẵn</TableHead>
              <TableHead>Ghi chú</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="c in [...p.changes, ...p.blocked]" :key="c.path" :class="c.enabled ? '' : 'opacity-60'">
              <TableCell class="text-sm">
                {{ pathLabel(c.path) }}
                <Badge v-if="c.sensitive" variant="destructive" class="ml-1">Nhạy cảm</Badge>
              </TableCell>
              <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ c.current_known ? show(c.current) : 'Chưa rõ (chưa đọc từ shop)' }}</TableCell>
              <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ show(c.target) }}</TableCell>
              <TableCell class="text-sm">{{ modeLabels[c.mode] }}</TableCell>
              <TableCell class="whitespace-normal text-sm">{{ !c.enabled ? 'Bị chặn' : c.default_selected ? 'Có' : 'Không, cần đánh dấu' }}</TableCell>
              <TableCell class="max-w-72 whitespace-normal text-xs text-muted-foreground">{{ rowNote(c) }}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </section>

    <Card v-if="overrideShop">
      <CardHeader>
        <CardTitle as="h2" class="text-base">Giá trị riêng cho shop {{ shopName(overrideShop) }}</CardTitle>
        <CardDescription>
          Giá trị nhập ở đây thay cho giá trị của mẫu, chỉ áp dụng cho shop này. Để trống một trường để giữ giá trị của mẫu.
          Nhóm trường được gộp theo từng trường; danh sách sẽ thay thế toàn bộ danh sách của mẫu.
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-3">
        <div class="flex flex-wrap items-end gap-3">
          <div v-if="overrideMode === 'form'" class="space-y-1.5">
            <Label for="override-section">Mục</Label>
            <select id="override-section" v-model="overrideSection" class="h-9 rounded-md border bg-background px-3 text-sm">
              <option v-for="sch in schemas" :key="sch.key" :value="sch.key">
                {{ labelOf(sch.key, sch.schema) }}{{ overrideData[sch.key] && Object.keys(overrideData[sch.key]!).length ? ' (có giá trị riêng)' : '' }}
              </option>
            </select>
          </div>
          <div role="group" aria-label="Trình chỉnh sửa" class="flex gap-1">
            <Button size="sm" :variant="overrideMode === 'form' ? 'default' : 'outline'" :aria-pressed="overrideMode === 'form'" @click="switchMode('form')">Biểu mẫu</Button>
            <Button size="sm" :variant="overrideMode === 'json' ? 'default' : 'outline'" :aria-pressed="overrideMode === 'json'" @click="switchMode('json')">JSON</Button>
          </div>
        </div>
        <template v-if="overrideMode === 'form' && sectionSchema">
          <SchemaForm :key="overrideShop + overrideSection" :schema="sectionSchema" :model-value="overrideData[overrideSection] ?? {}"
                      :path="`overrides.${overrideSection}`" :id-prefix="`ov-${overrideSection}`" :errors="overrideFields"
                      @update:model-value="(v) => (overrideData = { ...overrideData, [overrideSection]: v })" />
          <Button v-if="overrideData[overrideSection]" variant="link" size="sm" class="h-auto p-0"
                  @click="() => { const { [overrideSection]: _, ...rest } = overrideData; overrideData = rest }">
            Xóa giá trị riêng của mục này
          </Button>
        </template>
        <template v-else-if="overrideMode === 'json'">
          <Label for="override-json">Giá trị riêng dạng JSON</Label>
          <textarea id="override-json" v-model="overrideText" rows="10" spellcheck="false" class="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm" />
        </template>
        <ul v-if="overrideErrors.length" class="list-disc pl-5 text-sm text-destructive" role="alert">
          <li v-for="m in overrideErrors" :key="m">{{ m }}</li>
        </ul>
        <p v-if="overrideNotice" class="text-sm">{{ overrideNotice }}</p>
        <div class="flex gap-2">
          <Button @click="saveOverrides">Lưu giá trị riêng</Button>
          <Button variant="ghost" @click="overrideShop = ''">Đóng</Button>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
