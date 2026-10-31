<script setup lang="ts">
import type { components } from '~/types/api'

useHead({ title: 'Kịch bản thao tác' })

type Recipe = components['schemas']['BrowserRecipe']

const api = useApi()
const recipes = ref<Recipe[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const fields = ref<Record<string, string>>({})
const busy = ref(false)
const text = ref('')
const notes = ref('')
const open = ref<string | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    recipes.value = unwrap(await api.GET('/admin/browser-recipes')).data
  } catch (e) {
    error.value = toApiError(e).status === 404 ? 'Chỉ quản trị nền tảng mới mở được trang này.' : toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function create() {
  busy.value = true
  error.value = ''
  fields.value = {}
  let recipe: Record<string, unknown>
  try {
    recipe = JSON.parse(text.value)
  } catch {
    error.value = 'Kịch bản không phải JSON hợp lệ. Bạn hãy kiểm tra dấu ngoặc và dấu phẩy.'
    busy.value = false
    return
  }
  try {
    unwrap(await api.POST('/admin/browser-recipes', { body: { recipe, notes: notes.value } }))
    notice.value = 'Đã thêm kịch bản. Kịch bản sẽ tắt cho đến khi được kiểm chứng trên tài khoản người bán thật và được bật.'
    text.value = ''
    notes.value = ''
    await load()
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    error.value = Object.keys(err.fields).length ? 'Kịch bản có lỗi:' : err.message
  } finally {
    busy.value = false
  }
}

async function update(r: Recipe, body: { verified?: boolean; enabled?: boolean }, done: string) {
  error.value = ''
  try {
    unwrap(await api.PATCH('/admin/browser-recipes/{recipeID}', { params: { path: { recipeID: r.id } }, body }))
    notice.value = done
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Kịch bản thao tác"
                description="Các bước khai báo sẵn mà tiện ích trình duyệt chạy trong Kênh Người Bán. Chỉ bật được một phiên bản sau khi đã kiểm chứng trên tài khoản người bán thật; tắt kịch bản chính là công tắc tắt khẩn cấp." />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />
    <ul v-if="Object.keys(fields).length" class="list-disc pl-5 text-sm text-destructive">
      <li v-for="(m, f) in fields" :key="f"><span class="font-mono">{{ f }}</span>: {{ m }}</li>
    </ul>

    <form class="space-y-3 rounded-lg border bg-background p-4" @submit.prevent="create">
      <h2 class="text-base font-semibold">Thêm phiên bản</h2>
      <FormField id="rc-json" label="JSON của kịch bản" hint="Schema: internal/browser/recipe.schema.json. Nội dung được lưu nguyên từng byte vì các byte này được ký.">
        <textarea id="rc-json" v-model="text" rows="12" spellcheck="false" class="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs" />
      </FormField>
      <FormField id="rc-notes" label="Ghi chú (không bắt buộc)"><Input id="rc-notes" v-model="notes" maxlength="2000" /></FormField>
      <div class="flex justify-end"><Button type="submit" :disabled="busy || !text.trim()">Thêm phiên bản</Button></div>
    </form>

    <p v-if="loading && !recipes.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <EmptyState v-else-if="!recipes.length" title="Chưa có kịch bản nào" description="Thao tác không có Open API vẫn là việc làm thủ công cho đến khi một kịch bản đã kiểm chứng được bật." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Chức năng</TableHead><TableHead>Thị trường</TableHead><TableHead>Phiên bản</TableHead><TableHead>Trạng thái</TableHead>
            <TableHead>Lượt chạy</TableHead><TableHead><span class="sr-only">Thao tác</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <template v-for="r in recipes" :key="r.id">
            <TableRow>
              <TableCell class="font-mono text-xs">{{ r.capability }}</TableCell>
              <TableCell>{{ r.market }}</TableCell>
              <TableCell>v{{ r.version }}</TableCell>
              <TableCell class="space-y-1">
                <Badge :variant="r.enabled ? 'default' : 'secondary'">{{ r.enabled ? 'Đang bật' : 'Đang tắt' }}</Badge>
                <div class="text-xs text-muted-foreground">{{ r.verified_at ? `Đã kiểm chứng ${fmt.format(new Date(r.verified_at))}` : 'Chưa kiểm chứng' }}</div>
                <div v-if="r.disabled_reason" class="text-xs text-muted-foreground">{{ r.disabled_reason }}</div>
              </TableCell>
              <TableCell class="text-sm">{{ r.success_count }} thành công · {{ r.failure_count }} thất bại</TableCell>
              <TableCell class="space-x-2 whitespace-nowrap text-right">
                <Button variant="outline" size="sm" @click="open = open === r.id ? null : r.id">{{ open === r.id ? 'Ẩn' : 'Xem' }}</Button>
                <ConfirmDialog v-if="!r.verified_at" title="Đánh dấu đã kiểm chứng?"
                               description="Xác nhận đúng phiên bản này đã chạy thành công trên tài khoản người bán thật ở thị trường này."
                               confirm-label="Đánh dấu đã kiểm chứng" @confirm="update(r, { verified: true }, 'Đã đánh dấu là đã kiểm chứng.')">
                  <template #trigger><Button variant="outline" size="sm">Kiểm chứng</Button></template>
                </ConfirmDialog>
                <Button v-if="r.verified_at && !r.enabled" size="sm" @click="update(r, { enabled: true }, 'Đã bật. Các phiên bản khác đã được tắt.')">Bật</Button>
                <Button v-if="r.enabled" variant="outline" size="sm" @click="update(r, { enabled: false }, 'Đã tắt. Các việc sẽ chuyển sang làm thủ công.')">Tắt</Button>
              </TableCell>
            </TableRow>
            <TableRow v-if="open === r.id">
              <TableCell colspan="6">
                <pre class="max-h-96 overflow-auto rounded bg-muted p-3 text-xs">{{ JSON.stringify(r.recipe, null, 2) }}</pre>
                <p v-if="r.notes" class="mt-2 text-sm">{{ r.notes }}</p>
              </TableCell>
            </TableRow>
          </template>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
