<script setup lang="ts">
import type { components } from '~/types/api'

useHead({ title: 'Shopee apps' })

type App = components['schemas']['ShopeeApp']

const api = useApi()
const session = useSessionStore()
const apps = ref<App[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const fields = ref<Record<string, string>>({})
const busy = ref(false)

const form = reactive({
  name: '', environment: 'sandbox' as 'sandbox' | 'production', partner_id: '', partner_key: '', app_type: 'ERP System',
  api_host: '', auth_host: '', redirect_uri: '', push_callback_url: '',
})
const rotate = reactive<Record<string, string>>({})

async function load() {
  loading.value = true
  error.value = ''
  try {
    apps.value = unwrap(await api.GET('/admin/shopee-apps')).data
  } catch (e) {
    error.value = toApiError(e).status === 404 ? 'This page is only available to platform administrators.' : toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

const opt = (v: string) => (v.trim() ? v.trim() : undefined)

async function create() {
  busy.value = true
  fields.value = {}
  error.value = ''
  notice.value = ''
  try {
    const res = unwrap(await api.POST('/admin/shopee-apps', {
      body: {
        name: form.name, environment: form.environment, partner_id: form.partner_id.trim(), partner_key: form.partner_key,
        app_type: form.app_type, api_host: opt(form.api_host), auth_host: opt(form.auth_host),
        redirect_uri: opt(form.redirect_uri), push_callback_url: opt(form.push_callback_url),
      },
    }))
    notice.value = `${res.data.name} was added. Configure ${res.data.redirect_uri} as the redirect URL domain and ${res.data.push_callback_url ?? 'the push URL'} as the push callback in the Shopee Console.`
    form.partner_key = ''
    form.name = ''
    form.partner_id = ''
    await load()
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    if (!Object.keys(err.fields).length) error.value = err.message
  } finally {
    busy.value = false
  }
}

async function update(a: App, body: components['schemas']['UpdateShopeeAppRequest'], message: string) {
  error.value = ''
  notice.value = ''
  try {
    unwrap(await api.PATCH('/admin/shopee-apps/{appID}', { params: { path: { appID: a.id } }, body }))
    notice.value = message
    await load()
  } catch (e) {
    const err = toApiError(e)
    error.value = Object.values(err.fields)[0] ?? err.message
  }
}

function rotateKey(a: App) {
  const key = rotate[a.id] ?? ''
  void update(a, { partner_key: key }, `Partner key of ${a.name} was replaced.`).then(() => { rotate[a.id] = '' })
}
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-8">
    <PageHeader title="Shopee partner apps" description="Partner apps registered in the Shopee Open Platform Console. Partner keys are stored encrypted and never shown again." />
    <div aria-live="polite">
      <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
    </div>
    <FormError :message="error" />

    <section v-if="session.user?.is_platform_admin" aria-labelledby="apps-heading" class="space-y-3">
      <h2 id="apps-heading" class="text-lg font-semibold">Apps</h2>
      <p v-if="loading" role="status" class="text-sm text-muted-foreground">Loading…</p>
      <p v-else-if="!apps.length" class="text-sm text-muted-foreground">No apps yet. Add the sandbox app first.</p>
      <Card v-for="a in apps" :key="a.id">
        <CardHeader>
          <CardTitle as="h3" class="flex items-center gap-2 text-base">
            {{ a.name }}
            <Badge variant="outline">{{ a.environment }}</Badge>
            <Badge :variant="a.enabled ? 'default' : 'secondary'">{{ a.enabled ? 'Enabled' : 'Disabled' }}</Badge>
          </CardTitle>
        </CardHeader>
        <CardContent class="space-y-4">
          <dl class="grid grid-cols-1 gap-x-4 gap-y-1 text-sm sm:grid-cols-[10rem_1fr]">
            <dt class="text-muted-foreground">Partner ID</dt><dd>{{ a.partner_id }}</dd>
            <dt class="text-muted-foreground">App type</dt><dd>{{ a.app_type }}</dd>
            <dt class="text-muted-foreground">API host</dt><dd class="break-all">{{ a.api_host }}</dd>
            <dt class="text-muted-foreground">Auth host</dt><dd class="break-all">{{ a.auth_host }}</dd>
            <dt class="text-muted-foreground">Redirect URL</dt><dd class="break-all">{{ a.redirect_uri }}</dd>
            <dt class="text-muted-foreground">Push callback</dt><dd class="break-all">{{ a.push_callback_url ?? 'Not set (pushes are rejected)' }}</dd>
            <dt class="text-muted-foreground">Partner key</dt><dd>{{ a.partner_key_set ? 'Stored (encrypted)' : 'Missing' }}</dd>
          </dl>
          <div class="flex flex-wrap items-end gap-2">
            <div class="min-w-64 flex-1">
              <Label :for="`rotate-${a.id}`" class="text-xs">Replace partner key</Label>
              <Input :id="`rotate-${a.id}`" v-model="rotate[a.id]" type="password" autocomplete="off" />
            </div>
            <Button variant="outline" :disabled="!rotate[a.id]" @click="rotateKey(a)">Replace key</Button>
            <Button variant="outline" @click="update(a, { enabled: !a.enabled }, `${a.name} was ${a.enabled ? 'disabled' : 'enabled'}.`)">
              {{ a.enabled ? 'Disable' : 'Enable' }}
            </Button>
          </div>
        </CardContent>
      </Card>
    </section>

    <section v-if="session.user?.is_platform_admin" aria-labelledby="new-heading" class="space-y-3">
      <h2 id="new-heading" class="text-lg font-semibold">Add an app</h2>
      <form class="grid gap-4 rounded-lg border bg-background p-4 sm:grid-cols-2" novalidate @submit.prevent="create">
        <FormField id="name" label="Name" :error="fields.name"><Input id="name" v-model="form.name" required /></FormField>
        <FormField id="environment" label="Environment" :error="fields.environment">
          <select id="environment" v-model="form.environment" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
            <option value="sandbox">Sandbox</option>
            <option value="production">Production</option>
          </select>
        </FormField>
        <FormField id="partner_id" label="Partner ID" :error="fields.partner_id"><Input id="partner_id" v-model="form.partner_id" inputmode="numeric" required /></FormField>
        <FormField id="partner_key" label="Partner key" :error="fields.partner_key"><Input id="partner_key" v-model="form.partner_key" type="password" autocomplete="off" required /></FormField>
        <FormField id="app_type" label="App type" :error="fields.app_type"><Input id="app_type" v-model="form.app_type" required /></FormField>
        <FormField id="api_host" label="API host (optional)" :error="fields.api_host"><Input id="api_host" v-model="form.api_host" placeholder="Default for the environment" /></FormField>
        <FormField id="auth_host" label="Authorization host (optional)" :error="fields.auth_host"><Input id="auth_host" v-model="form.auth_host" placeholder="Default for the environment" /></FormField>
        <FormField id="redirect_uri" label="Redirect URL (optional)" :error="fields.redirect_uri"><Input id="redirect_uri" v-model="form.redirect_uri" placeholder="…/api/v1/oauth/shopee/callback" /></FormField>
        <FormField id="push_callback_url" label="Push callback URL (optional)" :error="fields.push_callback_url" class="sm:col-span-2">
          <Input id="push_callback_url" v-model="form.push_callback_url" placeholder="…/api/v1/webhooks/shopee/<app id>" />
        </FormField>
        <div class="sm:col-span-2"><Button type="submit" :disabled="busy">{{ busy ? 'Saving…' : 'Add app' }}</Button></div>
      </form>
    </section>
  </div>
</template>
