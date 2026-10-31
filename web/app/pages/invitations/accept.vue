<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Chấp nhận lời mời' })

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
      <CardTitle as="h1">Tham gia tổ chức</CardTitle>
      <CardDescription>Bạn đang đăng nhập bằng {{ session.user?.email }}.</CardDescription>
    </CardHeader>
    <CardContent class="space-y-4">
      <FormError :message="error" />
      <Button class="w-full" :disabled="busy" @click="accept">Chấp nhận lời mời</Button>
    </CardContent>
  </Card>
</template>
