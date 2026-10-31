<script setup lang="ts">
import { Menu, X } from '@lucide/vue'

const route = useRoute()
const session = useSessionStore()
const open = ref(false)

// Pages outside an organization (help, account) keep the menu of the
// organization used last, so users can always find their way back.
const lastOrg = useState<string>('last-org', () => '')
const orgId = computed(() => {
  if (typeof route.params.orgId === 'string') return route.params.orgId
  return lastOrg.value || session.memberships[0]?.organization.id || ''
})
watch(() => route.params.orgId, (id) => { if (typeof id === 'string') lastOrg.value = id }, { immediate: true })
const current = computed(() => (orgId.value ? session.membership(orgId.value) : undefined))

const nav = computed(() => {
  if (!orgId.value) return []
  const base = `/orgs/${orgId.value}`
  return [
    { label: 'Tổng quan', to: base },
    { label: 'Tổ chức', to: '/orgs' },
    { label: 'Shop', to: `${base}/shops` },
    { label: 'Mẫu shop', to: `${base}/templates` },
    { label: 'Sản phẩm', to: `${base}/products` },
    { label: 'Nhập sản phẩm', to: `${base}/imports` },
    { label: 'Quy trình tự động', to: `${base}/workflows` },
    { label: 'Nhật ký thực thi', to: `${base}/logs` },
    { label: 'Tiện ích trình duyệt', to: `${base}/extension` },
    { label: 'Thành viên', to: `${base}/members` },
    { label: 'Cài đặt', to: `${base}/settings` },
  ]
})

function isActive(to: string) {
  if (to === '/orgs' || to === `/orgs/${orgId.value}`) return route.path === to
  return route.path === to || route.path.startsWith(`${to}/`)
}

function switchOrg(id: unknown) {
  if (typeof id === 'string' && id) void navigateTo(`/orgs/${id}`)
}

async function logout() {
  await session.logout()
  await navigateTo('/login')
}

watch(() => route.fullPath, () => (open.value = false))

const api = useApi()
const resend = reactive({ busy: false, message: '' })
async function resendVerification() {
  resend.busy = true
  try {
    unwrap(await api.POST('/auth/verify-email/resend'))
    resend.message = 'Đã gửi liên kết mới. Bạn hãy kiểm tra hộp thư.'
  } catch (e) {
    resend.message = toApiError(e).message
  } finally {
    resend.busy = false
  }
}
</script>

<template>
  <div class="min-h-screen bg-muted/30 md:flex">
    <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-background focus:px-3 focus:py-2">
      Chuyển đến nội dung chính
    </a>

    <header class="flex items-center justify-between border-b bg-background px-4 py-3 md:hidden">
      <span class="text-sm font-semibold text-primary">Shopee Shop Automation</span>
      <Button variant="ghost" size="icon" :aria-expanded="open" aria-controls="sidebar" @click="open = !open">
        <X v-if="open" class="size-5" aria-hidden="true" />
        <Menu v-else class="size-5" aria-hidden="true" />
        <span class="sr-only">{{ open ? 'Đóng menu' : 'Mở menu' }}</span>
      </Button>
    </header>

    <aside
      id="sidebar"
      :class="[open ? 'block' : 'hidden', 'w-full border-b bg-background md:sticky md:top-0 md:block md:h-screen md:w-64 md:shrink-0 md:border-b-0 md:border-r']"
    >
      <div class="flex h-full flex-col gap-4 p-4">
        <p class="hidden text-sm font-semibold tracking-tight text-primary md:block">Shopee Shop Automation</p>

        <div v-if="session.memberships.length && orgId" class="space-y-1">
          <Label for="org-switcher" class="text-xs text-muted-foreground">Tổ chức</Label>
          <Select :model-value="orgId" @update:model-value="switchOrg">
            <SelectTrigger id="org-switcher" class="w-full">
              <SelectValue placeholder="Chọn tổ chức" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="m in session.memberships" :key="m.organization.id" :value="m.organization.id">
                {{ m.organization.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <nav v-if="nav.length" aria-label="Điều hướng chính" class="flex-1">
          <ul class="space-y-0.5">
            <li v-for="item in nav" :key="item.to">
              <NuxtLink
                :to="item.to"
                :aria-current="isActive(item.to) ? 'page' : undefined"
                :class="[
                  'block rounded-md px-3 py-2 text-sm transition-colors',
                  isActive(item.to) ? 'bg-primary/10 font-medium text-primary' : 'text-foreground/80 hover:bg-muted',
                ]"
              >
                {{ item.label }}
              </NuxtLink>
            </li>
          </ul>
        </nav>
        <div v-else class="flex-1" />

        <div class="border-t pt-4 text-sm">
          <p class="truncate font-medium">{{ session.user?.display_name }}</p>
          <p class="truncate text-xs text-muted-foreground">{{ session.user?.email }}</p>
          <p v-if="current" class="mt-1 text-xs text-muted-foreground">Vai trò: {{ roleLabels[current.role] }}</p>
          <NuxtLink to="/help" class="mt-2 block text-xs text-primary hover:underline">Hướng dẫn sử dụng</NuxtLink>
          <NuxtLink to="/account" class="mt-1 block text-xs text-primary hover:underline">
            Bảo mật tài khoản{{ session.user?.mfa_enabled ? '' : ' (chưa bật xác minh hai bước)' }}
          </NuxtLink>
          <NuxtLink v-if="session.user?.is_platform_admin" to="/admin/shopee-apps" class="mt-2 block text-xs text-primary hover:underline">
            Quản trị nền tảng: Ứng dụng Shopee
          </NuxtLink>
          <NuxtLink v-if="session.user?.is_platform_admin" to="/admin/browser-recipes" class="mt-1 block text-xs text-primary hover:underline">
            Quản trị nền tảng: Kịch bản thao tác
          </NuxtLink>
          <NuxtLink v-if="session.user?.is_platform_admin" to="/admin/capabilities" class="mt-1 block text-xs text-primary hover:underline">
            Quản trị nền tảng: Chức năng
          </NuxtLink>
          <Button variant="outline" size="sm" class="mt-3 w-full" @click="logout">Đăng xuất</Button>
        </div>
      </div>
    </aside>

    <main id="main" class="min-w-0 flex-1 p-4 md:p-8">
      <div v-if="orgId" class="mb-4 flex justify-end">
        <NotificationBell :org-id="orgId" />
      </div>
      <div
        v-if="session.user && !session.user.email_verified"
        role="status"
        class="mb-6 rounded-md border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900"
      >
        Bạn hãy xác minh địa chỉ email. Mở hộp thư và bấm vào liên kết chúng tôi đã gửi.
        <Button variant="link" size="sm" class="h-auto p-0 pl-1 text-amber-900 underline" :disabled="resend.busy" @click="resendVerification">
          Gửi lại liên kết
        </Button>
        <span v-if="resend.message" class="block">{{ resend.message }}</span>
      </div>
      <slot />
    </main>
  </div>
</template>
