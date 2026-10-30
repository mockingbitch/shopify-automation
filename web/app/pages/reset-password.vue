<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Choose a new password' })

const route = useRoute()
const password = ref('')
const fields = ref<Record<string, string>>({})
const error = ref('')
const done = ref(false)
const busy = ref(false)

async function submit() {
  error.value = ''
  fields.value = {}
  busy.value = true
  try {
    const token = typeof route.query.token === 'string' ? route.query.token : ''
    unwrap(await useApi().POST('/auth/password/reset', { body: { token, new_password: password.value } }))
    useSessionStore().reset()
    done.value = true
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    if (!err.fields.new_password) error.value = err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle as="h1">Choose a new password</CardTitle></CardHeader>
    <CardContent>
      <div v-if="done" role="status" class="space-y-2 text-sm">
        <p>Your password was changed and you were signed out everywhere.</p>
        <NuxtLink to="/login" class="text-primary underline-offset-4 hover:underline">Sign in</NuxtLink>
      </div>
      <form v-else class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <FormField id="new_password" label="New password" :error="fields.new_password" hint="At least 12 characters.">
          <template #default="{ describedBy, invalid }">
            <Input id="new_password" v-model="password" type="password" autocomplete="new-password" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">Change password</Button>
      </form>
    </CardContent>
  </Card>
</template>
