<script setup lang="ts">
import { batchLabels, isBatchActive, rowLabels, type ImportBatch, type ImportRow } from '~/utils/products'

definePageMeta({ middleware: 'org' })

const api = useApi()
const route = useRoute()
const { orgId, can } = useOrg()
const id = computed(() => String(route.params.batchId))
const batch = ref<ImportBatch | null>(null)
const rows = ref<ImportRow[]>([])
const next = ref<string | null>(null)
const status = ref<ImportRow['status'] | ''>('')
const error = ref('')
const notice = ref('')
const editing = ref<string | null>(null)
const draft = ref<Record<string, string>>({})
const rowError = ref('')
const publishing = ref(false)
useHead({ title: () => batch.value?.filename ?? 'Import' })

const path = computed(() => ({ orgID: orgId.value, batchID: id.value }))

async function loadBatch() {
  try {
    batch.value = unwrap(await api.GET('/orgs/{orgID}/imports/{batchID}', { params: { path: path.value } })).data
  } catch (e) {
    error.value = toApiError(e).message
  }
}
async function loadRows(more = false) {
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/imports/{batchID}/rows', {
      params: { path: path.value, query: { status: status.value || undefined, cursor: more ? next.value ?? undefined : undefined } },
    }))
    rows.value = more ? [...rows.value, ...res.data] : res.data
    next.value = res.next_cursor
  } catch (e) {
    error.value = toApiError(e).message
  }
}
onMounted(async () => {
  await loadBatch()
  await loadRows()
})
watch(status, () => loadRows())
const timer = setInterval(async () => {
  if (batch.value && isBatchActive(batch.value.status)) {
    await loadBatch()
    if (!isBatchActive(batch.value.status)) await loadRows()
  }
}, 2000)
onBeforeUnmount(() => clearInterval(timer))

const columns = computed(() => {
  const set = new Set<string>()
  for (const r of rows.value) for (const k of Object.keys(r.values)) set.add(k)
  // Columns missing from the file but needed by a row, so they can be filled in.
  for (const r of rows.value) for (const e of r.errors) if (e.column && fileColumns.includes(e.column)) set.add(e.column)
  const order = (c: string) => (fileColumns.includes(c) ? fileColumns.indexOf(c) : fileColumns.length)
  return [...set].sort((a, b) => order(a) - order(b))
})
// Columns the importer understands (internal/products/importfile.go).
const fileColumns = ['sku', 'name', 'description', 'category', 'brand', 'condition', 'price', 'currency', 'stock', 'weight_kg',
  'length_cm', 'width_cm', 'height_cm', 'days_to_ship', 'image_urls', 'tier1_name', 'tier1_option', 'tier2_name', 'tier2_option', 'variation_sku']
const errorsFor = (r: ImportRow, col: string) => r.errors.filter((e) => e.column === col)

function edit(r: ImportRow) {
  editing.value = r.id
  draft.value = Object.fromEntries(columns.value.map((c) => [c, r.values[c] ?? '']))
  rowError.value = ''
}
async function saveRow(r: ImportRow) {
  rowError.value = ''
  try {
    const res = unwrap(await api.PATCH('/orgs/{orgID}/imports/{batchID}/rows/{rowID}', { params: { path: { ...path.value, rowID: r.id } }, body: { values: draft.value } }))
    Object.assign(r, res.data)
    editing.value = null
  } catch (e) {
    rowError.value = toApiError(e).message
  }
}
async function retry() {
  error.value = ''
  try {
    batch.value = unwrap(await api.POST('/orgs/{orgID}/imports/{batchID}/retry', { params: { path: path.value } })).data
    notice.value = 'Importing the fixed rows again.'
  } catch (e) {
    error.value = toApiError(e).message
  }
}
async function report() {
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/imports/{batchID}/report', { params: { path: path.value } }))
    window.location.assign(res.data.url)
  } catch (e) {
    error.value = toApiError(e).message
  }
}
const needsAttention = computed(() => (batch.value?.invalid_rows ?? 0) + (batch.value?.failed_rows ?? 0))
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader :title="batch?.filename ?? 'Import'" description="Rows with errors are not imported. Fix them here, then import again.">
      <template v-if="batch" #actions>
        <Button v-if="batch.has_report" variant="outline" @click="report">Download report</Button>
        <Button v-if="can('imports.write') && !isBatchActive(batch.status) && (needsAttention || batch.status === 'FAILED')" variant="outline" @click="retry">
          {{ batch.status === 'FAILED' ? 'Retry import' : 'Import fixed rows' }}
        </Button>
        <Button v-if="can('workflows.run') && batch.imported_rows" @click="publishing = true">Publish imported products</Button>
      </template>
    </PageHeader>
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />
    <PublishPanel v-if="publishing && batch" :import-batch-id="batch.id" label="the imported products" @close="publishing = false" />

    <dl v-if="batch" class="grid grid-cols-2 gap-3 rounded-lg border bg-background p-4 text-sm sm:grid-cols-6" aria-live="polite">
      <div><dt class="text-muted-foreground">Status</dt><dd><Badge :variant="batch.status === 'FAILED' ? 'destructive' : 'secondary'">{{ batchLabels[batch.status] }}</Badge></dd></div>
      <div><dt class="text-muted-foreground">Rows</dt><dd>{{ batch.total_rows }}</dd></div>
      <div><dt class="text-muted-foreground">Valid</dt><dd>{{ batch.valid_rows }}</dd></div>
      <div><dt class="text-muted-foreground">Invalid</dt><dd>{{ batch.invalid_rows }}</dd></div>
      <div><dt class="text-muted-foreground">Imported</dt><dd>{{ batch.imported_rows }}</dd></div>
      <div><dt class="text-muted-foreground">Failed</dt><dd>{{ batch.failed_rows }}</dd></div>
    </dl>
    <Alert v-if="batch?.error" variant="destructive"><AlertDescription>{{ batch.error }}</AlertDescription></Alert>
    <p v-if="batch?.imported_rows" class="text-sm">
      <NuxtLink :to="`/orgs/${orgId}/products?batch=${batch.id}`" class="underline">View the imported products</NuxtLink>
    </p>

    <div class="flex items-end gap-3">
      <div class="space-y-1.5">
        <Label for="row-status">Show rows</Label>
        <select id="row-status" v-model="status" class="h-9 rounded-md border bg-background px-3 text-sm">
          <option value="">All</option>
          <option v-for="(label, s) in rowLabels" :key="s" :value="s">{{ label }}</option>
        </select>
      </div>
    </div>

    <EmptyState v-if="!rows.length" title="No rows" :description="batch && isBatchActive(batch.status) ? 'The file is being read…' : 'No rows match this filter.'" />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Line</TableHead><TableHead>Status</TableHead>
            <TableHead v-for="c in columns" :key="c">{{ c }}</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="r in rows" :key="r.id" :class="r.errors.length ? 'bg-destructive/5' : ''">
            <TableCell>{{ r.row_number }}</TableCell>
            <TableCell>
              <Badge :variant="r.status === 'INVALID' || r.status === 'FAILED' ? 'destructive' : r.status === 'IMPORTED' ? 'default' : 'secondary'">{{ rowLabels[r.status] }}</Badge>
              <p v-for="e in r.errors.filter((x) => !columns.includes(x.column))" :key="e.code + e.column" class="mt-1 text-xs text-destructive">{{ e.message }}</p>
            </TableCell>
            <TableCell v-for="c in columns" :key="c" class="min-w-32 align-top">
              <Input v-if="editing === r.id" v-model="draft[c]" :aria-label="`${c}, line ${r.row_number}`" />
              <span v-else class="block max-w-64 truncate" :title="r.values[c]">{{ r.values[c] }}</span>
              <p v-for="e in errorsFor(r, c)" :key="e.code" class="mt-1 text-xs text-destructive">{{ e.message }}</p>
            </TableCell>
            <TableCell class="space-x-2 whitespace-nowrap text-right align-top">
              <template v-if="can('imports.write') && r.status !== 'IMPORTED'">
                <template v-if="editing === r.id">
                  <Button size="sm" @click="saveRow(r)">Save</Button>
                  <Button size="sm" variant="outline" @click="editing = null">Cancel</Button>
                  <p v-if="rowError" class="mt-1 text-xs text-destructive">{{ rowError }}</p>
                </template>
                <Button v-else size="sm" variant="outline" :disabled="batch ? isBatchActive(batch.status) : true" :aria-label="`Fix line ${r.row_number}`" @click="edit(r)">Fix</Button>
              </template>
              <NuxtLink v-if="r.product_id" :to="`/orgs/${orgId}/products/${r.product_id}`" class="text-sm underline">Product</NuxtLink>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <div v-if="next" class="flex justify-center"><Button variant="outline" @click="loadRows(true)">Load more</Button></div>
  </div>
</template>
