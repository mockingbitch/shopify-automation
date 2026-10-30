<script setup lang="ts">
import { labelOf, type JsonObject } from '~/utils/schema'
import type { SectionSchema } from '~/composables/useTemplateSchemas'
import type { components } from '~/types/api'
import { modeLabels, reasonText, statusLabels, type Shop } from '~/utils/shops'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Template preview' })

type Detail = { template: components['schemas']['Template']; versions: components['schemas']['TemplateVersion'][] }
type Preview = components['schemas']['ShopPreview']
type Change = components['schemas']['Change']

const api = useApi()
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
      overrideErrors.value = ['Fix the JSON before switching back to the form.']
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
      overrideErrors.value = ['This is not valid JSON.']
      return
    }
  }
  try {
    unwrap(await api.PUT('/orgs/{orgID}/templates/{templateID}/shops/{shopID}/overrides', {
      params: { path: { ...path.value, shopID: overrideShop.value } }, body: { overrides: parsed },
    }))
    overrideNotice.value = 'Overrides saved.'
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
  if (typeof v === 'boolean') return v ? 'Yes' : 'No'
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
    <NuxtLink :to="`/orgs/${orgId}/templates/${templateId}`" class="text-sm text-muted-foreground hover:underline">← Back to template</NuxtLink>
    <PageHeader :title="`Preview: ${detail?.template.name ?? ''}`"
                description="What this version would change on each shop, compared with the last synced shop data. Nothing is changed on Shopee until you apply and approve." />
    <FormError :message="error" />

    <form v-if="detail" class="space-y-4 rounded-lg border bg-background p-4" @submit.prevent="run">
      <div class="flex flex-wrap items-center gap-3 text-sm">
        <Label for="pv-version">Version</Label>
        <select id="pv-version" v-model="versionId" class="h-9 rounded-md border bg-background px-3">
          <option v-for="v in detail.versions" :key="v.id" :value="v.id">v{{ v.version_no }} · {{ v.status.toLowerCase() }}</option>
        </select>
      </div>
      <fieldset>
        <legend class="mb-2 text-sm font-medium">Shops ({{ chosen.length }} selected, up to 50)</legend>
        <p v-if="!shops.length" class="text-sm text-muted-foreground">No connected shops. Connect a shop first.</p>
        <div class="grid gap-1 sm:grid-cols-2">
          <label v-for="s in shops" :key="s.id" class="flex items-center gap-2 rounded p-1.5 text-sm hover:bg-muted">
            <input v-model="chosen" type="checkbox" :value="s.id" class="size-4" :disabled="!chosen.includes(s.id) && chosen.length >= 50">
            {{ s.shop_name ?? `Shop ${s.shopee_shop_id}` }}
            <span class="text-xs text-muted-foreground">{{ s.region ?? '' }} · {{ statusLabels[s.connection_status] }}</span>
          </label>
        </div>
      </fieldset>
      <div class="flex flex-wrap items-center gap-2">
        <Button type="submit" :disabled="busy || !chosen.length || !versionId">{{ busy ? 'Working…' : 'Preview changes' }}</Button>
        <ConfirmDialog v-if="canApply" :title="`Apply to ${chosen.length} shop${chosen.length === 1 ? '' : 's'}?`"
                       description="One setup run starts per shop. Each run reads the shop again and waits for an approval before changing anything on Shopee."
                       confirm-label="Start setup" @confirm="apply">
          <template #trigger><Button type="button" variant="outline" :disabled="busy || !chosen.length">Apply to selected shops</Button></template>
        </ConfirmDialog>
        <span v-else-if="selectedVersion && selectedVersion.status !== 'PUBLISHED'" class="text-sm text-muted-foreground">Only the published version can be applied.</span>
      </div>
    </form>

    <section v-for="p in previews" :key="p.shop_id" class="space-y-3 rounded-lg border bg-background p-4" :aria-labelledby="`pv-${p.shop_id}`">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 :id="`pv-${p.shop_id}`" class="text-base font-semibold">
          {{ p.shop_name ?? shopName(p.shop_id) }}
          <Badge v-if="p.has_overrides" variant="outline" class="ml-1">Has overrides</Badge>
        </h2>
        <Button v-if="can('templates.write')" variant="outline" size="sm" @click="editOverrides(p.shop_id)">Edit overrides</Button>
      </div>
      <ul v-if="p.errors.length || p.warnings.length" class="list-disc pl-5 text-sm">
        <li v-for="e in p.errors" :key="e.field + e.code" class="text-destructive">{{ e.message }}</li>
        <li v-for="w in p.warnings" :key="w.field + w.code" class="text-amber-700">{{ w.message }}</li>
      </ul>
      <p v-if="!p.errors.length && !p.changes.length && !p.blocked.length" class="text-sm text-muted-foreground">No changes: the shop already matches.</p>

      <div v-if="p.changes.length || p.blocked.length" class="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Field</TableHead>
              <TableHead>Now</TableHead>
              <TableHead>Template</TableHead>
              <TableHead>Runs through</TableHead>
              <TableHead>Selected by default</TableHead>
              <TableHead>Notes</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="c in [...p.changes, ...p.blocked]" :key="c.path" :class="c.enabled ? '' : 'opacity-60'">
              <TableCell class="font-mono text-xs">
                {{ c.path }}
                <Badge v-if="c.sensitive" variant="destructive" class="ml-1">Sensitive</Badge>
              </TableCell>
              <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ c.current_known ? show(c.current) : 'Unknown until read' }}</TableCell>
              <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ show(c.target) }}</TableCell>
              <TableCell class="text-sm">{{ modeLabels[c.mode] }}</TableCell>
              <TableCell class="whitespace-normal text-sm">{{ !c.enabled ? 'Blocked' : c.default_selected ? 'Yes' : 'No, needs a tick' }}</TableCell>
              <TableCell class="max-w-72 whitespace-normal text-xs text-muted-foreground">{{ rowNote(c) }}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </section>

    <Card v-if="overrideShop">
      <CardHeader>
        <CardTitle as="h2" class="text-base">Overrides for {{ shopName(overrideShop) }}</CardTitle>
        <CardDescription>
          Values set here replace the template’s values for this shop only. Leave a field empty to keep the template’s value.
          Objects merge field by field; lists replace the template’s list.
        </CardDescription>
      </CardHeader>
      <CardContent class="space-y-3">
        <div class="flex flex-wrap items-end gap-3">
          <div v-if="overrideMode === 'form'" class="space-y-1.5">
            <Label for="override-section">Section</Label>
            <select id="override-section" v-model="overrideSection" class="h-9 rounded-md border bg-background px-3 text-sm">
              <option v-for="sch in schemas" :key="sch.key" :value="sch.key">
                {{ labelOf(sch.key, sch.schema) }}{{ overrideData[sch.key] && Object.keys(overrideData[sch.key]!).length ? ' (overridden)' : '' }}
              </option>
            </select>
          </div>
          <div role="group" aria-label="Editor" class="flex gap-1">
            <Button size="sm" :variant="overrideMode === 'form' ? 'default' : 'outline'" :aria-pressed="overrideMode === 'form'" @click="switchMode('form')">Form</Button>
            <Button size="sm" :variant="overrideMode === 'json' ? 'default' : 'outline'" :aria-pressed="overrideMode === 'json'" @click="switchMode('json')">JSON</Button>
          </div>
        </div>
        <template v-if="overrideMode === 'form' && sectionSchema">
          <SchemaForm :key="overrideShop + overrideSection" :schema="sectionSchema" :model-value="overrideData[overrideSection] ?? {}"
                      :path="`overrides.${overrideSection}`" :id-prefix="`ov-${overrideSection}`" :errors="overrideFields"
                      @update:model-value="(v) => (overrideData = { ...overrideData, [overrideSection]: v })" />
          <Button v-if="overrideData[overrideSection]" variant="link" size="sm" class="h-auto p-0"
                  @click="() => { const { [overrideSection]: _, ...rest } = overrideData; overrideData = rest }">
            Remove the override of this section
          </Button>
        </template>
        <template v-else-if="overrideMode === 'json'">
          <Label for="override-json">Overrides as JSON</Label>
          <textarea id="override-json" v-model="overrideText" rows="10" spellcheck="false" class="w-full rounded-md border bg-background px-3 py-2 font-mono text-sm" />
        </template>
        <ul v-if="overrideErrors.length" class="list-disc pl-5 text-sm text-destructive" role="alert">
          <li v-for="m in overrideErrors" :key="m">{{ m }}</li>
        </ul>
        <p v-if="overrideNotice" class="text-sm">{{ overrideNotice }}</p>
        <div class="flex gap-2">
          <Button @click="saveOverrides">Save overrides</Button>
          <Button variant="ghost" @click="overrideShop = ''">Close</Button>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
