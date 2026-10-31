<script setup lang="ts">
import type { components } from '~/types/api'
import { modeLabels, reasonText } from '~/utils/shops'
import { eventTypeLabels, isTerminal, runStatusLabels, runVariant, skipReasons, stepLabels, stepStatusLabels, waitReasons, type RunView, type WaitOutput, type WorkflowEvent, type WorkflowStep } from '~/utils/workflows'

definePageMeta({ middleware: 'org' })

type Change = components['schemas']['Change']

const api = useApi()

const pathLabel = usePathLabel()
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

useHead({ title: () => `Quy trình · ${shopName.value || 'lượt chạy'}` })

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
const isPublish = computed(() => run.value?.definition === 'PRODUCT_PUBLISH')
// product_id -> SKU, from the prepare step (labels fan-out item steps).
const skus = computed<Record<string, string>>(() => {
  const prep = view.value?.steps.find((s) => s.key === 'prepare_products')?.output as { plans?: { product_id: string; sku: string }[] } | null
  return Object.fromEntries((prep?.plans ?? []).map((p) => [p.product_id, p.sku]))
})
const stepTitle = (s: WorkflowStep) => {
  const base = stepLabels[s.key] ?? s.key
  return s.item_key ? `${base}: ${skus.value[s.item_key] ?? s.item_key}` : base
}
const itemResult = (s: WorkflowStep) => (s.item_key && s.status === 'SUCCEEDED' ? (s.output as { shopee_item_id?: string; models?: number; adopted?: boolean } | null) : null)
const target = (c: Change) => (c.target ?? {}) as { sku?: string; name?: string; price?: string; category_id?: number }
const report = computed(() => (run.value?.status && isTerminal(run.value.status) ? (run.value.output as Record<string, unknown>) : null))

type BrowserTask = components['schemas']['BrowserTask']
const browserTasks = ref<Record<string, BrowserTask>>({})
const browserLabels: Record<BrowserTask['status'], string> = {
  QUEUED: 'Chờ tiện ích trình duyệt', RUNNING: 'Tiện ích trình duyệt đang chạy', WAITING_USER: 'Tạm dừng: hãy hoàn tất trang trong Kênh Người Bán',
  SUCCEEDED: 'Tiện ích trình duyệt đã làm xong', FAILED: 'Tiện ích trình duyệt không làm xong được', CANCELLED: 'Đã hủy',
}
const outcomeLabels: Record<string, string> = { ok: 'xong', failed: 'thất bại', skipped: 'bỏ qua' }

async function loadBrowserTasks() {
  const hasBrowser = view.value?.steps.some((s) => ((s.output as WaitOutput | null)?.tasks ?? []).some((t) => t.browser_task_id))
  if (!hasBrowser) return
  const list = unwrap(await api.GET('/orgs/{orgID}/workflows/{runID}/browser-tasks', { params: { path: path.value } })).data
  browserTasks.value = Object.fromEntries(list.map((t) => [t.id, t]))
}

const requeue = (id: string) => act(async () => {
  unwrap(await api.POST('/orgs/{orgID}/browser-tasks/{taskID}/requeue', { params: { path: { orgID: orgId.value, taskID: id } } }))
}, 'Đã xếp hàng lại cho tiện ích trình duyệt.')

async function openScreenshot(taskID: string, screenshotID: string) {
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/browser-tasks/{taskID}/screenshots/{screenshotID}', {
      params: { path: { orgID: orgId.value, taskID, screenshotID } },
    }))
    window.open(res.data.url, '_blank', 'noopener')
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function load() {
  view.value = unwrap(await api.GET('/orgs/{orgID}/workflows/{runID}', { params: { path: path.value } })).data
  await loadBrowserTasks().catch(() => { /* optional */ })
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
}, decision === 'APPROVE' ? 'Đã duyệt. Đang áp dụng các thay đổi đã chọn.' : 'Đã từ chối. Lượt chạy đã bị hủy.')

const complete = (step: WorkflowStep) => act(async () => {
  unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/steps/{stepID}/complete', {
    params: { path: { ...path.value, stepID: step.id } }, body: { note: notes.value[step.id] || undefined },
  }))
}, 'Đã đánh dấu là xong. Lượt chạy tiếp tục.')

const cancel = () => act(async () => unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/cancel', { params: { path: path.value }, body: {} })), 'Đã hủy lượt chạy.')
const retry = () => act(async () => unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/retry', { params: { path: path.value } })), 'Đang thử lại các bước thất bại.')
const resume = () => act(async () => unwrap(await api.POST('/orgs/{orgID}/workflows/{runID}/resume', { params: { path: path.value } })), 'Đã tiếp tục.')

const waiting = (s: WorkflowStep) => (s.status === 'WAITING_USER' ? (s.output as WaitOutput) : null)
const hasResumable = computed(() => view.value?.steps.some((s) => s.status === 'WAITING_USER' && (s.output as WaitOutput)?.reason !== 'manual_tasks'))
const errMsg = (s: WorkflowStep) => (s.error as { message?: string } | null)?.message

function show(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'string') return v
  if (typeof v === 'boolean') return v ? 'Có' : 'Không'
  if (Array.isArray(v)) return v.map(show).join(', ')
  if (typeof v === 'object') return Object.entries(v as Record<string, unknown>).map(([k, x]) => `${k.replace(/_/g, ' ')}: ${show(x)}`).join('; ')
  return String(v)
}
function eventText(ev: WorkflowEvent): string {
  const d = (ev.data ?? {}) as { step?: string; status?: string; to?: keyof typeof runStatusLabels }
  const label = d.step ? (stepLabels[d.step] ?? d.step) : ''
  switch (ev.type) {
    case 'RUN_STATUS_CHANGED':
      return `Lượt chạy chuyển sang: ${d.to ? runStatusLabels[d.to] : ''}`
    case 'STEP_ATTEMPT_STARTED':
      return `Bắt đầu: ${label}`
    case 'STEP_STATUS_CHANGED':
      return ev.message ? `${label}: ${ev.message}` : `${label}: ${(stepStatusLabels[d.status as WorkflowStep['status']] ?? d.status ?? '').toLowerCase()}`
    default:
      return ev.message || eventTypeLabels[ev.type] || ev.type.replace(/_/g, ' ').toLowerCase()
  }
}

const stepIcon: Record<string, string> = {
  SUCCEEDED: '✓', SKIPPED: '–', FAILED: '✕', CANCELLED: '⦸', RUNNING: '…', PENDING: '○', WAITING_APPROVAL: '!', WAITING_USER: '!',
}
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'medium' })
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-6">
    <NuxtLink :to="`/orgs/${orgId}/workflows`" class="text-sm text-muted-foreground hover:underline">← Tất cả quy trình</NuxtLink>
    <FormError :message="error" />

    <template v-if="view && run">
      <PageHeader v-if="isPublish" :title="`Đăng sản phẩm: ${shopName}`"
                  :description="`${((run.input as { product_ids?: string[] }).product_ids ?? []).length} sản phẩm · bắt đầu lúc ${fmt.format(new Date(run.created_at))}`">
        <template #actions>
          <Button v-if="(run.status === 'FAILED' || run.status === 'PARTIALLY_COMPLETED') && can('workflows.retry')" :disabled="busy" @click="retry">Thử lại các sản phẩm thất bại</Button>
          <ConfirmDialog v-if="!isTerminal(run.status) && !run.cancel_requested && can('workflows.cancel')" title="Hủy lượt chạy này?"
                         description="Các sản phẩm chưa được tạo sẽ bị bỏ qua. Sản phẩm đã tạo trên Shopee vẫn được giữ nguyên." confirm-label="Hủy lượt chạy" @confirm="cancel">
            <template #trigger><Button variant="outline" :disabled="busy">Hủy lượt chạy</Button></template>
          </ConfirmDialog>
        </template>
      </PageHeader>
      <PageHeader v-else :title="`Thiết lập shop: ${shopName}`"
                  :description="`${template.template_name ?? run.definition} v${template.version_no ?? ''} · bắt đầu lúc ${fmt.format(new Date(run.created_at))}`">
        <template #actions>
          <Button v-if="hasResumable && can('workflows.run')" :disabled="busy" @click="resume">Tiếp tục</Button>
          <Button v-if="(run.status === 'FAILED' || run.status === 'PARTIALLY_COMPLETED') && can('workflows.retry')" :disabled="busy" @click="retry">Thử lại các bước thất bại</Button>
          <ConfirmDialog v-if="!isTerminal(run.status) && !run.cancel_requested && can('workflows.cancel')" title="Hủy lượt chạy này?"
                         description="Các bước còn lại sẽ bị hủy. Những thay đổi đã áp dụng trên Shopee không được hoàn tác." confirm-label="Hủy lượt chạy" @confirm="cancel">
            <template #trigger><Button variant="outline" :disabled="busy">Hủy lượt chạy</Button></template>
          </ConfirmDialog>
        </template>
      </PageHeader>

      <div class="flex flex-wrap items-center gap-3 text-sm">
        <Badge :variant="runVariant(run.status)">{{ runStatusLabels[run.status] }}</Badge>
        <span class="text-muted-foreground">Đã xong {{ view.progress.done }}/{{ view.progress.total }} bước</span>
      </div>
      <div aria-live="polite">
        <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
      </div>

      <!-- Product approval -->
      <Card v-if="approval && isPublish">
        <CardHeader>
          <CardTitle as="h2" class="text-base">Duyệt sản phẩm</CardTitle>
          <CardDescription>
            Sản phẩm được đánh dấu sẽ được tạo trên Shopee. Sản phẩm không qua kiểm tra được liệt kê kèm lý do; hãy sửa rồi đăng lại.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead><span class="sr-only">Đăng</span></TableHead>
                  <TableHead>Sản phẩm</TableHead><TableHead>Giá</TableHead><TableHead>Ngành hàng</TableHead><TableHead>Ghi chú</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="c in changes" :key="c.path">
                  <TableCell><input v-model="selected" type="checkbox" class="size-4" :value="c.path" :disabled="!canDecide" :aria-label="`Đăng ${target(c).sku}`"></TableCell>
                  <TableCell><div class="font-medium">{{ target(c).name }}</div><div class="text-xs text-muted-foreground">{{ target(c).sku }}</div></TableCell>
                  <TableCell>{{ target(c).price }}</TableCell>
                  <TableCell>{{ target(c).category_id }}</TableCell>
                  <TableCell class="max-w-72 whitespace-normal text-xs text-muted-foreground">{{ c.note }}</TableCell>
                </TableRow>
                <TableRow v-for="c in blocked" :key="c.path" class="bg-destructive/5">
                  <TableCell />
                  <TableCell><div class="font-medium">{{ target(c).name || 'Sản phẩm đã bị xóa' }}</div><div class="text-xs text-muted-foreground">{{ target(c).sku }}</div></TableCell>
                  <TableCell>{{ target(c).price }}</TableCell>
                  <TableCell>{{ target(c).category_id || '—' }}</TableCell>
                  <TableCell class="max-w-72 whitespace-normal text-xs text-destructive">{{ c.note }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
          <template v-if="canDecide">
            <div>
              <Label for="approval-comment-p">Nhận xét (không bắt buộc)</Label>
              <Input id="approval-comment-p" v-model="comment" maxlength="1000" />
            </div>
            <div class="flex flex-wrap gap-2">
              <Button :disabled="busy || !selected.length" @click="decide('APPROVE')">Đăng {{ selected.length }} sản phẩm</Button>
              <ConfirmDialog title="Từ chối?" description="Lượt chạy sẽ bị hủy và không sản phẩm nào được tạo." confirm-label="Từ chối" @confirm="decide('REJECT')">
                <template #trigger><Button variant="outline" :disabled="busy">Từ chối</Button></template>
              </ConfirmDialog>
            </div>
          </template>
          <p v-else class="text-sm text-muted-foreground">Cần người có quyền duyệt quyết định.</p>
        </CardContent>
      </Card>

      <!-- Approval -->
      <Card v-else-if="approval">
        <CardHeader>
          <CardTitle as="h2" class="text-base">Duyệt thay đổi</CardTitle>
          <CardDescription>
            Đánh dấu các thay đổi muốn áp dụng. Thay đổi nhạy cảm (tên shop, địa chỉ, vận chuyển) không bao giờ được chọn sẵn.
            Thay đổi ghi “Việc làm thủ công” sẽ thành việc bạn cần làm sau khi duyệt.
          </CardDescription>
        </CardHeader>
        <CardContent class="space-y-4">
          <div class="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead><span class="sr-only">Áp dụng</span></TableHead>
                  <TableHead>Thay đổi</TableHead>
                  <TableHead>Hiện tại</TableHead>
                  <TableHead>Mới</TableHead>
                  <TableHead>Thực hiện qua</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow v-for="c in changes" :key="c.path">
                  <TableCell>
                    <input v-model="selected" type="checkbox" class="size-4" :value="c.path" :disabled="!canDecide" :aria-label="`Áp dụng: ${pathLabel(c.path)}`">
                  </TableCell>
                  <TableCell class="text-sm">
                    {{ pathLabel(c.path) }}
                    <Badge v-if="c.sensitive" variant="destructive" class="ml-1">Nhạy cảm</Badge>
                  </TableCell>
                  <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ c.current_known ? show(c.current) : 'Chưa rõ' }}</TableCell>
                  <TableCell class="max-w-56 whitespace-normal break-words text-sm">{{ show(c.target) }}</TableCell>
                  <TableCell class="whitespace-normal text-sm">
                    {{ modeLabels[c.mode] }}
                    <span v-if="c.note" class="block text-xs text-muted-foreground">{{ c.note }}</span>
                  </TableCell>
                </TableRow>
                <TableRow v-for="c in blocked" :key="c.path" class="opacity-60">
                  <TableCell />
                  <TableCell class="text-sm">{{ pathLabel(c.path) }}</TableCell>
                  <TableCell colspan="3" class="whitespace-normal text-xs">Không thực hiện được: {{ reasonText[c.reason] ?? c.reason }}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
          <template v-if="canDecide">
            <p v-if="role === 'operator' && ownRun" class="text-sm text-amber-700">
              Bạn đã bắt đầu lượt chạy này. Nhân viên vận hành chỉ được tự duyệt lượt chạy của mình nếu tổ chức cho phép.
            </p>
            <div>
              <Label for="approval-comment">Nhận xét (không bắt buộc)</Label>
              <Input id="approval-comment" v-model="comment" maxlength="1000" />
            </div>
            <div class="flex flex-wrap gap-2">
              <Button :disabled="busy || !selected.length" @click="decide('APPROVE')">Duyệt {{ selected.length }} thay đổi</Button>
              <ConfirmDialog title="Từ chối các thay đổi này?" description="Lượt chạy sẽ bị hủy và không có thay đổi nào được áp dụng." confirm-label="Từ chối" @confirm="decide('REJECT')">
                <template #trigger><Button variant="outline" :disabled="busy">Từ chối</Button></template>
              </ConfirmDialog>
            </div>
          </template>
          <p v-else class="text-sm text-muted-foreground">Cần người có quyền duyệt quyết định.</p>
        </CardContent>
      </Card>

      <!-- Steps -->
      <section aria-labelledby="steps-heading" class="space-y-2">
        <h2 id="steps-heading" class="text-lg font-semibold">Các bước</h2>
        <ol class="space-y-2">
          <li v-for="s in view.steps" :key="s.id" class="rounded-lg border bg-background p-3">
            <div class="flex flex-wrap items-center gap-2">
              <span aria-hidden="true" class="w-4 text-center">{{ stepIcon[s.status] }}</span>
              <span class="font-medium">{{ stepTitle(s) }}</span>
              <Badge variant="outline">{{ stepStatusLabels[s.status] ?? s.status }}</Badge>
              <span v-if="s.attempt_count > 1 && (errMsg(s) || s.next_attempt_at)" class="text-xs text-muted-foreground">lần thử {{ s.attempt_count }}/{{ s.max_attempts }}</span>
              <span v-if="s.next_attempt_at" class="text-xs text-muted-foreground">thử lại lúc {{ fmt.format(new Date(s.next_attempt_at)) }}</span>
            </div>
            <p v-if="s.skip_reason" class="ml-6 mt-1 text-sm text-muted-foreground">{{ skipReasons[s.skip_reason] ?? s.skip_reason }}</p>
            <p v-if="errMsg(s)" class="ml-6 mt-1 text-sm text-destructive">{{ errMsg(s) }}</p>
            <p v-if="itemResult(s)" class="ml-6 mt-1 text-sm text-muted-foreground">
              Sản phẩm Shopee {{ itemResult(s)!.shopee_item_id }}<template v-if="itemResult(s)!.models">, {{ itemResult(s)!.models }} phân loại hàng</template><template v-if="itemResult(s)!.adopted"> (tìm thấy theo SKU sau một phản hồi không rõ ràng; không bị tạo hai lần)</template>
            </p>

            <div v-if="waiting(s)" class="ml-6 mt-2 space-y-3">
              <p class="text-sm">{{ waiting(s)!.message || waitReasons[waiting(s)!.reason ?? ''] || 'Bước này đang chờ bạn xử lý.' }}</p>
              <template v-if="waiting(s)!.reason === 'manual_tasks'">
                <div v-for="task in waiting(s)!.tasks" :key="task.key" class="rounded border bg-muted/30 p-3 text-sm">
                  <p class="font-medium">{{ task.title }}</p>
                  <p class="text-muted-foreground">{{ task.instructions }}</p>
                  <p v-if="task.data" class="mt-1 break-words">{{ show(task.data) }}</p>
                  <div v-if="task.browser_task_id && browserTasks[task.browser_task_id]" class="mt-2 space-y-1 border-t pt-2">
                    <p class="flex flex-wrap items-center gap-2">
                      <Badge :variant="browserTasks[task.browser_task_id]!.status === 'FAILED' ? 'destructive' : 'outline'">
                        {{ browserLabels[browserTasks[task.browser_task_id]!.status] }}
                      </Badge>
                      <span v-if="browserTasks[task.browser_task_id]!.status_message" class="text-xs text-muted-foreground">{{ browserTasks[task.browser_task_id]!.status_message }}</span>
                    </p>
                    <p v-if="browserTasks[task.browser_task_id]!.status === 'QUEUED'" class="text-xs text-muted-foreground">
                      Mở Kênh Người Bán của shop này và bấm Chạy trong cửa sổ tiện ích, hoặc tự làm thủ công rồi đánh dấu bước là xong.
                    </p>
                    <ol v-if="browserTasks[task.browser_task_id]!.status === 'FAILED' && browserTasks[task.browser_task_id]!.step_log?.length" class="list-decimal pl-5 text-xs">
                      <li v-for="l in browserTasks[task.browser_task_id]!.step_log" :key="l.index" :class="l.outcome === 'failed' ? 'text-destructive' : 'text-muted-foreground'">
                        {{ l.action }}: {{ outcomeLabels[l.outcome] ?? l.outcome }}<template v-if="l.error"> ({{ l.error }})</template>
                      </li>
                    </ol>
                    <div class="flex flex-wrap gap-2">
                      <Button v-if="browserTasks[task.browser_task_id]!.status === 'FAILED' && can('workflows.run')" size="sm" variant="outline" :disabled="busy"
                              @click="requeue(task.browser_task_id)">Chạy lại trong tiện ích</Button>
                      <Button v-for="(sid, i) in browserTasks[task.browser_task_id]!.screenshot_ids ?? []" :key="sid" size="sm" variant="outline"
                              @click="openScreenshot(task.browser_task_id, sid)">Ảnh chụp màn hình {{ i + 1 }}</Button>
                    </div>
                  </div>
                </div>
                <div v-if="can('workflows.run')" class="flex flex-wrap items-end gap-2">
                  <div class="min-w-64 flex-1">
                    <Label :for="`note-${s.id}`" class="text-xs">Ghi chú (không bắt buộc)</Label>
                    <Input :id="`note-${s.id}`" v-model="notes[s.id]" />
                  </div>
                  <Button :disabled="busy" @click="complete(s)">Đánh dấu là xong</Button>
                </div>
              </template>
              <p v-else-if="waiting(s)!.reason === 'reconnect_required'" class="text-sm">
                <NuxtLink :to="`/orgs/${orgId}/shops/${run.shop_id}`" class="underline">Kết nối lại shop</NuxtLink>, rồi bấm Tiếp tục.
              </p>
            </div>
          </li>
        </ol>
      </section>

      <!-- Report -->
      <Card v-if="report && isPublish && Object.keys(report).length">
        <CardHeader><CardTitle as="h2" class="text-base">Báo cáo</CardTitle></CardHeader>
        <CardContent>
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt class="text-muted-foreground">Sản phẩm đã kiểm tra</dt><dd>{{ report.products }}</dd>
            <dt class="text-muted-foreground">Đã duyệt</dt><dd>{{ report.approved }}</dd>
            <dt class="text-muted-foreground">Đã đăng</dt><dd>{{ report.published }}</dd>
            <dt class="text-muted-foreground">Thất bại</dt><dd>{{ report.failed }}</dd>
            <dt class="text-muted-foreground">Chưa sẵn sàng</dt><dd>{{ (report.blocked as unknown[] | undefined)?.length ?? 0 }}</dd>
          </dl>
        </CardContent>
      </Card>
      <Card v-else-if="report && Object.keys(report).length">
        <CardHeader><CardTitle as="h2" class="text-base">Báo cáo</CardTitle></CardHeader>
        <CardContent>
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt class="text-muted-foreground">Thay đổi tìm thấy</dt><dd>{{ report.changes_found }}</dd>
            <dt class="text-muted-foreground">Đã duyệt</dt><dd>{{ report.selected }}</dd>
            <dt class="text-muted-foreground">Đã áp dụng qua API</dt><dd>{{ show(report.applied) }}</dd>
            <dt class="text-muted-foreground">Việc làm thủ công</dt><dd>{{ report.manual_tasks }}</dd>
            <dt class="text-muted-foreground">Không được chọn</dt><dd>{{ show(report.not_selected) }}</dd>
            <dt class="text-muted-foreground">Không thực hiện được</dt><dd>{{ show(report.blocked) }}</dd>
            <dt class="text-muted-foreground">Chưa khớp sau khi áp dụng</dt><dd>{{ report.verification_mismatches }}</dd>
          </dl>
        </CardContent>
      </Card>

      <!-- Events -->
      <section aria-labelledby="events-heading" class="space-y-2">
        <h2 id="events-heading" class="text-lg font-semibold">Hoạt động</h2>
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
