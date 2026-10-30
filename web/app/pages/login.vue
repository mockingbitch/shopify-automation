<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Sign in' })

const route = useRoute()
const session = useSessionStore()
const form = reactive({ email: '', password: '' })
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await session.login(form.email, form.password)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') && !route.query.next.startsWith('//') ? route.query.next : '/'
    // The Shopee callback is served by the API, not the dashboard router.
    await navigateTo(next, { external: next.startsWith('/api/v1/oauth/shopee/callback?') })
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle as="h1">Sign in</CardTitle>
      <CardDescription>Use your work email and password.</CardDescription>
    </CardHeader>
    <CardContent>
      <form class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <FormField id="email" label="Email">
          <Input id="email" v-model="form.email" type="email" autocomplete="email" required />
        </FormField>
        <FormField id="password" label="Password">
          <Input id="password" v-model="form.password" type="password" autocomplete="current-password" required />
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">{{ busy ? 'Signing in…' : 'Sign in' }}</Button>
      </form>
    </CardContent>
    <CardFooter class="flex justify-between text-sm">
      <NuxtLink to="/forgot-password" class="text-muted-foreground underline-offset-4 hover:underline">Forgot password?</NuxtLink>
      <NuxtLink to="/register" class="text-primary underline-offset-4 hover:underline">Create account</NuxtLink>
    </CardFooter>
  </Card>
</template>
