<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Audit log' })
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
    <PageHeader title="Audit log" description="Every change made in this organization, newest first." />
    <EmptyState v-if="!can('audit.read')" title="You do not have access" description="Only owners and admins can view the audit log." />
    <template v-else>
      <FormError :message="error" />
      <p v-if="loading && !rows.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
      <AuditTable v-else-if="rows.length" :rows="rows" />
      <EmptyState v-else-if="!error" title="No entries yet" />
      <div v-if="cursor" class="mt-4">
        <Button variant="outline" :disabled="loading" @click="load(true)">Load more</Button>
      </div>
    </template>
  </div>
</template>
