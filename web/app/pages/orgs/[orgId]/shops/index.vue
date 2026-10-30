<script setup lang="ts">
import { connectErrors, daysUntil, statusLabels, statusVariant, type ConnectionStatus, type Shop } from '~/utils/shops'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Shops' })

const api = useApi()
const route = useRoute()
const router = useRouter()
const { orgId, can } = useOrg()

const shops = ref<Shop[]>([])
const loading = ref(true)
const error = ref('')
const connecting = ref(false)
const initialStatus = typeof route.query.status === 'string' && route.query.status in statusLabels ? route.query.status as ConnectionStatus : ''
const filter = reactive<{ status: ConnectionStatus | ''; q: string }>({ status: initialStatus, q: '' })

// Errors from the Shopee redirect arrive as ?connect_error=<code>.
const connectError = computed(() => {
  const code = route.query.connect_error
  return typeof code === 'string' ? (connectErrors[code] ?? connectErrors.internal_error) : ''
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    shops.value = unwrap(await api.GET('/orgs/{orgID}/shops', {
      params: {
        path: { orgID: orgId.value },
        query: { status: filter.status || undefined, q: filter.q.trim() || undefined },
      },
    })).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

let timer: ReturnType<typeof setTimeout> | undefined
watch(() => [filter.status, filter.q], () => {
  clearTimeout(timer)
  timer = setTimeout(load, 250)
})

// With several enabled Shopee apps (e.g. sandbox and production), the user
// chooses one; with one, it is used directly.
const apps = ref<{ id: string; name: string; environment: string }[]>([])
const appId = ref('')
onMounted(async () => {
  if (!can('shops.connect')) return
  try {
    apps.value = unwrap(await api.GET('/orgs/{orgID}/shopee-apps', { params: { path: { orgID: orgId.value } } })).data
    appId.value = apps.value[0]?.id ?? ''
  } catch { /* the connect call reports problems */ }
})

async function connect() {
  connecting.value = true
  error.value = ''
  try {
    const body = apps.value.length > 1 && appId.value ? { shopee_app_id: appId.value } : {}
    const res = unwrap(await api.POST('/orgs/{orgID}/shops/connect', { params: { path: { orgID: orgId.value } }, body }))
    // Leave the dashboard: the seller signs in on Shopee and is sent back.
    window.location.assign(res.data.authorization_url)
  } catch (e) {
    error.value = toApiError(e).message
    connecting.value = false
  }
}

function dismissConnectError() {
  const query = { ...route.query }
  delete query.connect_error
  void router.replace({ query })
}

const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
const date = (iso: string | null) => (iso ? fmt.format(new Date(iso)) : '—')
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Shops" description="Shopee shops connected to this organization.">
      <template #actions>
        <div v-if="can('shops.connect') && apps.length > 1" class="flex items-center gap-2">
          <Label for="connect-app" class="text-sm">Shopee app</Label>
          <select id="connect-app" v-model="appId" class="h-9 rounded-md border bg-background px-3 text-sm">
            <option v-for="a in apps" :key="a.id" :value="a.id">{{ a.name }} ({{ a.environment }})</option>
          </select>
        </div>
        <Button v-if="can('shops.connect')" :disabled="connecting" @click="connect">
          {{ connecting ? 'Opening Shopee…' : 'Connect shop' }}
        </Button>
      </template>
    </PageHeader>

    <Alert v-if="connectError" variant="destructive" role="alert">
      <AlertTitle>Shop not connected</AlertTitle>
      <AlertDescription class="flex flex-wrap items-center gap-3">
        {{ connectError }}
        <Button variant="outline" size="sm" @click="dismissConnectError">Dismiss</Button>
      </AlertDescription>
    </Alert>
    <FormError :message="error" />

    <div class="flex flex-col gap-3 sm:flex-row">
      <div class="flex-1">
        <Label for="shop-search" class="sr-only">Search shops</Label>
        <Input id="shop-search" v-model="filter.q" type="search" placeholder="Search by name or Shopee shop ID" />
      </div>
      <div class="sm:w-56">
        <Label for="shop-status" class="sr-only">Status</Label>
        <select id="shop-status" v-model="filter.status" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">All statuses</option>
          <option v-for="(label, key) in statusLabels" :key="key" :value="key">{{ label }}</option>
        </select>
      </div>
    </div>

    <p v-if="loading && !shops.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <EmptyState
      v-else-if="!shops.length && !filter.q && !filter.status"
      title="No shops yet"
      description="Connect a Shopee shop to sync its information and automate setup. You will sign in on Shopee to authorize access."
    >
      <Button v-if="can('shops.connect')" :disabled="connecting" @click="connect">Connect shop</Button>
    </EmptyState>
    <p v-else-if="!shops.length" class="text-sm text-muted-foreground">No shops match the filters.</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Shop</TableHead>
            <TableHead>Market</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Authorization ends</TableHead>
            <TableHead>Last synced</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="s in shops" :key="s.id">
            <TableCell>
              <NuxtLink :to="`/orgs/${orgId}/shops/${s.id}`" class="font-medium underline-offset-4 hover:underline">
                {{ s.shop_name ?? `Shop ${s.shopee_shop_id}` }}
              </NuxtLink>
              <div class="text-xs text-muted-foreground">
                ID {{ s.shopee_shop_id }}<span v-if="s.environment === 'sandbox'"> · Sandbox</span>
              </div>
            </TableCell>
            <TableCell>{{ s.region ?? '—' }}</TableCell>
            <TableCell><Badge :variant="statusVariant(s.connection_status)">{{ statusLabels[s.connection_status] }}</Badge></TableCell>
            <TableCell>
              {{ date(s.authorization_expires_at) }}
              <span
                v-if="s.connection_status === 'ACTIVE' && (daysUntil(s.authorization_expires_at) ?? 99) <= 7"
                class="block text-xs text-destructive"
              >Ends in {{ Math.max(0, daysUntil(s.authorization_expires_at) ?? 0) }} days, reconnect soon</span>
            </TableCell>
            <TableCell>
              {{ date(s.last_synced_at) }}
              <span v-if="s.last_sync_error" class="block text-xs text-destructive">Last sync failed ({{ s.last_sync_error }})</span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
