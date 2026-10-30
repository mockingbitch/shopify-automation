<script setup lang="ts">
import type { components } from '~/types/api'
import { modeLabels, reasonText } from '~/utils/shops'
import { isTerminal, runStatusLabels, runVariant, skipReasons, stepLabels, type RunView, type WaitOutput, type WorkflowEvent, type WorkflowStep } from '~/utils/workflows'

definePageMeta({ middleware: 'org' })

type Change = components['schemas']['Change']

const api = useApi()
const route = useRoute()
const session = useSessionStore()
const { orgId, can, role } = useOrg()
const runId = computed(() => String(route.params.runId))
const path = computed(() => ({ orgID: orgId.value, runID: runId.value }))

const view = ref<RunView>()
const shopName = ref('')
const events = ref<WorkflowEvent[]>([])
const cursor = ref<string | null>(null)
const error = ref('')
const notice = ref('')
const busy = ref(false)
const selected = ref<string[]>([])
const comment = ref('')
const notes = ref<Record<string, string>>({})

useHead({ title: () => `Workflow · ${shopName.value || 'run'}` })

const run = computed(() => view.value?.run)
const approval = computed(() => view.value?.pending_approval ?? null)
const changes = computed<Change[]>(() => approval.value?.diff.changes ?? [])
const blocked = computed<Change[]>(() => approval.value?.diff.blocked ?? [])
const ownRun = computed(() => run.value?.created_by === session.user?.id)
const canDecide = computed(() => can('approvals.decide'))
const template = computed(() => {
  const i = (run.value?.input ?? {}) as { template_name?: string; version_no?: number; template_id?: string }
  return i
})
const report = computed(() => (run.value?.status && isTerminal(run.value.status) ? (run.value.output as Record<string, unknown>) : null))

async function load() {
  view.value = unwrap(await api.GET('/orgs/{orgID}/workflows/{runID}', { params: { path: path.value } })).data
  if (approval.value && !selected.value.length) {
    selected.value = changes.value.filter((c) => c.default_selected).map((c) => c.path)
  }
}

async function loadEvents() {
  const res = unwrap(await api.GET('/orgs/{orgID}/workflows/{runID}/events', {
    params: { path: path.value, query: { after: cursor.value ?? undefined } },
  }))
  events.value = [...events.value, ...res.data]
  cursor.value = res.next_cursor
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  try {
    await Promise.all([load(), loadEvents()])
    if (run.value) {
      const s = unwrap(await api.GET('/orgs/{orgID}/shops/{shopID}', { params: { path: { orgID: orgId.value, shopID: run.value.shop_id } } })).data
      shopName.value = s.shop.shop_name ?? `Shop ${s.shop.shopee_shop_id}`
    }
  } catch (e) {
    error.value = toApiError(e).message
  }
  // Poll while the run is active.
  timer = setInterval(async () => {
    if (!run.value || isTerminal(run.value.status) || busy.value) return
    try {
      await Promise.all([load(), loadEvents()])
    } catch { /* keep polling */ }
  }, 3000)
})
onBeforeUnmount(() => clearInterval(timer))

async function act(fn: () => Promise<unknown>, done: string) {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await fn()
    await Promise.all([load(), loadEvents()])
    notice.value = done
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    busy.value = false
  }
}

const decide = (decision: 'APPROVE' | 'REJECT') => act(async () => {
  unwrap(await api.POST('/orgs/{orgID}/approvals/{approvalID}/decision', {
    params: { path: { orgID: orgId.value, approvalID: approval.value!.id } },
    body: { decision, selected_change_paths: decision === 'APPROVE' ? selected.value : [], comment: comment.value || undefined },
  }))
  selected.value = []
}, decision === 'APPROVE' ? 'Approved. Applying the selected changes.' : 'Rejected. The run was cancelled.')

const complete = (step: WorkflowStep) => act(async () => {
  unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/steps/{stepID}/complete', {
    params: { path: { ...path.value, stepID: step.id } }, body: { note: notes.value[step.id] || undefined },
  }))
}, 'Marked as done. The run continues.')

const cancel = () => act(async () => unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/cancel', { params: { path: path.value }, body: {} })), 'Run cancelled.')
const retry = () => act(async () => unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/retry', { params: { path: path.value } })), 'Retrying the failed steps.')
const resume = () => act(async () => unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/resume', { params: { path: path.value } })), 'Resumed.')

const waiting = (s: WorkflowStep) => (s.status === 'WAITING_USER' ? (s.output as WaitOutput) : null)
const hasResumable = computed(() => view.value?.steps.some((s) => s.status === 'WAITING_USER' && (s.output as WaitOutput)?.reason !== 'manual_tasks'))
const errMsg = (s: WorkflowStep) => (s.error as { message?: string } | null)?.message

function show(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'string') return v
  if (typeof v === 'boolean') return v ? 'Yes' : 'No'
  if (Array.isArray(v)) return v.map(show).join(', ')
  if (typeof v === 'object') return Object.entries(v as Record<string, unknown>).map(([k, x]) => `${k.replace(/_/g, ' ')}: ${show(x)}`).join('; ')
  return String(v)
}
function eventText(ev: WorkflowEvent): string {
  const d = (ev.data ?? {}) as { step?: string; status?: string; to?: keyof typeof runStatusLabels }
  const label = d.step ? (stepLabels[d.step] ?? d.step) : ''
  switch (ev.type) {
    case 'RUN_STATUS_CHANGED':
      return `Run is now: ${d.to ? runStatusLabels[d.to] : ''}`
    case 'STEP_ATTEMPT_STARTED':
      return `Started: ${label}`
    case 'STEP_STATUS_CHANGED':
      return ev.message ? `${label}: ${ev.message}` : `${label}: ${(d.status ?? '').replace('_', ' ').toLowerCase()}`
    default:
      return ev.message || ev.type.replace(/_/g, ' ').toLowerCase()
  }
}

const stepIcon: Record<string, string> = {
  SUCCEEDED: '✓', SKIPPED: '–', FAILED: '✕', CANCELLED: '⦸', RUNNING: '…', PENDING: '○', WAITING_APPROVAL: '!', WAITING_USER: '!',
}
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'medium' })
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-6">
    <NuxtLink :to="`/orgs/${orgId}/workflows`" class="text-sm text-muted-foreground hover:underline">← All workflows</NuxtLink>
    <FormError :message="error" />

    <template v-if="view && run">
      <PageHeader :title="`Shop setup: ${shopName}`"
                  :description="`${template.template_name ?? run.definition} v${template.version_no ?? ''} · started ${fmt.format(new Date(run.created_at))}`">
        <template #actions>
          <Button v-if="hasResumable && can('workflows.run')" :disabled="busy" @click="resume">Resume</Button>
          <Button v-if="(run.status === 'FAILED' || run.status === 'PARTIALLY_COMPLETED') && can('workflows.retry')" :disabled="busy" @click="retry">Retry failed steps</Button>
          <ConfirmDialog v-if="!isTerminal(run.status) && !run.cancel_requested && can('workflows.cancel')" title="Cancel this run?"
                         description="Remaining steps are cancelled. Changes already applied on Shopee are not rolled back." confirm-label="Cancel run" @confirm="cancel">
            <template #trigger><Button variant="outline" :disabled="busy">Cancel run</Button></template>
          </ConfirmDialog>
        </template>
      </PageHeader>

      <div class="flex flex-wrap items-center gap-3 text-sm">
        <Badge :variant="runVariant(run.status)">{{ runStatusLabels[run.status] }}</Badge>
        <span class="text-muted-foreground">{{ view.progress.done }} of {{ view.progress.total }} steps done</span>
      </div>
      <div aria-live="polite">
        <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
      </div>

      <!-- Approval -->
      <Card v-if="approval">
        <CardHeader>
          <CardTitle as="h2" class="text-base">Approve changes</CardTitle>
          <CardDescription>
            Tick the changes to apply. Sensitive changes (shop name, addresses, shipping) are never pre-selected.
            Changes marked “Manual task” become a task for you after approval.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead><span class="sr-only">Apply</span></TableHead>
                  <TableHead>Change</TableHead>
                  <TableHead>Now</TableHead>
                  <TableHead>New</TableHead>
                  <TableHead>Runs through</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="c in changes" :key="c.path">
                  <TableCell>
                    <input v-model="selected" type="checkbox" class="size-4" :value="c.path" :disabled="!canDecide" :aria-label="`Apply ${c.path}`">
                  </TableCell>
                  <TableCell class="font-mono text-xs">
                    {{ c.path }}
                    <Badge v-if="c.sensitive" variant="destructive" class="ml-1">Sensitive</Badge>
                  </TableCell>
                  <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ c.current_known ? show(c.current) : 'Unknown' }}</TableCell>
                  <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ show(c.target) }}</TableCell>
                  <TableCell class="whitespace-normal text-sm">
                    {{ modeLabels[c.mode] }}
                    <span v-if="c.note" class="block text-xs text-muted-foreground">{{ c.note }}</span>
                  </TableCell>
                </TableRow>
                <TableRow v-for="c in blocked" :key="c.path" class="opacity-60">
                  <TableCell />
                  <TableCell class="font-mono text-xs">{{ c.path }}</TableCell>
                  <TableCell colspan="3" class="whitespace-normal text-xs">Not available: {{ reasonText[c.reason] ?? c.reason }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
          <template v-if="canDecide">
            <p v-if="role === 'operator' && ownRun" class="text-sm text-amber-700">
              You started this run. Operators can approve their own runs only if the organization allows it.
            </p>
            <div>
              <Label for="approval-comment">Comment (optional)</Label>
              <Input id="approval-comment" v-model="comment" maxlength="1000" />
            </div>
            <div class="flex flex-wrap gap-2">
              <Button :disabled="busy || !selected.length" @click="decide('APPROVE')">Approve {{ selected.length }} change{{ selected.length === 1 ? '' : 's' }}</Button>
              <ConfirmDialog title="Reject these changes?" description="The run is cancelled and nothing is applied." confirm-label="Reject" @confirm="decide('REJECT')">
                <template #trigger><Button variant="outline" :disabled="busy">Reject</Button></template>
              </ConfirmDialog>
            </div>
          </template>
          <p v-else class="text-sm text-muted-foreground">Someone with approval rights must decide.</p>
        </CardContent>
      </Card>

      <!-- Steps -->
      <section aria-labelledby="steps-heading" class="space-y-2">
        <h2 id="steps-heading" class="text-lg font-semibold">Steps</h2>
        <ol class="space-y-2">
          <li v-for="s in view.steps" :key="s.id" class="rounded-lg border bg-background p-3">
            <div class="flex flex-wrap items-center gap-2">
              <span aria-hidden="true" class="w-4 text-center">{{ stepIcon[s.status] }}</span>
              <span class="font-medium">{{ stepLabels[s.key] ?? s.key }}</span>
              <Badge variant="outline">{{ s.status.replace('_', ' ').toLowerCase() }}</Badge>
              <span v-if="s.attempt_count > 1 && (errMsg(s) || s.next_attempt_at)" class="text-xs text-muted-foreground">attempt {{ s.attempt_count }} of {{ s.max_attempts }}</span>
              <span v-if="s.next_attempt_at" class="text-xs text-muted-foreground">next try {{ fmt.format(new Date(s.next_attempt_at)) }}</span>
            </div>
            <p v-if="s.skip_reason" class="ml-6 mt-1 text-sm text-muted-foreground">{{ skipReasons[s.skip_reason] ?? s.skip_reason }}</p>
            <p v-if="errMsg(s)" class="ml-6 mt-1 text-sm text-destructive">{{ errMsg(s) }}</p>

            <div v-if="waiting(s)" class="ml-6 mt-2 space-y-3">
              <p class="text-sm">{{ waiting(s)!.message }}</p>
              <template v-if="waiting(s)!.reason === 'manual_tasks'">
                <div v-for="task in waiting(s)!.tasks" :key="task.key" class="rounded border bg-muted/30 p-3 text-sm">
                  <p class="font-medium">{{ task.title }}</p>
                  <p class="text-muted-foreground">{{ task.instructions }}</p>
                  <p v-if="task.data" class="mt-1 break-words">{{ show(task.data) }}</p>
                </div>
                <div v-if="can('workflows.run')" class="flex flex-wrap items-end gap-2">
                  <div class="min-w-64 flex-1">
                    <Label :for="`note-${s.id}`" class="text-xs">Note (optional)</Label>
                    <Input :id="`note-${s.id}`" v-model="notes[s.id]" />
                  </div>
                  <Button :disabled="busy" @click="complete(s)">Mark as done</Button>
                </div>
              </template>
              <p v-else-if="waiting(s)!.reason === 'reconnect_required'" class="text-sm">
                <NuxtLink :to="`/orgs/${orgId}/shops/${run.shop_id}`" class="underline">Reconnect the shop</NuxtLink>, then press Resume.
              </p>
            </div>
          </li>
        </ol>
      </section>

      <!-- Report -->
      <Card v-if="report && Object.keys(report).length">
        <CardHeader><CardTitle as="h2" class="text-base">Report</CardTitle></CardHeader>
        <CardContent>
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt class="text-muted-foreground">Changes found</dt><dd>{{ report.changes_found }}</dd>
            <dt class="text-muted-foreground">Approved</dt><dd>{{ report.selected }}</dd>
            <dt class="text-muted-foreground">Applied through the API</dt><dd>{{ show(report.applied) }}</dd>
            <dt class="text-muted-foreground">Manual tasks</dt><dd>{{ report.manual_tasks }}</dd>
            <dt class="text-muted-foreground">Not selected</dt><dd>{{ show(report.not_selected) }}</dd>
            <dt class="text-muted-foreground">Not available</dt><dd>{{ show(report.blocked) }}</dd>
            <dt class="text-muted-foreground">Mismatches after applying</dt><dd>{{ report.verification_mismatches }}</dd>
          </dl>
        </CardContent>
      </Card>

      <!-- Events -->
      <section aria-labelledby="events-heading" class="space-y-2">
        <h2 id="events-heading" class="text-lg font-semibold">Activity</h2>
        <ol class="space-y-1 text-sm">
          <li v-for="ev in [...events].reverse()" :key="ev.id" class="flex flex-wrap gap-x-3">
            <span class="w-44 shrink-0 text-muted-foreground">{{ fmt.format(new Date(ev.created_at)) }}</span>
            <span :class="ev.level === 'error' ? 'text-destructive' : ev.level === 'warn' ? 'text-amber-700' : ''">
              {{ eventText(ev) }}
            </span>
          </li>
        </ol>
      </section>
    </template>
  </div>
</template>
