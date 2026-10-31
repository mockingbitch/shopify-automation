<script setup lang="ts">
import type { components, paths } from '~/types/api'

useHead({ title: 'Chức năng' })

type Cap = paths['/admin/capabilities']['get']['responses']['200']['content']['application/json']['data'][number]
type App = components['schemas']['ShopeeApp']

const api = useApi()
const caps = ref<Cap[]>([])
const apps = ref<App[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const fields = ref<Record<string, string>>({})
const markets = ['VN', 'SG', 'MY', 'TH', 'PH', 'ID', 'TW']
const form = reactive({ capability_key: '', shopee_app_id: '', region: '', enabled: 'off', mode: '', reason: '' })

async function load() {
  loading.value = true
  error.value = ''
  try {
    caps.value = unwrap(await api.GET('/admin/capabilities')).data
    apps.value = unwrap(await api.GET('/admin/shopee-apps')).data
    if (!form.capability_key) form.capability_key = caps.value[0]?.key ?? ''
  } catch (e) {
    error.value = toApiError(e).status === 404 ? 'Chỉ quản trị nền tảng mới mở được trang này.' : toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function save() {
  fields.value = {}
  error.value = ''
  try {
    unwrap(await api.PUT('/admin/capability-overrides', {
      body: {
        capability_key: form.capability_key, shopee_app_id: form.shopee_app_id || null, region: form.region || null,
        enabled: form.enabled === '' ? null : form.enabled === 'on', mode: (form.mode || null) as 'API' | null, reason: form.reason,
      },
    }))
    notice.value = 'Đã lưu thiết lập riêng. Thiết lập áp dụng từ lần kiểm tra chức năng tiếp theo (ngay lập tức với lượt chạy và việc mới).'
    form.reason = ''
    await load()
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    if (!Object.keys(err.fields).length) error.value = err.message
  }
}

async function remove(id: string) {
  error.value = ''
  try {
    unwrap(await api.DELETE('/admin/capability-overrides/{overrideID}', { params: { path: { overrideID: id } } }))
    notice.value = 'Đã xóa thiết lập riêng.'
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}
const modeLabels: Record<string, string> = { API: 'Shopee API', BROWSER: 'Tiện ích trình duyệt', MANUAL: 'Việc làm thủ công', UNSUPPORTED: 'Không hỗ trợ' }
const appName = (id: string | null) => (id ? (apps.value.find((a) => a.id === id)?.name ?? 'Ứng dụng không xác định') : 'Tất cả ứng dụng')
const scope = (o: { shopee_app_id: string | null; region: string | null }) => `${appName(o.shopee_app_id)} · ${o.region ?? 'mọi thị trường'}`
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Chức năng" description="Mọi thao tác trên shop và cách thực hiện. Thiết lập riêng giúp tắt một thao tác (công tắc tắt khẩn cấp) hoặc đổi cách chạy cho một ứng dụng hay thị trường." />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <form class="grid gap-3 rounded-lg border bg-background p-4 md:grid-cols-3" novalidate @submit.prevent="save">
      <h2 class="text-base font-semibold md:col-span-3">Thêm thiết lập riêng</h2>
      <FormField id="ov-cap" label="Chức năng" :error="fields.capability_key">
        <select id="ov-cap" v-model="form.capability_key" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option v-for="c in caps" :key="c.key" :value="c.key">{{ c.title }} ({{ c.key }})</option>
        </select>
      </FormField>
      <FormField id="ov-app" label="Ứng dụng Shopee" :error="fields.shopee_app_id">
        <select id="ov-app" v-model="form.shopee_app_id" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">Tất cả ứng dụng</option>
          <option v-for="a in apps" :key="a.id" :value="a.id">{{ a.name }} ({{ a.environment }})</option>
        </select>
      </FormField>
      <FormField id="ov-region" label="Thị trường" :error="fields.region">
        <select id="ov-region" v-model="form.region" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">Mọi thị trường</option>
          <option v-for="m in markets" :key="m" :value="m">{{ m }}</option>
        </select>
      </FormField>
      <FormField id="ov-enabled" label="Trạng thái" :error="fields.enabled">
        <select id="ov-enabled" v-model="form.enabled" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="off">Tắt (công tắc tắt khẩn cấp)</option>
          <option value="on">Bật</option>
          <option value="">Giữ nguyên</option>
        </select>
      </FormField>
      <FormField id="ov-mode" label="Cách chạy" :error="fields.mode">
        <select id="ov-mode" v-model="form.mode" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">Giữ nguyên</option>
          <option value="API">Shopee API</option>
          <option value="BROWSER">Tiện ích trình duyệt</option>
          <option value="MANUAL">Việc làm thủ công</option>
          <option value="UNSUPPORTED">Không hỗ trợ</option>
        </select>
      </FormField>
      <FormField id="ov-reason" label="Lý do (hiển thị cho người dùng)" :error="fields.reason">
        <Input id="ov-reason" v-model="form.reason" maxlength="300" />
      </FormField>
      <div class="flex justify-end md:col-span-3"><Button type="submit" :disabled="!form.reason.trim()">Lưu thiết lập riêng</Button></div>
    </form>

    <p v-if="loading && !caps.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Chức năng</TableHead><TableHead>Mặc định</TableHead><TableHead>Kiểm chứng</TableHead><TableHead>Thiết lập riêng</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="c in caps" :key="c.key">
            <TableCell>
              <div class="font-medium">{{ c.title }}</div>
              <div class="font-mono text-xs text-muted-foreground">{{ c.key }}</div>
            </TableCell>
            <TableCell class="text-sm">
              {{ modeLabels[c.default_mode] ?? c.default_mode }}<template v-if="c.fallback_mode"> → {{ modeLabels[c.fallback_mode] ?? c.fallback_mode }}</template>
              <Badge v-if="!c.enabled" variant="secondary" class="ml-1">Tắt</Badge>
            </TableCell>
            <TableCell class="text-xs">{{ c.verification }}</TableCell>
            <TableCell class="space-y-1 text-sm">
              <div v-for="o in c.overrides" :key="o.id" class="flex flex-wrap items-center gap-2">
                <Badge :variant="o.enabled === false ? 'destructive' : 'outline'">{{ o.enabled === false ? 'Tắt' : o.enabled ? 'Bật' : (modeLabels[o.mode ?? ''] ?? o.mode) }}</Badge>
                <span>{{ scope(o) }}: {{ o.reason }}</span>
                <ConfirmDialog title="Xóa thiết lập riêng này?" description="Chức năng sẽ trở về mặc định trong phạm vi này." confirm-label="Xóa" @confirm="remove(o.id)">
                  <template #trigger><Button variant="link" size="sm" class="h-auto p-0">Xóa</Button></template>
                </ConfirmDialog>
              </div>
              <span v-if="!c.overrides.length" class="text-muted-foreground">—</span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
