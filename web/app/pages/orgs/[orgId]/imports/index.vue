<script setup lang="ts">
import type { components } from '~/types/api'
import { batchLabels, importColumnHelp, isBatchActive, type ImportBatch } from '~/utils/products'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Nhập sản phẩm' })

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
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Nhập sản phẩm" description="Nhập sản phẩm từ file CSV hoặc Excel. Hệ thống kiểm tra từng dòng; bạn sửa các dòng lỗi ngay tại đây rồi nhập lại." />
    <FormError :message="error" />

    <form v-if="can('imports.write')" class="grid gap-3 rounded-lg border bg-background p-4 sm:grid-cols-[2fr_2fr_auto] sm:items-end" @submit.prevent="submit">
      <FormField id="imp-file" label="File (CSV hoặc XLSX, tối đa 20 MB)">
        <input id="imp-file" type="file" accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
               class="block w-full text-sm" @change="file = ($event.target as HTMLInputElement).files?.[0] ?? null">
      </FormField>
      <FormField id="imp-tpl" label="Dùng giá trị mặc định từ mẫu shop (không bắt buộc)" hint="Lấy ngành hàng, quy tắc SKU và các giá trị mặc định từ một mẫu shop đã phát hành.">
        <select id="imp-tpl" v-model="templateId" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">Không dùng</option>
          <option v-for="t in templates" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
      </FormField>
      <Button type="submit" :disabled="busy || !file">{{ busy ? 'Đang tải lên…' : 'Nhập file' }}</Button>
    </form>
    <p class="text-sm text-muted-foreground">
      Chưa rõ file cần những cột nào? <a :href="`/api/v1/orgs/${orgId}/imports/sample.csv`" class="underline" download>Tải file CSV mẫu</a>.
    </p>
    <details class="rounded-lg border bg-background p-4 text-sm">
      <summary class="cursor-pointer font-medium">Ý nghĩa các cột trong file</summary>
      <p class="mt-2 text-muted-foreground">
        Dòng đầu tiên của file là tên cột. Giữ nguyên tên cột bằng tiếng Anh như bên dưới; các cột khác sẽ bị bỏ qua.
        Sản phẩm có nhiều phân loại hàng thì mỗi phân loại là một dòng, cùng SKU sản phẩm.
      </p>
      <dl class="mt-3 grid gap-x-4 gap-y-1 sm:grid-cols-[auto_1fr]">
        <template v-for="(help, col) in importColumnHelp" :key="col">
          <dt class="font-mono">{{ col }}</dt><dd class="text-muted-foreground">{{ help }}</dd>
        </template>
      </dl>
    </details>

    <p v-if="loading && !batches.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <EmptyState v-else-if="!batches.length" title="Chưa có lô nhập nào" description="Tải file lên để tạo nhiều sản phẩm cùng lúc." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>File</TableHead><TableHead>Trạng thái</TableHead><TableHead>Số dòng</TableHead>
            <TableHead>Đã nhập</TableHead><TableHead>Cần xử lý</TableHead><TableHead>Ngày tải lên</TableHead>
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
