<script setup lang="ts">
import type { Shop } from '~/utils/shops'
import { runStatusLabels, runVariant, type RunStatus, type RunSummary } from '~/utils/workflows'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Automation Workflows' })

const api = useApi()
const route = useRoute()
const { orgId } = useOrg()
const runs = ref<RunSummary[]>([])
const shops = ref<Record<string, Shop>>({})
const pendingApprovals = ref(0)
const next = ref<string | null>(null)
const loading = ref(true)
const error = ref('')
const status = ref<RunStatus | ''>(typeof route.query.status === 'string' && route.query.status in runStatusLabels ? route.query.status as RunStatus : '')
const batch = computed(() => (typeof route.query.batch === 'string' ? route.query.batch : undefined))

async function load(more = false) {
  loading.value = true
  error.value = ''
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/workflows', {
      params: { path: { orgID: orgId.value }, query: { status: status.value || undefined, batch_id: batch.value, cursor: more ? next.value ?? undefined : undefined } },
    }))
    runs.value = more ? [...runs.value, ...res.data] : res.data
    next.value = res.next_cursor
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await load()
  try {
    const [s, a] = await Promise.all([
      api.GET('/orgs/{orgID}/shops', { params: { path: { orgID: orgId.value } } }),
      api.GET('/orgs/{orgID}/approvals', { params: { path: { orgID: orgId.value }, query: { status: 'PENDING' } } }),
    ])
    shops.value = Object.fromEntries(unwrap(s).data.map((x) => [x.id, x]))
    pendingApprovals.value = unwrap(a).data.length
  } catch { /* names and counts are optional */ }
})
watch(status, () => load())
watch(batch, () => load())

const shopName = (id: string) => shops.value[id]?.shop_name ?? (shops.value[id] ? `Shop ${shops.value[id]!.shopee_shop_id}` : 'Shop')
const templateOf = (r: RunSummary) => {
  const i = r.input as { template_name?: string; version_no?: number; product_ids?: string[] }
  if (r.definition === 'PRODUCT_PUBLISH') return `Publish ${i.product_ids?.length ?? 0} products`
  return i.template_name ? `${i.template_name} v${i.version_no}` : r.definition
}
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Automation Workflows" description="Shop setups and product publishing. Open a run to approve changes, finish manual tasks or retry." />
    <FormError :message="error" />
    <Alert v-if="pendingApprovals">
      <AlertTitle>{{ pendingApprovals }} run(s) waiting for approval</AlertTitle>
      <AlertDescription>
        <button type="button" class="underline" @click="status = 'WAITING_APPROVAL'">Show them</button>
      </AlertDescription>
    </Alert>

    <div class="flex flex-wrap items-center gap-3">
      <Label for="run-status" class="sr-only">Status</Label>
      <select id="run-status" v-model="status" class="h-9 rounded-md border bg-background px-3 text-sm">
        <option value="">All statuses</option>
        <option v-for="(label, key) in runStatusLabels" :key="key" :value="key">{{ label }}</option>
      </select>
      <span v-if="batch" class="text-sm text-muted-foreground">
        Showing one batch · <NuxtLink :to="`/orgs/${orgId}/workflows`" class="underline">show all</NuxtLink>
      </span>
    </div>

    <p v-if="loading && !runs.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <EmptyState v-else-if="!runs.length" title="No workflow runs" description="Apply a published template to shops from its preview page to start a shop setup." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Shop</TableHead>
            <TableHead>Template</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Started</TableHead>
            <TableHead>Finished</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="r in runs" :key="r.id">
            <TableCell>
              <NuxtLink :to="`/orgs/${orgId}/workflows/${r.id}`" class="font-medium underline-offset-4 hover:underline">{{ shopName(r.shop_id) }}</NuxtLink>
            </TableCell>
            <TableCell>{{ templateOf(r) }}</TableCell>
            <TableCell><Badge :variant="runVariant(r.status)">{{ runStatusLabels[r.status] }}</Badge></TableCell>
            <TableCell>{{ fmt.format(new Date(r.created_at)) }}</TableCell>
            <TableCell>{{ r.finished_at ? fmt.format(new Date(r.finished_at)) : '—' }}</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <Button v-if="next" variant="outline" :disabled="loading" @click="load(true)">Load more</Button>
  </div>
</template>
