<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Cài đặt' })

type Member = components['schemas']['Member']
const api = useApi()
const session = useSessionStore()
const { orgId, role, can } = useOrg()
const path = computed(() => ({ orgID: orgId.value }))

const etag = ref('')
const form = reactive({ name: '', slug: '', allowSelfApproval: false, requireMFA: false })
const fields = ref<Record<string, string>>({})
const error = ref('')
const notice = ref('')
const saving = ref(false)

const members = ref<Member[]>([])
const newOwner = ref('')
const confirmSlug = ref('')

async function load() {
  const res = await api.GET('/orgs/{orgID}', { params: { path: path.value } })
  const org = unwrap(res).data
  etag.value = res.response.headers.get('ETag') ?? ''
  form.name = org.name
  form.slug = org.slug
  form.allowSelfApproval = org.settings.allow_self_approval
  form.requireMFA = org.settings.require_mfa ?? false
  if (role.value === 'owner') {
    members.value = unwrap(await api.GET('/orgs/{orgID}/members', { params: { path: path.value } })).data.filter((m) => m.role !== 'owner')
  }
}
onMounted(() => load().catch((e) => (error.value = toApiError(e).message)))

async function save() {
  saving.value = true
  fields.value = {}
  error.value = ''
  notice.value = ''
  try {
    const res = await api.PATCH('/orgs/{orgID}', {
      params: { path: path.value, header: { 'If-Match': etag.value } },
      body: { name: form.name, settings: { allow_self_approval: form.allowSelfApproval, require_mfa: form.requireMFA } },
    })
    unwrap(res)
    etag.value = res.response.headers.get('ETag') ?? ''
    notice.value = 'Đã lưu cài đặt.'
    await session.load(true)
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    error.value = err.status === 412 ? 'Người khác vừa thay đổi cài đặt này. Bạn hãy tải lại trang để xem bản mới nhất.' : (Object.keys(err.fields).length ? '' : err.message)
  } finally {
    saving.value = false
  }
}

async function transfer() {
  error.value = ''
  try {
    unwrap(await api.POST('/orgs/{orgID}/transfer-ownership', { params: { path: path.value }, body: { new_owner_user_id: newOwner.value } }))
    await session.load(true)
    notice.value = 'Đã chuyển quyền sở hữu. Giờ bạn là quản trị viên.'
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function remove() {
  error.value = ''
  try {
    unwrap(await api.DELETE('/orgs/{orgID}', { params: { path: path.value }, body: { confirm_slug: confirmSlug.value } }))
    await session.load(true)
    await navigateTo('/orgs')
  } catch (e) {
    error.value = toApiError(e).message
  }
}
</script>

<template>
  <div class="mx-auto max-w-3xl space-y-8">
    <PageHeader title="Cài đặt" />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <Card>
      <CardHeader><CardTitle as="h2">Thông tin chung</CardTitle></CardHeader>
      <CardContent>
        <form class="space-y-4" novalidate @submit.prevent="save">
          <FormField id="org-name" label="Tên tổ chức" :error="fields.name">
            <template #default="{ describedBy, invalid }">
              <Input id="org-name" v-model="form.name" :disabled="!can('org.update')" :aria-invalid="invalid" :aria-describedby="describedBy" />
            </template>
          </FormField>
          <FormField id="org-slug" label="Tên trong URL" hint="Không thể đổi tên trong URL.">
            <Input id="org-slug" :model-value="form.slug" disabled />
          </FormField>
          <div class="flex items-start gap-3">
            <input id="self-approval" v-model="form.allowSelfApproval" type="checkbox" class="mt-1 size-4 accent-primary" :disabled="!can('org.update')" aria-describedby="self-approval-hint">
            <div>
              <Label for="self-approval">Nhân viên vận hành được tự duyệt quy trình của mình</Label>
              <p id="self-approval-hint" class="text-xs text-muted-foreground">Khi tắt, thay đổi của một quy trình phải do người khác (không phải người bắt đầu quy trình) duyệt.</p>
            </div>
          </div>
          <div class="flex items-start gap-3">
            <input id="require-mfa" v-model="form.requireMFA" type="checkbox" class="mt-1 size-4 accent-primary" :disabled="!can('org.update')" aria-describedby="require-mfa-hint">
            <div>
              <Label for="require-mfa">Bắt buộc chủ sở hữu và quản trị viên bật xác minh hai bước</Label>
              <p id="require-mfa-hint" class="text-xs text-muted-foreground">
                Chủ sở hữu và quản trị viên chưa bật sẽ không mở được tổ chức này cho đến khi bật trong
                <NuxtLink to="/account" class="underline">Bảo mật tài khoản</NuxtLink>. Bạn hãy bật cho tài khoản của mình trước.
              </p>
              <p v-if="fields['settings.require_mfa']" class="text-xs text-destructive">{{ fields['settings.require_mfa'] }}</p>
            </div>
          </div>
          <Button v-if="can('org.update')" type="submit" :disabled="saving">Lưu thay đổi</Button>
        </form>
      </CardContent>
    </Card>

    <Card v-if="can('audit.read')">
      <CardHeader>
        <CardTitle as="h2">Nhật ký hoạt động</CardTitle>
        <CardDescription>Xem ai đã thay đổi gì trong tổ chức này.</CardDescription>
      </CardHeader>
      <CardContent><Button variant="outline" as-child><NuxtLink :to="`/orgs/${orgId}/audit`">Mở nhật ký hoạt động</NuxtLink></Button></CardContent>
    </Card>

    <Card v-if="role === 'owner'">
      <CardHeader>
        <CardTitle as="h2">Chuyển quyền sở hữu</CardTitle>
        <CardDescription>Chủ sở hữu mới phải là thành viên của tổ chức. Bạn sẽ trở thành quản trị viên.</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-3 sm:flex-row sm:items-end">
        <FormField id="new-owner" label="Chủ sở hữu mới" class="flex-1">
          <Select v-model="newOwner">
            <SelectTrigger id="new-owner" class="w-full"><SelectValue placeholder="Chọn thành viên" /></SelectTrigger>
            <SelectContent>
              <SelectItem v-for="m in members" :key="m.user_id" :value="m.user_id">{{ m.display_name }} ({{ m.email }})</SelectItem>
            </SelectContent>
          </Select>
        </FormField>
        <ConfirmDialog title="Chuyển quyền sở hữu?" description="Bạn sẽ không thể xóa tổ chức hay chuyển quyền sở hữu thêm lần nữa." confirm-label="Chuyển quyền" :disabled="!newOwner" @confirm="transfer">
          <template #trigger><Button variant="outline" :disabled="!newOwner">Chuyển quyền</Button></template>
        </ConfirmDialog>
      </CardContent>
    </Card>

    <Card v-if="can('org.delete')" class="border-destructive/40">
      <CardHeader>
        <CardTitle as="h2" class="text-destructive">Xóa tổ chức</CardTitle>
        <CardDescription>Mọi thành viên sẽ mất quyền truy cập. Không thể hoàn tác thao tác này trên bảng điều khiển.</CardDescription>
      </CardHeader>
      <CardContent>
        <ConfirmDialog title="Xóa tổ chức này?" :description="`Nhập ${form.slug} để xác nhận.`" confirm-label="Xóa" :disabled="confirmSlug !== form.slug" @confirm="remove">
          <template #trigger><Button variant="destructive">Xóa tổ chức</Button></template>
          <div class="space-y-1.5">
            <Label for="confirm-slug">Tên trong URL của tổ chức</Label>
            <Input id="confirm-slug" v-model="confirmSlug" autocomplete="off" />
          </div>
        </ConfirmDialog>
      </CardContent>
    </Card>
  </div>
</template>
