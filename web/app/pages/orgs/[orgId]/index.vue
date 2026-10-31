<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
const { orgId, membership, can } = useOrg()
useHead({ title: () => membership.value?.organization.name ?? 'Tổng quan' })

type Overview = components['schemas']['Overview']
const api = useApi()
const counts = ref<Overview | null>(null)
const activity = ref<components['schemas']['AuditLog'][]>([])
const loading = ref(true)
const loadError = ref('')

onMounted(async () => {
  try {
    if (can('shops.read')) {
      counts.value = unwrap(await api.GET('/orgs/{orgID}/overview', { params: { path: { orgID: orgId.value } } })).data
    }
    if (can('audit.read')) {
      activity.value = unwrap(await api.GET('/orgs/{orgID}/audit-logs', { params: { path: { orgID: orgId.value }, query: { limit: 8 } } })).data
    }
  } catch (e) {
    loadError.value = toApiError(e).message
  } finally {
    loading.value = false
  }
})

const base = computed(() => `/orgs/${orgId.value}`)
const stats = computed(() => {
  const c = counts.value
  if (!c) return []
  return [
    { label: 'Shop đã kết nối', value: c.connected_shops, to: `${base.value}/shops` },
    { label: 'Shop đang hoạt động', value: c.active_shops, to: `${base.value}/shops?status=ACTIVE` },
    { label: 'Shop bị ngắt kết nối', value: c.disconnected_shops, to: `${base.value}/shops`, warn: c.disconnected_shops > 0 },
    { label: 'Quy trình đang chạy', value: c.running_workflows, to: `${base.value}/workflows?status=RUNNING` },
    { label: 'Quy trình thất bại (30 ngày)', value: c.failed_workflows_30d, to: `${base.value}/workflows?status=FAILED`, warn: c.failed_workflows_30d > 0 },
    { label: 'Lượt thiết lập hoàn tất', value: c.completed_setups, to: `${base.value}/workflows?status=COMPLETED` },
  ]
})
const attention = computed(() => {
  const c = counts.value
  if (!c) return []
  const out: { text: string; to: string }[] = []
  if (c.pending_approvals) out.push({ text: `${c.pending_approvals} nhóm thay đổi đang chờ duyệt`, to: `${base.value}/workflows?status=WAITING_APPROVAL` })
  if (c.waiting_workflows > c.pending_approvals) out.push({ text: `${c.waiting_workflows - c.pending_approvals} quy trình đang chờ bạn xử lý`, to: `${base.value}/workflows?status=WAITING_USER` })
  if (c.reconnect_required) out.push({ text: `${c.reconnect_required} shop cần kết nối lại`, to: `${base.value}/shops?status=RECONNECT_REQUIRED` })
  if (c.queued_browser_tasks) out.push({ text: `${c.queued_browser_tasks} việc đang chờ tiện ích trình duyệt`, to: `${base.value}/extension` })
  return out
})
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-8">
    <PageHeader :title="membership?.organization.name ?? 'Tổng quan'" description="Tình trạng các shop và quy trình tự động của bạn." />
    <FormError :message="loadError" />

    <GettingStarted v-if="counts && (can('shops.connect') || can('products.write'))" :counts="counts" />

    <section v-if="can('shops.read')" aria-labelledby="stats-heading">
      <h2 id="stats-heading" class="sr-only">Số liệu</h2>
      <p v-if="loading" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
      <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <NuxtLink v-for="s in stats" :key="s.label" :to="s.to" class="rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
          <Card class="h-full transition-colors hover:bg-muted/40">
            <CardHeader class="pb-2">
              <CardDescription>{{ s.label }}</CardDescription>
              <CardTitle class="text-3xl" :class="s.warn ? 'text-destructive' : ''">{{ s.value }}</CardTitle>
            </CardHeader>
          </Card>
        </NuxtLink>
      </div>
      <p v-if="counts" class="mt-2 text-sm text-muted-foreground">
        {{ counts.products }} sản phẩm, {{ counts.published_products }} sản phẩm đã đăng lên ít nhất một shop.
      </p>
    </section>

    <section v-if="attention.length" aria-labelledby="attention-heading" class="rounded-lg border border-amber-300 bg-amber-50 p-4">
      <h2 id="attention-heading" class="mb-2 text-base font-semibold text-amber-900">Cần chú ý</h2>
      <ul class="space-y-1 text-sm">
        <li v-for="a in attention" :key="a.text"><NuxtLink :to="a.to" class="text-amber-900 underline underline-offset-4">{{ a.text }}</NuxtLink></li>
      </ul>
    </section>

    <section v-if="can('audit.read')" aria-labelledby="activity-heading">
      <h2 id="activity-heading" class="mb-3 text-lg font-medium">Hoạt động gần đây</h2>
      <AuditTable v-if="activity.length" :rows="activity" compact />
      <EmptyState v-else-if="!loading && !loadError" title="Chưa có hoạt động nào" />
    </section>
  </div>
</template>
