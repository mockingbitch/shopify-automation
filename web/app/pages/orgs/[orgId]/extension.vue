<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Browser Extension' })

type Device = components['schemas']['ExtensionDevice']

const api = useApi()
const session = useSessionStore()
const { orgId, can } = useOrg()
const devices = ref<Device[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const code = ref<{ code: string; expires_at: string } | null>(null)
const now = ref(Date.now())
const timer = setInterval(() => { now.value = Date.now() }, 1000)
onBeforeUnmount(() => clearInterval(timer))

const remaining = computed(() => {
  if (!code.value) return 0
  return Math.max(0, Math.floor((new Date(code.value.expires_at).getTime() - now.value) / 1000))
})

async function load() {
  loading.value = true
  try {
    devices.value = unwrap(await api.GET('/orgs/{orgID}/extension/devices', { params: { path: { orgID: orgId.value } } })).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function createCode() {
  error.value = ''
  try {
    code.value = unwrap(await api.POST('/orgs/{orgID}/extension/pairing-codes', { params: { path: { orgID: orgId.value } } })).data
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function revoke(d: Device) {
  error.value = ''
  try {
    unwrap(await api.DELETE('/orgs/{orgID}/extension/devices/{deviceID}', { params: { path: { orgID: orgId.value, deviceID: d.id } } }))
    notice.value = `“${d.name}” was unpaired.`
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}

const canRevoke = (d: Device) => d.user_id === session.user?.id || can('members.manage')
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <PageHeader title="Browser Extension"
                description="Some shop settings have no Shopee Open API (banners, decoration, new addresses). The extension does them in your own Seller Centre tab while you watch." />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <Card>
      <CardHeader>
        <CardTitle as="h2" class="text-base">How it works</CardTitle>
      </CardHeader>
      <CardContent class="space-y-2 text-sm">
        <ol class="list-decimal space-y-1 pl-5">
          <li>Install the extension in Chrome, Edge or another Chromium browser, then pair it with a code from this page.</li>
          <li>When a workflow needs a change without an Open API, a task appears in the extension popup.</li>
          <li>Open Seller Centre, logged in to the right shop, and press <strong>Run</strong>. The extension checks the shop ID first and stops if it is a different shop.</li>
          <li>If Seller Centre asks for a login, verification code or CAPTCHA, the extension pauses. Finish it yourself, then press <strong>Continue</strong>.</li>
        </ol>
        <p class="text-muted-foreground">
          The extension never reads your Shopee password or cookies, and only acts on pages listed in its verified steps.
          Operations without verified steps for your market stay manual tasks.
        </p>
      </CardContent>
    </Card>

    <section v-if="can('workflows.run')" class="space-y-3 rounded-lg border bg-background p-4" aria-labelledby="pair-heading">
      <h2 id="pair-heading" class="text-base font-semibold">Pair a browser</h2>
      <div v-if="code && remaining > 0" class="space-y-1">
        <p class="text-sm">Type this code in the extension popup:</p>
        <p class="font-mono text-3xl tracking-widest" aria-live="polite">{{ code.code }}</p>
        <p class="text-xs text-muted-foreground">Valid for {{ Math.floor(remaining / 60) }}:{{ String(remaining % 60).padStart(2, '0') }} and only once.</p>
      </div>
      <p v-else-if="code" class="text-sm text-muted-foreground">The code expired. Create a new one.</p>
      <div class="flex gap-2">
        <Button @click="createCode">{{ code ? 'New code' : 'Create pairing code' }}</Button>
        <Button v-if="code" variant="outline" @click="load">I paired it</Button>
      </div>
    </section>

    <section class="space-y-2" aria-labelledby="devices-heading">
      <h2 id="devices-heading" class="text-lg font-semibold">Paired browsers</h2>
      <p v-if="loading && !devices.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
      <EmptyState v-else-if="!devices.length" title="No browser paired" description="Pair a browser to let the extension do the tasks without an Open API." />
      <div v-else class="overflow-x-auto rounded-lg border bg-background">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Browser</TableHead><TableHead>User</TableHead><TableHead>Version</TableHead><TableHead>Last used</TableHead>
              <TableHead><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="d in devices" :key="d.id">
              <TableCell class="font-medium">{{ d.name }}</TableCell>
              <TableCell>{{ d.user_email }}</TableCell>
              <TableCell>{{ d.extension_version || '—' }}</TableCell>
              <TableCell>{{ d.last_used_at ? fmt.format(new Date(d.last_used_at)) : '—' }}</TableCell>
              <TableCell class="text-right">
                <ConfirmDialog v-if="canRevoke(d)" :title="`Unpair “${d.name}”?`" description="The extension in that browser stops working until it is paired again."
                               confirm-label="Unpair" @confirm="revoke(d)">
                  <template #trigger><Button variant="outline" size="sm">Unpair</Button></template>
                </ConfirmDialog>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </section>
  </div>
</template>
