<script setup lang="ts">
useHead({ title: 'Account security' })

const api = useApi()
const route = useRoute()
const session = useSessionStore()
const requiredBy = computed(() => (typeof route.query.required === 'string' ? route.query.required : ''))
const status = ref<{ enabled: boolean; recovery_codes_left: number; available: boolean } | null>(null)
const setup = ref<{ secret: string; otpauth_uri: string } | null>(null)
const codes = ref<string[]>([])
const code = ref('')
const password = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)

async function load() {
  try {
    status.value = unwrap(await api.GET('/auth/mfa')).data
  } catch (e) {
    error.value = toApiError(e).message
  }
}
onMounted(load)

async function act(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await fn()
  } catch (e) {
    const err = toApiError(e)
    error.value = Object.values(err.fields).join(' ') || err.message
  } finally {
    busy.value = false
  }
}

const start = () => act(async () => {
  setup.value = unwrap(await api.POST('/auth/mfa/setup')).data
  codes.value = []
})
const confirm = () => act(async () => {
  codes.value = unwrap(await api.POST('/auth/mfa/confirm', { body: { code: code.value } })).data.recovery_codes
  setup.value = null
  code.value = ''
  notice.value = 'Two-step verification is on. Save the recovery codes below.'
  await session.load(true)
  await load()
})
const regenerate = () => act(async () => {
  codes.value = unwrap(await api.POST('/auth/mfa/recovery-codes', { body: { code: code.value } })).data.recovery_codes
  code.value = ''
  notice.value = 'New recovery codes. The old ones no longer work.'
  await load()
})
const disable = () => act(async () => {
  unwrap(await api.POST('/auth/mfa/disable', { body: { password: password.value, code: code.value } }))
  code.value = ''
  password.value = ''
  codes.value = []
  notice.value = 'Two-step verification is off.'
  await session.load(true)
  await load()
})
const grouped = computed(() => setup.value?.secret.match(/.{1,4}/g)?.join(' ') ?? '')
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-6">
    <PageHeader title="Account security" description="Two-step verification protects your account with a code from an authenticator app." />
    <Alert v-if="requiredBy && !status?.enabled" role="alert">
      <AlertDescription>{{ requiredBy }} requires two-step verification for owners and admins. Turn it on to continue.</AlertDescription>
    </Alert>
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <p v-if="!status" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <Alert v-else-if="!status.available" variant="destructive">
      <AlertDescription>Two-step verification is not available on this server.</AlertDescription>
    </Alert>

    <Card v-else-if="!status.enabled">
      <CardHeader>
        <CardTitle as="h2" class="text-base">Two-step verification is off</CardTitle>
        <CardDescription>Organizations can require it for owners and admins.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <Button v-if="!setup" :disabled="busy" @click="start">Turn on</Button>
        <form v-else class="space-y-4" @submit.prevent="confirm">
          <ol class="list-decimal space-y-2 pl-5 text-sm">
            <li>
              In your authenticator app (Google Authenticator, 1Password, Authy…), add an account with this key:
              <code class="mt-1 block break-all rounded bg-muted px-2 py-1 font-mono text-base tracking-wider">{{ grouped }}</code>
              <a :href="setup.otpauth_uri" class="text-xs underline">Open in an authenticator app on this device</a>
            </li>
            <li>Enter the 6-digit code it shows.</li>
          </ol>
          <FormField id="mfa-code" label="Code">
            <Input id="mfa-code" v-model="code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" required />
          </FormField>
          <Button type="submit" :disabled="busy || code.length < 6">Confirm</Button>
        </form>
      </CardContent>
    </Card>

    <Card v-else>
      <CardHeader>
        <CardTitle as="h2" class="text-base">Two-step verification is on</CardTitle>
        <CardDescription>{{ status.recovery_codes_left }} recovery codes left.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <FormField id="mfa-current" label="Current code from your app (or a recovery code to turn it off)">
          <Input id="mfa-current" v-model="code" autocomplete="one-time-code" maxlength="20" />
        </FormField>
        <Button variant="outline" :disabled="busy || !code" @click="regenerate">Get new recovery codes</Button>
        <form class="space-y-3 border-t pt-4" @submit.prevent="disable">
          <FormField id="mfa-password" label="Password (to turn it off)">
            <Input id="mfa-password" v-model="password" type="password" autocomplete="current-password" />
          </FormField>
          <Button type="submit" variant="destructive" :disabled="busy || !code || !password">Turn off</Button>
        </form>
      </CardContent>
    </Card>

    <Card v-if="codes.length">
      <CardHeader>
        <CardTitle as="h2" class="text-base">Recovery codes</CardTitle>
        <CardDescription>Each code works once if you lose your phone. They are shown only now: store them somewhere safe.</CardDescription>
      </CardHeader>
      <CardContent>
        <ul class="grid grid-cols-2 gap-2 font-mono text-sm">
          <li v-for="c in codes" :key="c">{{ c }}</li>
        </ul>
      </CardContent>
    </Card>
  </div>
</template>
