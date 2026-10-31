<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Sign in' })

const route = useRoute()
const session = useSessionStore()
const form = reactive({ email: '', password: '', code: '' })
const error = ref('')
const busy = ref(false)
// Set when the account uses two-step verification.
const challenge = ref('')

async function done() {
  const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') && !route.query.next.startsWith('//') ? route.query.next : '/'
  // The Shopee callback is served by the API, not the dashboard router.
  await navigateTo(next, { external: next.startsWith('/api/v1/oauth/shopee/callback?') })
}

async function submit() {
  error.value = ''
  busy.value = true
  try {
    if (challenge.value) {
      await session.loginMFA(challenge.value, form.code)
      await done()
      return
    }
    const step = await session.login(form.email, form.password)
    if (step) {
      challenge.value = step.challenge
      await nextTick()
      document.getElementById('code')?.focus()
      return
    }
    await done()
  } catch (e) {
    const err = toApiError(e)
    if (challenge.value && err.status === 401) {
      // The challenge expired or ran out of attempts: start again.
      challenge.value = ''
      form.code = ''
    }
    error.value = err.code === 'validation_failed' ? 'The code is wrong or was already used.' : err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle as="h1">Sign in</CardTitle>
      <CardDescription>{{ challenge ? 'Enter the 6-digit code from your authenticator app, or a recovery code.' : 'Use your work email and password.' }}</CardDescription>
    </CardHeader>
    <CardContent>
      <form class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <template v-if="!challenge">
          <FormField id="email" label="Email">
            <Input id="email" v-model="form.email" type="email" autocomplete="email" required />
          </FormField>
          <FormField id="password" label="Password">
            <Input id="password" v-model="form.password" type="password" autocomplete="current-password" required />
          </FormField>
        </template>
        <FormField v-else id="code" label="Verification code">
          <Input id="code" v-model="form.code" inputmode="numeric" autocomplete="one-time-code" maxlength="20" required />
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">{{ busy ? 'Signing in…' : challenge ? 'Verify' : 'Sign in' }}</Button>
      </form>
    </CardContent>
    <CardFooter class="flex justify-between text-sm">
      <NuxtLink to="/forgot-password" class="text-muted-foreground underline-offset-4 hover:underline">Forgot password?</NuxtLink>
      <NuxtLink to="/register" class="text-primary underline-offset-4 hover:underline">Create account</NuxtLink>
    </CardFooter>
  </Card>
</template>
