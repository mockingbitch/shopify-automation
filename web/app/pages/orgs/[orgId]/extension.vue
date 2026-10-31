<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Tiện ích trình duyệt' })

type Device = components['schemas']['ExtensionDevice']

const api = useApi()
const session = useSessionStore()
const { orgId, can } = useOrg()
const devices = ref<Device[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const code = ref<{ code: string; expires_at: string } | null>(null)
const now = ref(Date.now())
const timer = setInterval(() => { now.value = Date.now() }, 1000)
onBeforeUnmount(() => clearInterval(timer))

const remaining = computed(() => {
  if (!code.value) return 0
  return Math.max(0, Math.floor((new Date(code.value.expires_at).getTime() - now.value) / 1000))
})

async function load() {
  loading.value = true
  try {
    devices.value = unwrap(await api.GET('/orgs/{orgID}/extension/devices', { params: { path: { orgID: orgId.value } } })).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function createCode() {
  error.value = ''
  try {
    code.value = unwrap(await api.POST('/orgs/{orgID}/extension/pairing-codes', { params: { path: { orgID: orgId.value } } })).data
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function revoke(d: Device) {
  error.value = ''
  try {
    unwrap(await api.DELETE('/orgs/{orgID}/extension/devices/{deviceID}', { params: { path: { orgID: orgId.value, deviceID: d.id } } }))
    notice.value = `Đã hủy ghép nối “${d.name}”.`
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}

const canRevoke = (d: Device) => d.user_id === session.user?.id || can('members.manage')
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <PageHeader title="Tiện ích trình duyệt"
                description="Một số cài đặt shop không có Shopee Open API (banner, trang trí shop, địa chỉ mới). Tiện ích sẽ làm thay bạn ngay trong tab Kênh Người Bán của bạn, bạn có thể theo dõi trực tiếp." />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <Card>
      <CardHeader>
        <CardTitle as="h2" class="text-base">Cách hoạt động</CardTitle>
      </CardHeader>
      <CardContent class="space-y-2 text-sm">
        <ol class="list-decimal space-y-1 pl-5">
          <li>Cài tiện ích vào Chrome, Edge hoặc trình duyệt Chromium khác, rồi ghép nối bằng mã lấy từ trang này.</li>
          <li>Khi quy trình cần thay đổi một mục không có Open API, việc cần làm sẽ hiện trong cửa sổ của tiện ích.</li>
          <li>Mở Kênh Người Bán, đăng nhập đúng shop, rồi bấm <strong>Chạy</strong>. Tiện ích kiểm tra ID shop trước và sẽ dừng nếu không đúng shop.</li>
          <li>Nếu Kênh Người Bán yêu cầu đăng nhập, mã xác minh hoặc CAPTCHA, tiện ích sẽ tạm dừng. Bạn tự hoàn tất, rồi bấm <strong>Tiếp tục</strong>.</li>
        </ol>
        <p class="text-muted-foreground">
          Tiện ích không bao giờ đọc mật khẩu hay cookie Shopee của bạn, và chỉ thao tác trên các trang có trong kịch bản thao tác đã kiểm chứng.
          Thao tác chưa có kịch bản đã kiểm chứng cho thị trường của bạn sẽ là việc làm thủ công.
        </p>
      </CardContent>
    </Card>

    <section v-if="can('workflows.run')" class="space-y-3 rounded-lg border bg-background p-4" aria-labelledby="pair-heading">
      <h2 id="pair-heading" class="text-base font-semibold">Ghép nối trình duyệt</h2>
      <div v-if="code && remaining > 0" class="space-y-1">
        <p class="text-sm">Nhập mã này vào cửa sổ của tiện ích:</p>
        <p class="font-mono text-3xl tracking-widest" aria-live="polite">{{ code.code }}</p>
        <p class="text-xs text-muted-foreground">Còn hiệu lực {{ Math.floor(remaining / 60) }}:{{ String(remaining % 60).padStart(2, '0') }} và chỉ dùng được một lần.</p>
      </div>
      <p v-else-if="code" class="text-sm text-muted-foreground">Mã đã hết hạn. Bạn hãy tạo mã mới.</p>
      <div class="flex gap-2">
        <Button @click="createCode">{{ code ? 'Tạo mã mới' : 'Tạo mã ghép nối' }}</Button>
        <Button v-if="code" variant="outline" @click="load">Tôi đã ghép nối xong</Button>
      </div>
    </section>

    <section class="space-y-2" aria-labelledby="devices-heading">
      <h2 id="devices-heading" class="text-lg font-semibold">Trình duyệt đã ghép nối</h2>
      <p v-if="loading && !devices.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
      <EmptyState v-else-if="!devices.length" title="Chưa ghép nối trình duyệt nào" description="Ghép nối một trình duyệt để tiện ích làm các việc không có Open API." />
      <div v-else class="overflow-x-auto rounded-lg border bg-background">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Trình duyệt</TableHead><TableHead>Người dùng</TableHead><TableHead>Phiên bản</TableHead><TableHead>Dùng lần cuối</TableHead>
              <TableHead><span class="sr-only">Thao tác</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="d in devices" :key="d.id">
              <TableCell class="font-medium">{{ d.name }}</TableCell>
              <TableCell>{{ d.user_email }}</TableCell>
              <TableCell>{{ d.extension_version || '—' }}</TableCell>
              <TableCell>{{ d.last_used_at ? fmt.format(new Date(d.last_used_at)) : '—' }}</TableCell>
              <TableCell class="text-right">
                <ConfirmDialog v-if="canRevoke(d)" :title="`Hủy ghép nối “${d.name}”?`" description="Tiện ích trên trình duyệt đó sẽ ngừng hoạt động cho đến khi được ghép nối lại."
                               confirm-label="Hủy ghép nối" @confirm="revoke(d)">
                  <template #trigger><Button variant="outline" size="sm">Hủy ghép nối</Button></template>
                </ConfirmDialog>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </section>
  </div>
</template>
