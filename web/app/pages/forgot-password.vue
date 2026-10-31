<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Đặt lại mật khẩu' })

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
      <CardTitle as="h1">Đặt lại mật khẩu</CardTitle>
      <CardDescription v-if="!done">Chúng tôi sẽ gửi email kèm liên kết để bạn đặt mật khẩu mới.</CardDescription>
    </CardHeader>
    <CardContent>
      <p v-if="done" role="status" class="text-sm">Nếu <strong>{{ email }}</strong> có tài khoản, chúng tôi đã gửi liên kết đặt lại mật khẩu. Liên kết có hiệu lực trong 1 giờ.</p>
      <form v-else class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <FormField id="email" label="Email">
          <Input id="email" v-model="email" type="email" autocomplete="email" required />
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">Gửi liên kết đặt lại</Button>
      </form>
    </CardContent>
    <CardFooter class="text-sm">
      <NuxtLink to="/login" class="text-muted-foreground underline-offset-4 hover:underline">Quay lại đăng nhập</NuxtLink>
    </CardFooter>
  </Card>
</template>
