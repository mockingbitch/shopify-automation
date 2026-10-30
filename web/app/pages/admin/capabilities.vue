<script setup lang="ts">
import type { components, paths } from '~/types/api'

useHead({ title: 'Capabilities' })

type Cap = paths['/admin/capabilities']['get']['responses']['200']['content']['application/json']['data'][number]
type App = components['schemas']['ShopeeApp']

const api = useApi()
const caps = ref<Cap[]>([])
const apps = ref<App[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const fields = ref<Record<string, string>>({})
const markets = ['VN', 'SG', 'MY', 'TH', 'PH', 'ID', 'TW']
const form = reactive({ capability_key: '', shopee_app_id: '', region: '', enabled: 'off', mode: '', reason: '' })

async function load() {
  loading.value = true
  error.value = ''
  try {
    caps.value = unwrap(await api.GET('/admin/capabilities')).data
    apps.value = unwrap(await api.GET('/admin/shopee-apps')).data
    if (!form.capability_key) form.capability_key = caps.value[0]?.key ?? ''
  } catch (e) {
    error.value = toApiError(e).status === 404 ? 'This page is only available to platform administrators.' : toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function save() {
  fields.value = {}
  error.value = ''
  try {
    unwrap(await api.PUT('/admin/capability-overrides', {
      body: {
        capability_key: form.capability_key, shopee_app_id: form.shopee_app_id || null, region: form.region || null,
        enabled: form.enabled === '' ? null : form.enabled === 'on', mode: (form.mode || null) as 'API' | null, reason: form.reason,
      },
    }))
    notice.value = 'Override saved. It applies to the next capability check (immediately for new runs and claims).'
    form.reason = ''
    await load()
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    if (!Object.keys(err.fields).length) error.value = err.message
  }
}

async function remove(id: string) {
  error.value = ''
  try {
    unwrap(await api.DELETE('/admin/capability-overrides/{overrideID}', { params: { path: { overrideID: id } } }))
    notice.value = 'Override removed.'
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}
const appName = (id: string | null) => (id ? (apps.value.find((a) => a.id === id)?.name ?? 'Unknown app') : 'All apps')
const scope = (o: { shopee_app_id: string | null; region: string | null }) => `${appName(o.shopee_app_id)} · ${o.region ?? 'all markets'}`
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Capabilities" description="Every shop operation and how it runs. Overrides switch an operation off (kill switch) or change its mode for an app or market." />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <form class="grid gap-3 rounded-lg border bg-background p-4 md:grid-cols-3" novalidate @submit.prevent="save">
      <h2 class="text-base font-semibold md:col-span-3">Set an override</h2>
      <FormField id="ov-cap" label="Capability" :error="fields.capability_key">
        <select id="ov-cap" v-model="form.capability_key" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option v-for="c in caps" :key="c.key" :value="c.key">{{ c.title }} ({{ c.key }})</option>
        </select>
      </FormField>
      <FormField id="ov-app" label="Shopee app" :error="fields.shopee_app_id">
        <select id="ov-app" v-model="form.shopee_app_id" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">All apps</option>
          <option v-for="a in apps" :key="a.id" :value="a.id">{{ a.name }} ({{ a.environment }})</option>
        </select>
      </FormField>
      <FormField id="ov-region" label="Market" :error="fields.region">
        <select id="ov-region" v-model="form.region" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">All markets</option>
          <option v-for="m in markets" :key="m" :value="m">{{ m }}</option>
        </select>
      </FormField>
      <FormField id="ov-enabled" label="State" :error="fields.enabled">
        <select id="ov-enabled" v-model="form.enabled" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="off">Turned off (kill switch)</option>
          <option value="on">Turned on</option>
          <option value="">Unchanged</option>
        </select>
      </FormField>
      <FormField id="ov-mode" label="Mode" :error="fields.mode">
        <select id="ov-mode" v-model="form.mode" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
          <option value="">Unchanged</option>
          <option value="API">Shopee API</option>
          <option value="BROWSER">Browser extension</option>
          <option value="MANUAL">Manual task</option>
          <option value="UNSUPPORTED">Not supported</option>
        </select>
      </FormField>
      <FormField id="ov-reason" label="Reason (shown to users)" :error="fields.reason">
        <Input id="ov-reason" v-model="form.reason" maxlength="300" />
      </FormField>
      <div class="flex justify-end md:col-span-3"><Button type="submit" :disabled="!form.reason.trim()">Save override</Button></div>
    </form>

    <p v-if="loading && !caps.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Capability</TableHead><TableHead>Default</TableHead><TableHead>Verification</TableHead><TableHead>Overrides</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="c in caps" :key="c.key">
            <TableCell>
              <div class="font-medium">{{ c.title }}</div>
              <div class="font-mono text-xs text-muted-foreground">{{ c.key }}</div>
            </TableCell>
            <TableCell class="text-sm">
              {{ c.default_mode }}<template v-if="c.fallback_mode"> → {{ c.fallback_mode }}</template>
              <Badge v-if="!c.enabled" variant="secondary" class="ml-1">Off</Badge>
            </TableCell>
            <TableCell class="text-xs">{{ c.verification }}</TableCell>
            <TableCell class="space-y-1 text-sm">
              <div v-for="o in c.overrides" :key="o.id" class="flex flex-wrap items-center gap-2">
                <Badge :variant="o.enabled === false ? 'destructive' : 'outline'">{{ o.enabled === false ? 'Off' : o.enabled ? 'On' : o.mode }}</Badge>
                <span>{{ scope(o) }}: {{ o.reason }}</span>
                <ConfirmDialog title="Remove this override?" description="The capability goes back to its default for this scope." confirm-label="Remove" @confirm="remove(o.id)">
                  <template #trigger><Button variant="link" size="sm" class="h-auto p-0">Remove</Button></template>
                </ConfirmDialog>
              </div>
              <span v-if="!c.overrides.length" class="text-muted-foreground">—</span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
