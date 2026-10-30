<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Create account' })

const form = reactive({ display_name: '', email: '', password: '' })
const fields = ref<Record<string, string>>({})
const error = ref('')
const done = ref(false)
const busy = ref(false)

async function submit() {
  error.value = ''
  fields.value = {}
  busy.value = true
  try {
    unwrap(await useApi().POST('/auth/register', { body: { ...form } }))
    done.value = true
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    if (!Object.keys(err.fields).length) error.value = err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle as="h1">Create account</CardTitle>
      <CardDescription v-if="!done">You can create or join an organization after signing up.</CardDescription>
    </CardHeader>
    <CardContent>
      <div v-if="done" role="status" class="space-y-2 text-sm">
        <p>Check <strong>{{ form.email }}</strong> for a link to verify your email address.</p>
        <NuxtLink to="/login" class="text-primary underline-offset-4 hover:underline">Go to sign in</NuxtLink>
      </div>
      <form v-else class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <FormField id="display_name" label="Your name" :error="fields.display_name">
          <template #default="{ describedBy, invalid }">
            <Input id="display_name" v-model="form.display_name" autocomplete="name" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <FormField id="email" label="Email" :error="fields.email">
          <template #default="{ describedBy, invalid }">
            <Input id="email" v-model="form.email" type="email" autocomplete="email" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <FormField id="password" label="Password" :error="fields.password" hint="At least 12 characters. A short sentence works well.">
          <template #default="{ describedBy, invalid }">
            <Input id="password" v-model="form.password" type="password" autocomplete="new-password" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">{{ busy ? 'Creating…' : 'Create account' }}</Button>
      </form>
    </CardContent>
    <CardFooter class="text-sm">
      <NuxtLink to="/login" class="text-muted-foreground underline-offset-4 hover:underline">Already have an account? Sign in</NuxtLink>
    </CardFooter>
  </Card>
</template>
