<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Accept invitation' })

const route = useRoute()
const session = useSessionStore()
const error = ref('')
const busy = ref(false)

async function accept() {
  error.value = ''
  busy.value = true
  try {
    const token = typeof route.query.token === 'string' ? route.query.token : ''
    const res = unwrap(await useApi().POST('/invitations/accept', { body: { token } }))
    await session.load(true)
    await navigateTo(`/orgs/${res.data.organization.id}`)
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
      <CardTitle as="h1">Join organization</CardTitle>
      <CardDescription>You are signed in as {{ session.user?.email }}.</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4">
      <FormError :message="error" />
      <Button class="w-full" :disabled="busy" @click="accept">Accept invitation</Button>
    </CardContent>
  </Card>
</template>
