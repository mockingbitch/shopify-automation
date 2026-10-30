<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Reset password' })

const email = ref('')
const done = ref(false)
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    unwrap(await useApi().POST('/auth/password/forgot', { body: { email: email.value } }))
    done.value = true
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
      <CardTitle as="h1">Reset password</CardTitle>
      <CardDescription v-if="!done">We will email you a link to choose a new password.</CardDescription>
    </CardHeader>
    <CardContent>
      <p v-if="done" role="status" class="text-sm">If an account exists for <strong>{{ email }}</strong>, a reset link is on its way. It is valid for 1 hour.</p>
      <form v-else class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <FormField id="email" label="Email">
          <Input id="email" v-model="email" type="email" autocomplete="email" required />
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">Send reset link</Button>
      </form>
    </CardContent>
    <CardFooter class="text-sm">
      <NuxtLink to="/login" class="text-muted-foreground underline-offset-4 hover:underline">Back to sign in</NuxtLink>
    </CardFooter>
  </Card>
</template>
