<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
const { orgId, membership, can } = useOrg()
useHead({ title: () => membership.value?.organization.name ?? 'Overview' })

const stats = ['Connected shops', 'Active shops', 'Disconnected shops', 'Running workflows', 'Failed workflows', 'Completed setups']
const activity = ref<components['schemas']['AuditLog'][]>([])
const loadError = ref('')

onMounted(async () => {
  if (!can('audit.read')) return
  try {
    activity.value = unwrap(await useApi().GET('/orgs/{orgID}/audit-logs', { params: { path: { orgID: orgId.value }, query: { limit: 8 } } })).data
  } catch (e) {
    loadError.value = toApiError(e).message
  }
})
</script>

<template>
  <div class="mx-auto max-w-6xl">
    <PageHeader :title="membership?.organization.name ?? 'Overview'" description="Status of your shops and automation." />
    <section aria-labelledby="stats-heading">
      <h2 id="stats-heading" class="sr-only">Statistics</h2>
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <Card v-for="s in stats" :key="s">
          <CardHeader class="pb-2">
            <CardDescription>{{ s }}</CardDescription>
            <CardTitle class="text-3xl" aria-describedby="stats-note">—</CardTitle>
          </CardHeader>
        </Card>
      </div>
      <p id="stats-note" class="mt-2 text-xs text-muted-foreground">Shop and workflow numbers appear once Shopee shops can be connected (Phase 2).</p>
    </section>

    <section v-if="can('audit.read')" class="mt-8" aria-labelledby="activity-heading">
      <h2 id="activity-heading" class="mb-3 text-lg font-medium">Recent activity</h2>
      <FormError :message="loadError" />
      <AuditTable v-if="activity.length" :rows="activity" compact />
      <EmptyState v-else-if="!loadError" title="No activity yet" />
    </section>
  </div>
</template>
