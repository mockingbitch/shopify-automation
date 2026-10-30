<script setup lang="ts">
import type { components } from '~/types/api'
import { batchLabels, isBatchActive, type ImportBatch } from '~/utils/products'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Product Imports' })

type Template = components['schemas']['Template']

const api = useApi()
const upload = useUpload()
const { orgId, can } = useOrg()
const batches = ref<ImportBatch[]>([])
const templates = ref<Template[]>([])
const loading = ref(true)
const error = ref('')
const templateId = ref('')
const file = ref<File | null>(null)
const busy = ref(false)

async function load() {
  loading.value = true
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/imports', { params: { path: { orgID: orgId.value } } }))
    batches.value = res.data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(async () => {
  await load()
  try {
    templates.value = unwrap(await api.GET('/orgs/{orgID}/templates', { params: { path: { orgID: orgId.value } } })).data
      .filter((t) => t.current_published_version_id)
  } catch { /* templates are optional */ }
})
// Refresh while an import is running.
const timer = setInterval(() => {
  if (batches.value.some((b) => isBatchActive(b.status))) void load()
}, 3000)
onBeforeUnmount(() => clearInterval(timer))

async function submit() {
  if (!file.value) return
  busy.value = true
  error.value = ''
  try {
    const t = templates.value.find((x) => x.id === templateId.value)
    const b = await upload<ImportBatch>(`/orgs/${orgId.value}/imports`, file.value, {
      template_id: t?.id, template_version_id: t?.current_published_version_id ?? undefined,
    })
    await navigateTo(`/orgs/${orgId.value}/imports/${b.id}`)
  } catch (e) {
    const err = toApiError(e)
    error.value = Object.values(err.fields).join(' ') || err.message
  } finally {
    busy.value = false
  }
}
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Product Imports" description="Import products from CSV or Excel. Every row is checked; fix invalid rows here and import them again." />
    <FormError :message="error" />

    <form v-if="can('imports.write')" class="grid gap-3 rounded-lg border bg-background p-4 sm:grid-cols-[2fr_2fr_auto] sm:items-end" @submit.prevent="submit">
      <FormField id="imp-file" label="File (CSV or XLSX, up to 20 MB)">
        <input id="imp-file" type="file" accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
               class="block w-full text-sm" @change="file = ($event.target as HTMLInputElement).files?.[0] ?? null">
      </FormField>
      <FormField id="imp-tpl" label="Template defaults (optional)" hint="Category mappings, SKU rules and defaults from a published template.">
        <select id="imp-tpl" v-model="templateId" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">None</option>
          <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </FormField>
      <Button type="submit" :disabled="busy || !file">{{ busy ? 'Uploading…' : 'Import' }}</Button>
    </form>
    <p class="text-sm text-muted-foreground">
      Not sure about the columns? <a :href="`/api/v1/orgs/${orgId}/imports/sample.csv`" class="underline" download>Download a sample CSV</a>.
    </p>

    <p v-if="loading && !batches.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <EmptyState v-else-if="!batches.length" title="No imports yet" description="Upload a file to create products in bulk." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>File</TableHead><TableHead>Status</TableHead><TableHead>Rows</TableHead>
            <TableHead>Imported</TableHead><TableHead>Need attention</TableHead><TableHead>Uploaded</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="b in batches" :key="b.id">
            <TableCell><NuxtLink :to="`/orgs/${orgId}/imports/${b.id}`" class="font-medium underline-offset-4 hover:underline">{{ b.filename }}</NuxtLink></TableCell>
            <TableCell><Badge :variant="b.status === 'FAILED' ? 'destructive' : b.status === 'COMPLETED' ? 'default' : 'secondary'">{{ batchLabels[b.status] }}</Badge></TableCell>
            <TableCell>{{ b.total_rows }}</TableCell>
            <TableCell>{{ b.imported_rows }}</TableCell>
            <TableCell>{{ b.invalid_rows + b.failed_rows || '—' }}</TableCell>
            <TableCell>{{ fmt.format(new Date(b.created_at)) }}</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
