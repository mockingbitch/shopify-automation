<script setup lang="ts">
useHead({ title: 'Bảo mật tài khoản' })

const api = useApi()
const route = useRoute()
const session = useSessionStore()
const requiredBy = computed(() => (typeof route.query.required === 'string' ? route.query.required : ''))
const status = ref<{ enabled: boolean; recovery_codes_left: number; available: boolean } | null>(null)
const setup = ref<{ secret: string; otpauth_uri: string } | null>(null)
const codes = ref<string[]>([])
const code = ref('')
const password = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)

async function load() {
  try {
    status.value = unwrap(await api.GET('/auth/mfa')).data
  } catch (e) {
    error.value = toApiError(e).message
  }
}
onMounted(load)

async function act(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    await fn()
  } catch (e) {
    const err = toApiError(e)
    error.value = Object.values(err.fields).join(' ') || err.message
  } finally {
    busy.value = false
  }
}

const start = () => act(async () => {
  setup.value = unwrap(await api.POST('/auth/mfa/setup')).data
  codes.value = []
})
const confirm = () => act(async () => {
  codes.value = unwrap(await api.POST('/auth/mfa/confirm', { body: { code: code.value } })).data.recovery_codes
  setup.value = null
  code.value = ''
  notice.value = 'Đã bật xác minh hai bước. Bạn hãy lưu các mã khôi phục bên dưới.'
  await session.load(true)
  await load()
})
const regenerate = () => act(async () => {
  codes.value = unwrap(await api.POST('/auth/mfa/recovery-codes', { body: { code: code.value } })).data.recovery_codes
  code.value = ''
  notice.value = 'Đã tạo mã khôi phục mới. Các mã cũ không còn dùng được.'
  await load()
})
const disable = () => act(async () => {
  unwrap(await api.POST('/auth/mfa/disable', { body: { password: password.value, code: code.value } }))
  code.value = ''
  password.value = ''
  codes.value = []
  notice.value = 'Đã tắt xác minh hai bước.'
  await session.load(true)
  await load()
})
const grouped = computed(() => setup.value?.secret.match(/.{1,4}/g)?.join(' ') ?? '')
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-6">
    <PageHeader title="Bảo mật tài khoản" description="Xác minh hai bước bảo vệ tài khoản của bạn bằng mã từ ứng dụng xác thực." />
    <Alert v-if="requiredBy && !status?.enabled" role="alert">
      <AlertDescription>{{ requiredBy }} yêu cầu chủ sở hữu và quản trị viên bật xác minh hai bước. Bạn hãy bật để tiếp tục.</AlertDescription>
    </Alert>
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <p v-if="!status" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <Alert v-else-if="!status.available" variant="destructive">
      <AlertDescription>Máy chủ này chưa hỗ trợ xác minh hai bước.</AlertDescription>
    </Alert>

    <Card v-else-if="!status.enabled">
      <CardHeader>
        <CardTitle as="h2" class="text-base">Xác minh hai bước đang tắt</CardTitle>
        <CardDescription>Tổ chức có thể yêu cầu chủ sở hữu và quản trị viên bật tính năng này.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <Button v-if="!setup" :disabled="busy" @click="start">Bật</Button>
        <form v-else class="space-y-4" @submit.prevent="confirm">
          <ol class="list-decimal space-y-2 pl-5 text-sm">
            <li>
              Trong ứng dụng xác thực (Google Authenticator, 1Password, Authy…), thêm tài khoản bằng khóa này:
              <code class="mt-1 block break-all rounded bg-muted px-2 py-1 font-mono text-base tracking-wider">{{ grouped }}</code>
              <a :href="setup.otpauth_uri" class="text-xs underline">Mở bằng ứng dụng xác thực trên thiết bị này</a>
            </li>
            <li>Nhập mã 6 chữ số mà ứng dụng hiển thị.</li>
          </ol>
          <FormField id="mfa-code" label="Mã">
            <Input id="mfa-code" v-model="code" inputmode="numeric" autocomplete="one-time-code" maxlength="6" required />
          </FormField>
          <Button type="submit" :disabled="busy || code.length < 6">Xác nhận</Button>
        </form>
      </CardContent>
    </Card>

    <Card v-else>
      <CardHeader>
        <CardTitle as="h2" class="text-base">Xác minh hai bước đang bật</CardTitle>
        <CardDescription>Còn {{ status.recovery_codes_left }} mã khôi phục.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <FormField id="mfa-current" label="Mã hiện tại trong ứng dụng (hoặc mã khôi phục, nếu muốn tắt)">
          <Input id="mfa-current" v-model="code" autocomplete="one-time-code" maxlength="20" />
        </FormField>
        <Button variant="outline" :disabled="busy || !code" @click="regenerate">Tạo mã khôi phục mới</Button>
        <form class="space-y-3 border-t pt-4" @submit.prevent="disable">
          <FormField id="mfa-password" label="Mật khẩu (để tắt)">
            <Input id="mfa-password" v-model="password" type="password" autocomplete="current-password" />
          </FormField>
          <Button type="submit" variant="destructive" :disabled="busy || !code || !password">Tắt</Button>
        </form>
      </CardContent>
    </Card>

    <Card v-if="codes.length">
      <CardHeader>
        <CardTitle as="h2" class="text-base">Mã khôi phục</CardTitle>
        <CardDescription>Mỗi mã dùng được một lần khi bạn mất điện thoại. Mã chỉ hiện lúc này, bạn hãy lưu ở nơi an toàn.</CardDescription>
      </CardHeader>
      <CardContent>
        <ul class="grid grid-cols-2 gap-2 font-mono text-sm">
          <li v-for="c in codes" :key="c">{{ c }}</li>
        </ul>
      </CardContent>
    </Card>
  </div>
</template>
