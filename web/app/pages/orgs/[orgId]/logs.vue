<script setup lang="ts">
import type { WorkflowEvent } from '~/utils/workflows'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Execution Logs' })

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

const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Execution Logs" description="Everything the workflows did, newest first. Tokens and raw Shopee responses are never logged." />
    <FormError :message="error" />
    <div>
      <Label for="log-level" class="sr-only">Level</Label>
      <select id="log-level" v-model="level" class="h-9 rounded-md border bg-background px-3 text-sm">
        <option value="">All levels</option>
        <option value="info">Info</option>
        <option value="warn">Warnings</option>
        <option value="error">Errors</option>
      </select>
    </div>
    <p v-if="loading && !events.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <EmptyState v-else-if="!events.length" title="No log entries" description="Logs appear when workflows run." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Time</TableHead>
            <TableHead>Level</TableHead>
            <TableHead>Event</TableHead>
            <TableHead>Run</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="ev in events" :key="ev.id">
            <TableCell class="whitespace-nowrap text-sm">{{ fmt.format(new Date(ev.created_at)) }}</TableCell>
            <TableCell>
              <Badge :variant="ev.level === 'error' ? 'destructive' : ev.level === 'warn' ? 'outline' : 'secondary'">{{ ev.level }}</Badge>
            </TableCell>
            <TableCell class="whitespace-normal text-sm">{{ ev.message || ev.type.replace(/_/g, ' ').toLowerCase() }}</TableCell>
            <TableCell><NuxtLink :to="`/orgs/${orgId}/workflows/${ev.run_id}`" class="text-sm underline">Open</NuxtLink></TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <Button v-if="next" variant="outline" :disabled="loading" @click="load(true)">Load more</Button>
  </div>
</template>
