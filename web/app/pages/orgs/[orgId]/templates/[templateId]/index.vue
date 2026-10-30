<script setup lang="ts">
import type { components } from '~/types/api'
import { prune, type FieldErrors, type JsonObject } from '~/utils/schema'
import type { SectionSchema } from '~/composables/useTemplateSchemas'

definePageMeta({ middleware: 'org' })

type Detail = { template: components['schemas']['Template']; versions: components['schemas']['TemplateVersion'][] }
type Report = components['schemas']['ValidationReport']

const api = useApi()
const route = useRoute()
const router = useRouter()
const { orgId, can } = useOrg()
const templateId = computed(() => String(route.params.templateId))

const detail = ref<Detail>()
const schemas = ref<SectionSchema[]>([])
const versionId = ref('')
const sections = ref<Record<string, { content: JsonObject; etag: string }>>({})
const edits = ref<Record<string, JsonObject>>({})
const activeKey = ref('general')
const errors = ref<FieldErrors>({})
const error = ref('')
const notice = ref('')
const report = ref<Report>()
const busy = ref(false)

useHead({ title: () => detail.value?.template.name ?? 'Template' })

const version = computed(() => detail.value?.versions.find((v) => v.id === versionId.value))
const isDraft = computed(() => version.value?.status === 'DRAFT')
const editable = computed(() => isDraft.value && can('templates.write'))
const activeSchema = computed(() => schemas.value.find((s) => s.key === activeKey.value))
const dirty = computed(() => JSON.stringify(prune(edits.value[activeKey.value] ?? {})) !== JSON.stringify(sections.value[activeKey.value]?.content ?? {}))
const groups = computed(() => [
  { title: 'Applied to the shop', items: schemas.value.filter((s) => s.applies_to === 'shop') },
  { title: 'Defaults', items: schemas.value.filter((s) => s.applies_to === 'defaults') },
  { title: 'Used for products', items: schemas.value.filter((s) => s.applies_to === 'products') },
])
const path = computed(() => ({ orgID: orgId.value, templateID: templateId.value }))

async function loadDetail() {
  detail.value = unwrap(await api.GET('/orgs/{orgID}/templates/{templateID}', { params: { path: path.value } })).data
}

async function loadVersion(id: string) {
  const res = unwrap(await api.GET('/orgs/{orgID}/templates/{templateID}/versions/{versionID}', {
    params: { path: { ...path.value, versionID: id } },
  })).data
  versionId.value = id
  sections.value = Object.fromEntries(res.sections.map((s) => [s.key, { content: s.content as JsonObject, etag: s.etag }]))
  edits.value = Object.fromEntries(res.sections.map((s) => [s.key, structuredClone(s.content as JsonObject)]))
  errors.value = {}
  report.value = undefined
}

function defaultVersion(d: Detail): string {
  const q = typeof route.query.version === 'string' ? route.query.version : ''
  if (q && d.versions.some((v) => v.id === q)) return q
  return d.template.draft_version_id ?? d.template.current_published_version_id ?? d.versions[0]?.id ?? ''
}

onMounted(async () => {
  try {
    const [s] = await Promise.all([useTemplateSchemas(), loadDetail()])
    schemas.value = s
    if (detail.value) await loadVersion(defaultVersion(detail.value))
  } catch (e) {
    error.value = toApiError(e).message
  }
})

async function selectVersion(id: unknown) {
  if (typeof id !== 'string') return
  await router.replace({ query: { ...route.query, version: id } })
  await act(() => loadVersion(id))
}

async function act(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await fn()
  } catch (e) {
    const err = toApiError(e)
    errors.value = err.fields
    error.value = err.status === 412 ? 'Someone else changed this section. Reload the page to see their version.' : err.message
  } finally {
    busy.value = false
  }
}

const save = () => act(async () => {
  const key = activeKey.value
  const body = prune(edits.value[key] ?? {}) as JsonObject
  errors.value = {}
  const res = unwrap(await api.PUT('/orgs/{orgID}/templates/{templateID}/versions/{versionID}/sections/{sectionKey}', {
    params: { path: { ...path.value, versionID: versionId.value, sectionKey: key }, header: { 'If-Match': sections.value[key]!.etag } },
    body,
  })).data
  sections.value[key] = { content: res.content as JsonObject, etag: res.etag }
  edits.value[key] = structuredClone(res.content as JsonObject)
  notice.value = 'Section saved.'
})

const validate = () => act(async () => {
  report.value = unwrap(await api.POST('/orgs/{orgID}/templates/{templateID}/versions/{versionID}/validate', {
    params: { path: { ...path.value, versionID: versionId.value } },
  })).data
  errors.value = Object.fromEntries(report.value.errors.map((e) => [e.field, e.message]))
})

const publish = () => act(async () => {
  unwrap(await api.POST('/orgs/{orgID}/templates/{templateID}/versions/{versionID}/publish', {
    params: { path: { ...path.value, versionID: versionId.value } },
  }))
  await loadDetail()
  await loadVersion(versionId.value)
  notice.value = `Version ${version.value?.version_no} is published and can no longer be changed.`
})

const newDraft = () => act(async () => {
  const res = unwrap(await api.POST('/orgs/{orgID}/templates/{templateID}/versions', {
    params: { path: path.value }, body: { from_version_id: versionId.value },
  })).data
  await loadDetail()
  await selectVersion(res.id)
  notice.value = `Draft version ${res.version_no} created.`
})

function discard() {
  edits.value[activeKey.value] = structuredClone(sections.value[activeKey.value]!.content)
}

function sectionHasErrors(key: string) {
  return Object.keys(errors.value).some((f) => f === `sections.${key}` || f.startsWith(`sections.${key}.`))
}
function sectionFilled(key: string) {
  return Object.keys(sections.value[key]?.content ?? {}).length > 0
}

const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <NuxtLink :to="`/orgs/${orgId}/templates`" class="text-sm text-muted-foreground hover:underline">← All templates</NuxtLink>
    <FormError :message="error" />

    <template v-if="detail && version">
      <PageHeader :title="detail.template.name" :description="detail.template.description || undefined">
        <template #actions>
          <Button as-child variant="outline"><NuxtLink :to="`/orgs/${orgId}/templates/${templateId}/preview?version=${versionId}`">Preview on shops</NuxtLink></Button>
          <Button v-if="!detail.template.draft_version_id && can('templates.write')" variant="outline" :disabled="busy" @click="newDraft">New draft from this version</Button>
          <Button v-if="isDraft" variant="outline" :disabled="busy" @click="validate">Validate</Button>
          <ConfirmDialog v-if="isDraft && can('templates.publish')" title="Publish this version?"
                         description="A published version can no longer be edited. The current published version is archived."
                         confirm-label="Publish" @confirm="publish">
            <template #trigger><Button :disabled="busy">Publish</Button></template>
          </ConfirmDialog>
        </template>
      </PageHeader>

      <div class="flex flex-wrap items-center gap-3 text-sm">
        <Label for="version-select">Version</Label>
        <select id="version-select" class="h-9 rounded-md border bg-background px-3" :value="versionId" @change="selectVersion(($event.target as HTMLSelectElement).value)">
          <option v-for="v in detail.versions" :key="v.id" :value="v.id">
            v{{ v.version_no }} · {{ v.status.toLowerCase() }}{{ v.published_at ? ` · ${fmt.format(new Date(v.published_at))}` : '' }}
          </option>
        </select>
        <Badge :variant="isDraft ? 'secondary' : 'default'">{{ version.status }}</Badge>
        <span v-if="!isDraft" class="text-muted-foreground">Read-only. Create a new draft to make changes.</span>
      </div>

      <div aria-live="polite">
        <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
      </div>
      <Alert v-if="report" :variant="report.valid ? 'default' : 'destructive'">
        <AlertTitle>{{ report.valid ? 'Ready to publish' : `${report.errors.length} problem(s) to fix` }}</AlertTitle>
        <AlertDescription>
          <ul class="list-disc pl-5">
            <li v-for="e in report.errors" :key="e.field + e.code">{{ e.field.replace('sections.', '') }}: {{ e.message }}</li>
            <li v-for="w in report.warnings" :key="w.field + w.code" class="text-muted-foreground">Warning: {{ w.message }}</li>
          </ul>
        </AlertDescription>
      </Alert>

      <div class="grid gap-6 md:grid-cols-[14rem_1fr]">
        <nav aria-label="Sections" class="space-y-4">
          <div v-for="g in groups" :key="g.title">
            <p class="mb-1 text-xs font-medium uppercase text-muted-foreground">{{ g.title }}</p>
            <ul class="space-y-0.5">
              <li v-for="s in g.items" :key="s.key">
                <button type="button" :aria-current="activeKey === s.key ? 'true' : undefined"
                        :class="['flex w-full items-center justify-between rounded-md px-3 py-1.5 text-left text-sm',
                                 activeKey === s.key ? 'bg-primary/10 font-medium text-primary' : 'hover:bg-muted']"
                        @click="activeKey = s.key">
                  {{ s.schema.title }}
                  <span v-if="sectionHasErrors(s.key)" class="text-destructive" aria-label="has errors">●</span>
                  <span v-else-if="sectionFilled(s.key)" class="text-muted-foreground" aria-label="filled">✓</span>
                </button>
              </li>
            </ul>
          </div>
        </nav>

        <Card v-if="activeSchema">
          <CardHeader>
            <CardTitle as="h2" class="text-base">{{ activeSchema.schema.title }}</CardTitle>
            <CardDescription>{{ activeSchema.schema.description }}</CardDescription>
          </CardHeader>
          <CardContent class="space-y-4">
            <SchemaForm v-model="edits[activeKey]!" :schema="activeSchema.schema" :path="`sections.${activeKey}`"
                        :id-prefix="activeKey" :errors="errors" :readonly="!editable" />
            <p v-if="errors[`sections.${activeKey}`]" class="text-xs text-destructive">{{ errors[`sections.${activeKey}`] }}</p>
            <div v-if="editable" class="flex gap-2">
              <Button :disabled="busy || !dirty" @click="save">Save section</Button>
              <Button variant="ghost" :disabled="busy || !dirty" @click="discard">Discard</Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </template>
  </div>
</template>
