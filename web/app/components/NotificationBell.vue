<script setup lang="ts">
import { Bell } from '@lucide/vue'
import type { components } from '~/types/api'

// Notifications of the current user in the current organization. Polls
// every minute; the panel is a disclosure (button + region), closed with Escape.
const props = defineProps<{ orgId: string }>()
type Note = components['schemas']['Notification']

const api = useApi()
const notes = ref<Note[]>([])
const unread = ref(0)
const open = ref(false)
const button = ref<HTMLElement>()

async function load() {
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/notifications', { params: { path: { orgID: props.orgId }, query: { limit: 20 } } }))
    notes.value = res.data
    unread.value = res.unread
  } catch { /* keep the last state */ }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(load, 60_000)
})
onBeforeUnmount(() => clearInterval(timer))
watch(() => props.orgId, () => load())

async function follow(n: Note) {
  open.value = false
  if (!n.read_at) {
    try {
      unwrap(await api.POST('/orgs/{orgID}/notifications/{notificationID}/read', { params: { path: { orgID: props.orgId, notificationID: n.id } } }))
    } catch { /* still navigate */ }
  }
  await load()
  if (n.link) await navigateTo(n.link)
}

async function readAll() {
  try {
    unwrap(await api.POST('/orgs/{orgID}/notifications/read-all', { params: { path: { orgID: props.orgId } } }))
  } catch { /* ignore */ }
  await load()
}

function close() {
  open.value = false
  button.value?.focus()
}
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'short', timeStyle: 'short' })
</script>

<template>
  <div class="relative" @keydown.esc="close">
    <Button ref="button" variant="outline" size="sm" :aria-expanded="open" aria-controls="notification-panel" @click="open = !open">
      <Bell class="size-4" aria-hidden="true" />
      <span>Thông báo</span>
      <span v-if="unread" class="rounded-full bg-primary px-1.5 text-xs text-primary-foreground">{{ unread }}<span class="sr-only"> chưa đọc</span></span>
    </Button>
    <section v-if="open" id="notification-panel" aria-label="Thông báo"
             class="absolute right-0 z-40 mt-2 w-80 max-w-[calc(100vw-2rem)] rounded-lg border bg-background p-2 shadow-lg">
      <div class="flex items-center justify-between px-2 py-1">
        <h2 class="text-sm font-semibold">Thông báo</h2>
        <Button v-if="unread" variant="link" size="sm" class="h-auto p-0" @click="readAll">Đánh dấu tất cả là đã đọc</Button>
      </div>
      <p v-if="!notes.length" class="px-2 py-4 text-sm text-muted-foreground">Không có việc gì cần bạn xử lý.</p>
      <ul v-else class="max-h-96 overflow-y-auto">
        <li v-for="n in notes" :key="n.id">
          <button type="button" class="w-full rounded-md px-2 py-2 text-left text-sm hover:bg-muted focus-visible:bg-muted" @click="follow(n)">
            <span class="flex items-center gap-2">
              <span v-if="!n.read_at" class="size-2 shrink-0 rounded-full bg-primary" aria-hidden="true" />
              <span :class="n.read_at ? 'text-muted-foreground' : 'font-medium'">{{ n.title }}</span>
              <span v-if="!n.read_at" class="sr-only">(chưa đọc)</span>
            </span>
            <span v-if="n.body" class="block text-xs text-muted-foreground">{{ n.body }}</span>
            <span class="block text-xs text-muted-foreground">{{ fmt.format(new Date(n.created_at)) }}</span>
          </button>
        </li>
      </ul>
    </section>
  </div>
</template>
