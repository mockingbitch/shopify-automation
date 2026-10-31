<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Nhật ký hoạt động' })
const { orgId, can } = useOrg()

const rows = ref<components['schemas']['AuditLog'][]>([])
const cursor = ref<string | null>(null)
const loading = ref(true)
const error = ref('')

async function load(more = false) {
  loading.value = true
  error.value = ''
  try {
    const res = unwrap(await useApi().GET('/orgs/{orgID}/audit-logs', {
      params: { path: { orgID: orgId.value }, query: { limit: 50, cursor: more && cursor.value ? cursor.value : undefined } },
    }))
    rows.value = more ? [...rows.value, ...res.data] : res.data
    cursor.value = res.next_cursor ?? null
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(() => { if (can('audit.read')) void load() })
</script>

<template>
  <div class="mx-auto max-w-6xl">
    <PageHeader title="Nhật ký hoạt động" description="Mọi thay đổi trong tổ chức này, mới nhất ở trên cùng." />
    <EmptyState v-if="!can('audit.read')" title="Bạn không có quyền xem trang này" description="Chỉ chủ sở hữu và quản trị viên mới xem được nhật ký hoạt động." />
    <template v-else>
      <FormError :message="error" />
      <p v-if="loading && !rows.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
      <AuditTable v-else-if="rows.length" :rows="rows" />
      <EmptyState v-else-if="!error" title="Chưa có hoạt động nào" />
      <div v-if="cursor" class="mt-4">
        <Button variant="outline" :disabled="loading" @click="load(true)">Tải thêm</Button>
      </div>
    </template>
  </div>
</template>
