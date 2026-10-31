<script setup lang="ts">
definePageMeta({ layout: 'auth' })
useHead({ title: 'Tạo tài khoản' })

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
      <CardTitle as="h1">Tạo tài khoản</CardTitle>
      <CardDescription v-if="!done">Sau khi đăng ký, bạn có thể tạo tổ chức mới hoặc tham gia một tổ chức có sẵn.</CardDescription>
    </CardHeader>
    <CardContent>
      <div v-if="done" role="status" class="space-y-2 text-sm">
        <p>Mở hộp thư <strong>{{ form.email }}</strong> và bấm vào liên kết để xác minh địa chỉ email.</p>
        <NuxtLink to="/login" class="text-primary underline-offset-4 hover:underline">Đến trang đăng nhập</NuxtLink>
      </div>
      <form v-else class="space-y-4" novalidate @submit.prevent="submit">
        <FormError :message="error" />
        <FormField id="display_name" label="Tên của bạn" :error="fields.display_name">
          <template #default="{ describedBy, invalid }">
            <Input id="display_name" v-model="form.display_name" autocomplete="name" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <FormField id="email" label="Email" :error="fields.email">
          <template #default="{ describedBy, invalid }">
            <Input id="email" v-model="form.email" type="email" autocomplete="email" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <FormField id="password" label="Mật khẩu" :error="fields.password" hint="Tối thiểu 12 ký tự. Bạn có thể dùng một câu ngắn dễ nhớ.">
          <template #default="{ describedBy, invalid }">
            <Input id="password" v-model="form.password" type="password" autocomplete="new-password" :aria-invalid="invalid" :aria-describedby="describedBy" required />
          </template>
        </FormField>
        <Button type="submit" class="w-full" :disabled="busy">{{ busy ? 'Đang tạo…' : 'Tạo tài khoản' }}</Button>
      </form>
    </CardContent>
    <CardFooter class="text-sm">
      <NuxtLink to="/login" class="text-muted-foreground underline-offset-4 hover:underline">Đã có tài khoản? Đăng nhập</NuxtLink>
    </CardFooter>
  </Card>
</template>
