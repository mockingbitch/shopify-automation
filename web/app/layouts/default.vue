<script setup lang="ts">
import { Menu, X } from '@lucide/vue'

const route = useRoute()
const session = useSessionStore()
const open = ref(false)

const orgId = computed(() => (typeof route.params.orgId === 'string' ? route.params.orgId : ''))
const current = computed(() => (orgId.value ? session.membership(orgId.value) : undefined))

const nav = computed(() => {
  if (!orgId.value) return []
  const base = `/orgs/${orgId.value}`
  return [
    { label: 'Overview', to: base },
    { label: 'Organizations', to: '/orgs' },
    { label: 'Shops', to: `${base}/shops` },
    { label: 'Shop Templates', to: `${base}/templates` },
    { label: 'Products', to: `${base}/products` },
    { label: 'Product Imports', to: `${base}/imports` },
    { label: 'Automation Workflows', to: `${base}/workflows` },
    { label: 'Execution Logs', to: `${base}/logs` },
    { label: 'Members', to: `${base}/members` },
    { label: 'Settings', to: `${base}/settings` },
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
    resend.message = 'A new link is on its way.'
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
      Skip to content
    </a>

    <header class="flex items-center justify-between border-b bg-background px-4 py-3 md:hidden">
      <span class="text-sm font-semibold text-primary">Shopee Shop Automation</span>
      <Button variant="ghost" size="icon" :aria-expanded="open" aria-controls="sidebar" @click="open = !open">
        <X v-if="open" class="size-5" aria-hidden="true" />
        <Menu v-else class="size-5" aria-hidden="true" />
        <span class="sr-only">{{ open ? 'Close menu' : 'Open menu' }}</span>
      </Button>
    </header>

    <aside
      id="sidebar"
      :class="[open ? 'block' : 'hidden', 'w-full border-b bg-background md:sticky md:top-0 md:block md:h-screen md:w-64 md:shrink-0 md:border-b-0 md:border-r']"
    >
      <div class="flex h-full flex-col gap-4 p-4">
        <p class="hidden text-sm font-semibold tracking-tight text-primary md:block">Shopee Shop Automation</p>

        <div v-if="session.memberships.length && orgId" class="space-y-1">
          <Label for="org-switcher" class="text-xs text-muted-foreground">Organization</Label>
          <Select :model-value="orgId" @update:model-value="switchOrg">
            <SelectTrigger id="org-switcher" class="w-full">
              <SelectValue placeholder="Choose organization" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="m in session.memberships" :key="m.organization.id" :value="m.organization.id">
                {{ m.organization.name }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <nav v-if="nav.length" aria-label="Main" class="flex-1">
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
          <p v-if="current" class="mt-1 text-xs text-muted-foreground">Role: {{ roleLabels[current.role] }}</p>
          <NuxtLink v-if="session.user?.is_platform_admin" to="/admin/shopee-apps" class="mt-2 block text-xs text-primary hover:underline">
            Platform admin: Shopee apps
          </NuxtLink>
          <Button variant="outline" size="sm" class="mt-3 w-full" @click="logout">Sign out</Button>
        </div>
      </div>
    </aside>

    <main id="main" class="min-w-0 flex-1 p-4 md:p-8">
      <div
        v-if="session.user && !session.user.email_verified"
        role="status"
        class="mb-6 rounded-md border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900"
      >
        Please verify your email address. Check your inbox for the link we sent.
        <Button variant="link" size="sm" class="h-auto p-0 pl-1 text-amber-900 underline" :disabled="resend.busy" @click="resendVerification">
          Send a new link
        </Button>
        <span v-if="resend.message" class="block">{{ resend.message }}</span>
      </div>
      <slot />
    </main>
  </div>
</template>
