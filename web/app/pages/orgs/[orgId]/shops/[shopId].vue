<script setup lang="ts">
import { runStatusLabels, runVariant } from '~/utils/workflows'
import type { components } from '~/types/api'
import { daysUntil, modeLabels, needsReconnect, reasonText, statusLabels, statusVariant } from '~/utils/shops'

definePageMeta({ middleware: 'org', validate: (route) => route.params.shopId !== 'select' })

type Detail = components['schemas']['ShopDetail']

const api = useApi()
const route = useRoute()
const { orgId, can } = useOrg()
const shopId = computed(() => String(route.params.shopId))

const detail = ref<Detail>()
type Activity = components['schemas']['RunRef']
const activity = ref<{ last_setup: Activity | null; published_products: number; products_needing_attention: number; runs: Activity[] }>()
const runTitle = (r: Activity) => {
  const i = r.input as { template_name?: string; version_no?: number; product_ids?: string[] }
  if (r.definition === 'PRODUCT_PUBLISH') return `Đăng ${i.product_ids?.length ?? 0} sản phẩm`
  return i.template_name ? `Thiết lập: ${i.template_name} v${i.version_no}` : 'Thiết lập shop'
}
const loading = ref(true)
const error = ref('')
const notice = ref(route.query.connected === '1' ? 'Đã kết nối shop. Thông tin shop đang được đồng bộ.' : '')
const busy = ref(false)

const title = computed(() => detail.value?.shop.shop_name ?? (detail.value ? `Shop ${detail.value.shop.shopee_shop_id}` : 'Shop'))
useHead({ title })

const path = computed(() => ({ orgID: orgId.value, shopID: shopId.value }))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [d, a] = await Promise.all([
      api.GET('/orgs/{orgID}/shops/{shopID}', { params: { path: path.value } }),
      api.GET('/orgs/{orgID}/shops/{shopID}/activity', { params: { path: path.value } }),
    ])
    detail.value = unwrap(d).data
    activity.value = unwrap(a).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function act(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    busy.value = false
  }
}

const sync = () => act(async () => {
  unwrap(await api.POST('/orgs/{orgID}/shops/{shopID}/sync', { params: { path: path.value } }))
  notice.value = 'Đã xếp lịch đồng bộ. Hãy tải lại trang sau ít phút để xem thông tin mới nhất.'
})

const reconnect = () => act(async () => {
  const res = unwrap(await api.POST('/orgs/{orgID}/shops/{shopID}/reconnect', { params: { path: path.value } }))
  window.location.assign(res.data.authorization_url)
})

const disconnect = () => act(async () => {
  unwrap(await api.DELETE('/orgs/{orgID}/shops/{shopID}', { params: { path: path.value }, body: { confirm: true } }))
  notice.value = 'Đã ngắt kết nối shop. Mã truy cập Shopee đã lưu và dữ liệu đã đồng bộ đã bị xóa.'
  await load()
})

const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'short' })
const date = (iso: string | null | undefined) => (iso ? fmt.format(new Date(iso)) : '—')

const eventLabels: Record<string, string> = {
  GRANTED: 'Đã ủy quyền', REAUTHORIZED: 'Đã ủy quyền lại', REFRESHED: 'Đã gia hạn mã truy cập', REFRESH_FAILED: 'Gia hạn mã truy cập thất bại',
  EXPIRING: 'Ủy quyền sắp hết hạn', EXPIRED: 'Ủy quyền đã hết hạn', REVOKED: 'Người bán đã thu hồi', DISCONNECTED: 'Đã ngắt kết nối',
}
const sourceLabels: Record<string, string> = {
  callback: 'trang ủy quyền Shopee', push: 'thông báo từ Shopee', sync: 'lần đồng bộ', refresh: 'gia hạn tự động', user: 'người dùng',
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-6">
    <NuxtLink :to="`/orgs/${orgId}/shops`" class="text-sm text-muted-foreground hover:underline">← Tất cả shop</NuxtLink>
    <p v-if="loading && !detail" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <FormError :message="error" />

    <template v-if="detail">
      <PageHeader :title="title" :description="`ID shop Shopee ${detail.shop.shopee_shop_id}${detail.shop.environment === 'sandbox' ? ' · Sandbox' : ''}`">
        <template #actions>
          <Button v-if="can('shops.sync') && detail.shop.connection_status === 'ACTIVE'" variant="outline" :disabled="busy" @click="sync">Đồng bộ ngay</Button>
          <Button v-if="can('shops.connect') && needsReconnect(detail.shop.connection_status)" :disabled="busy" @click="reconnect">Kết nối lại</Button>
          <ConfirmDialog
            v-if="can('shops.disconnect') && detail.shop.connection_status !== 'DISCONNECTED'"
            title="Ngắt kết nối shop này?"
            description="Mã truy cập Shopee đã lưu và dữ liệu shop đã đồng bộ sẽ bị xóa ngay. Các quy trình tự động cho shop này sẽ dừng. Bạn có thể kết nối lại sau. Để thu hồi quyền truy cập cả trên Shopee, hãy hủy ủy quyền trong Kênh Người Bán Shopee."
            confirm-label="Ngắt kết nối"
            @confirm="disconnect"
          >
            <template #trigger><Button variant="destructive" :disabled="busy">Ngắt kết nối</Button></template>
          </ConfirmDialog>
        </template>
      </PageHeader>

      <div aria-live="polite">
        <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
      </div>
      <Alert v-if="needsReconnect(detail.shop.connection_status) && detail.shop.connection_status !== 'DISCONNECTED'" variant="destructive">
        <AlertTitle>{{ statusLabels[detail.shop.connection_status] }}</AlertTitle>
        <AlertDescription>Shopee không còn chấp nhận ủy quyền của shop này. Hãy kết nối lại shop để tiếp tục.</AlertDescription>
      </Alert>

      <div class="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader><CardTitle as="h2" class="text-base">Kết nối</CardTitle></CardHeader>
          <CardContent>
            <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
              <dt class="text-muted-foreground">Trạng thái</dt>
              <dd><Badge :variant="statusVariant(detail.shop.connection_status)">{{ statusLabels[detail.shop.connection_status] }}</Badge></dd>
              <dt class="text-muted-foreground">Thị trường</dt><dd>{{ detail.shop.region ?? '—' }}</dd>
              <dt class="text-muted-foreground">Ủy quyền hết hạn</dt>
              <dd>
                {{ date(detail.shop.authorization_expires_at) }}
                <span v-if="(daysUntil(detail.shop.authorization_expires_at) ?? 99) <= 7 && detail.shop.connection_status === 'ACTIVE'" class="block text-xs text-destructive">
                  Hãy kết nối lại trong vòng {{ Math.max(0, daysUntil(detail.shop.authorization_expires_at) ?? 0) }} ngày để không bị gián đoạn.
                </span>
              </dd>
              <dt class="text-muted-foreground">Ngày kết nối</dt><dd>{{ date(detail.shop.connected_at) }}</dd>
              <dt class="text-muted-foreground">Đồng bộ gần nhất</dt>
              <dd>
                {{ date(detail.shop.last_synced_at) }}
                <span v-if="detail.shop.last_sync_error" class="block text-xs text-destructive">Lần đồng bộ gần nhất thất bại ({{ detail.shop.last_sync_error }})</span>
              </dd>
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle as="h2" class="text-base">Hồ sơ trên Shopee</CardTitle></CardHeader>
          <CardContent>
            <p v-if="!detail.profile" class="text-sm text-muted-foreground">Chưa đồng bộ.</p>
            <div v-else class="flex gap-4">
              <img v-if="detail.profile.shop_logo_url" :src="detail.profile.shop_logo_url" alt="Logo shop" class="size-16 rounded border object-cover" referrerpolicy="no-referrer">
              <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                <dt class="text-muted-foreground">Tên</dt><dd>{{ detail.profile.shop_name ?? '—' }}</dd>
                <dt class="text-muted-foreground">Trạng thái trên Shopee</dt><dd>{{ detail.profile.status ?? '—' }}</dd>
                <dt class="text-muted-foreground">Shop xuyên biên giới</dt><dd>{{ detail.profile.is_cb ? 'Có' : 'Không' }}</dd>
                <dt class="text-muted-foreground">Mô tả</dt><dd class="whitespace-pre-line">{{ detail.profile.description || '—' }}</dd>
              </dl>
            </div>
          </CardContent>
        </Card>
      </div>

      <section v-if="activity" aria-labelledby="config-heading" class="grid gap-4 md:grid-cols-3">
        <h2 id="config-heading" class="sr-only">Cấu hình và sản phẩm</h2>
        <Card>
          <CardHeader class="pb-2">
            <CardDescription>Cấu hình</CardDescription>
            <CardTitle class="text-base">
              <template v-if="activity.last_setup">
                <Badge :variant="runVariant(activity.last_setup.status)">{{ runStatusLabels[activity.last_setup.status] }}</Badge>
              </template>
              <template v-else>Chưa thiết lập lần nào</template>
            </CardTitle>
          </CardHeader>
          <CardContent class="text-sm text-muted-foreground">
            <NuxtLink v-if="activity.last_setup" :to="`/orgs/${orgId}/workflows/${activity.last_setup.id}`" class="underline">
              {{ runTitle(activity.last_setup) }}, {{ date(activity.last_setup.finished_at ?? activity.last_setup.created_at) }}
            </NuxtLink>
            <span v-else>Hãy áp dụng một mẫu shop để thiết lập shop này.</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader class="pb-2">
            <CardDescription>Sản phẩm đã đăng</CardDescription>
            <CardTitle class="text-3xl">{{ activity.published_products }}</CardTitle>
          </CardHeader>
          <CardContent v-if="activity.products_needing_attention" class="text-sm text-destructive">
            {{ activity.products_needing_attention }} sản phẩm cần chú ý (thất bại hoặc chưa rõ kết quả)
          </CardContent>
        </Card>
        <Card>
          <CardHeader class="pb-2">
            <CardDescription>Lần đồng bộ gần nhất</CardDescription>
            <CardTitle class="text-base">{{ date(detail?.shop.last_synced_at) }}</CardTitle>
          </CardHeader>
        </Card>
      </section>

      <section v-if="activity?.runs.length" aria-labelledby="runs-heading" class="space-y-2">
        <h2 id="runs-heading" class="text-lg font-semibold">Lịch sử thiết lập</h2>
        <ul class="divide-y rounded-lg border bg-background text-sm">
          <li v-for="r in activity.runs" :key="r.id" class="flex flex-wrap items-center justify-between gap-2 px-4 py-2">
            <NuxtLink :to="`/orgs/${orgId}/workflows/${r.id}`" class="underline-offset-4 hover:underline">{{ runTitle(r) }}</NuxtLink>
            <span class="flex items-center gap-2">
              <Badge :variant="runVariant(r.status)">{{ runStatusLabels[r.status] }}</Badge>
              <span class="text-xs text-muted-foreground">{{ date(r.created_at) }}</span>
            </span>
          </li>
        </ul>
      </section>

      <section aria-labelledby="cap-heading" class="space-y-3">
        <div>
          <h2 id="cap-heading" class="text-lg font-semibold">Chức năng</h2>
          <p class="text-sm text-muted-foreground">Những việc có thể tự động cho shop này và cách thực hiện. Chưa chức năng nào được kiểm chứng trên môi trường sandbox của Shopee.</p>
        </div>
        <div class="overflow-x-auto rounded-lg border bg-background">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Chức năng</TableHead>
                <TableHead>Thực hiện qua</TableHead>
                <TableHead>Dùng được</TableHead>
                <TableHead>Ghi chú</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="c in detail.capabilities" :key="c.key">
                <TableCell>
                  {{ c.title }}
                  <div class="text-xs text-muted-foreground">{{ c.key }}</div>
                </TableCell>
                <TableCell>{{ modeLabels[c.mode] }}</TableCell>
                <TableCell>
                  <Badge :variant="c.enabled ? 'secondary' : 'outline'">{{ c.enabled ? 'Có' : 'Không' }}</Badge>
                </TableCell>
                <TableCell class="text-sm text-muted-foreground">
                  <span v-if="c.reason">{{ reasonText[c.reason] ?? c.reason }}</span>
                  <span v-if="c.requires_approval" class="block">Cần duyệt{{ c.sensitive ? ' (nhạy cảm)' : '' }}.</span>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </section>

      <section aria-labelledby="hist-heading" class="space-y-3">
        <h2 id="hist-heading" class="text-lg font-semibold">Lịch sử ủy quyền</h2>
        <p v-if="!detail.history.length" class="text-sm text-muted-foreground">Chưa có sự kiện nào.</p>
        <ol v-else class="space-y-2 text-sm">
          <li v-for="(h, i) in detail.history" :key="i" class="flex flex-wrap gap-x-3">
            <span class="w-44 text-muted-foreground">{{ date(h.created_at) }}</span>
            <span>{{ eventLabels[h.event] ?? h.event }}</span>
            <span class="text-muted-foreground">qua {{ sourceLabels[h.source] ?? h.source }}</span>
            <span v-if="h.expire_time" class="text-muted-foreground">· hết hạn {{ date(h.expire_time) }}</span>
          </li>
        </ol>
      </section>
    </template>
  </div>
</template>
