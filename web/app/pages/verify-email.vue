<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Xác minh email' })

const route = useRoute()
const state = ref<'working' | 'done' | 'failed'>('working')
const message = ref('')

onMounted(async () => {
  const token = typeof route.query.token === 'string' ? route.query.token : ''
  try {
    unwrap(await useApi().POST('/auth/verify-email', { body: { token } }))
    state.value = 'done'
    await useSessionStore().load(true)
  } catch (e) {
    state.value = 'failed'
    message.value = toApiError(e).message
  }
})
</script>

<template>
  <Card>
    <CardHeader><CardTitle as="h1">Xác minh email</CardTitle></CardHeader>
    <CardContent role="status" class="space-y-3 text-sm">
      <p v-if="state === 'working'">Đang xác minh…</p>
      <p v-else-if="state === 'done'">Địa chỉ email của bạn đã được xác minh.</p>
      <p v-else class="text-destructive">{{ message }}</p>
      <NuxtLink v-if="state !== 'working'" to="/" class="text-primary underline-offset-4 hover:underline">Tiếp tục</NuxtLink>
    </CardContent>
  </Card>
</template>
