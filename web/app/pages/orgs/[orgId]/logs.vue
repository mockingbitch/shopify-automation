<script setup lang="ts">
import type { WorkflowEvent } from '~/utils/workflows'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Nhật ký thực thi' })

const api = useApi()
const { orgId } = useOrg()
const events = ref<WorkflowEvent[]>([])
const next = ref<string | null>(null)
const level = ref<'' | 'info' | 'warn' | 'error'>('')
const loading = ref(true)
const error = ref('')

async function load(more = false) {
  loading.value = true
  error.value = ''
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/workflow-events', {
      params: { path: { orgID: orgId.value }, query: { level: level.value || undefined, cursor: more ? next.value ?? undefined : undefined } },
    }))
    events.value = more ? [...events.value, ...res.data] : res.data
    next.value = res.next_cursor
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(() => load())
watch(level, () => load())

const levelLabels: Record<string, string> = { info: 'Thông tin', warn: 'Cảnh báo', error: 'Lỗi' }
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'medium' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Nhật ký thực thi" description="Mọi việc các quy trình đã làm, mới nhất ở trên cùng. Mã truy cập và dữ liệu gốc từ Shopee không bao giờ được ghi lại." />
    <FormError :message="error" />
    <div>
      <Label for="log-level" class="sr-only">Mức độ</Label>
      <select id="log-level" v-model="level" class="h-9 rounded-md border bg-background px-3 text-sm">
        <option value="">Tất cả mức độ</option>
        <option value="info">Thông tin</option>
        <option value="warn">Cảnh báo</option>
        <option value="error">Lỗi</option>
      </select>
    </div>
    <p v-if="loading && !events.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <EmptyState v-else-if="!events.length" title="Chưa có nhật ký" description="Nhật ký sẽ xuất hiện khi quy trình chạy." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Thời gian</TableHead>
            <TableHead>Mức độ</TableHead>
            <TableHead>Sự kiện</TableHead>
            <TableHead>Lượt chạy</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="ev in events" :key="ev.id">
            <TableCell class="whitespace-nowrap text-sm">{{ fmt.format(new Date(ev.created_at)) }}</TableCell>
            <TableCell>
              <Badge :variant="ev.level === 'error' ? 'destructive' : ev.level === 'warn' ? 'outline' : 'secondary'">{{ levelLabels[ev.level] ?? ev.level }}</Badge>
            </TableCell>
            <TableCell class="whitespace-normal text-sm">{{ ev.message || ev.type.replace(/_/g, ' ').toLowerCase() }}</TableCell>
            <TableCell><NuxtLink :to="`/orgs/${orgId}/workflows/${ev.run_id}`" class="text-sm underline">Mở</NuxtLink></TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <Button v-if="next" variant="outline" :disabled="loading" @click="load(true)">Tải thêm</Button>
  </div>
</template>
