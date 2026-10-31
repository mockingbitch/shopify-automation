<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Settings' })

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
    notice.value = 'Settings saved.'
    await session.load(true)
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    error.value = err.status === 412 ? 'Someone else changed these settings. Reload the page to see the latest version.' : (Object.keys(err.fields).length ? '' : err.message)
  } finally {
    saving.value = false
  }
}

async function transfer() {
  error.value = ''
  try {
    unwrap(await api.POST('/orgs/{orgID}/transfer-ownership', { params: { path: path.value }, body: { new_owner_user_id: newOwner.value } }))
    await session.load(true)
    notice.value = 'Ownership transferred. You are now an admin.'
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
    <PageHeader title="Settings" />
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />

    <Card>
      <CardHeader><CardTitle as="h2">General</CardTitle></CardHeader>
      <CardContent>
        <form class="space-y-4" novalidate @submit.prevent="save">
          <FormField id="org-name" label="Organization name" :error="fields.name">
            <template #default="{ describedBy, invalid }">
              <Input id="org-name" v-model="form.name" :disabled="!can('org.update')" :aria-invalid="invalid" :aria-describedby="describedBy" />
            </template>
          </FormField>
          <FormField id="org-slug" label="URL name" hint="The URL name cannot be changed.">
            <Input id="org-slug" :model-value="form.slug" disabled />
          </FormField>
          <div class="flex items-start gap-3">
            <input id="self-approval" v-model="form.allowSelfApproval" type="checkbox" class="mt-1 size-4 accent-primary" :disabled="!can('org.update')" aria-describedby="self-approval-hint">
            <div>
              <Label for="self-approval">Operators may approve their own workflows</Label>
              <p id="self-approval-hint" class="text-xs text-muted-foreground">When off, someone other than the person who started a workflow must approve its changes.</p>
            </div>
          </div>
          <div class="flex items-start gap-3">
            <input id="require-mfa" v-model="form.requireMFA" type="checkbox" class="mt-1 size-4 accent-primary" :disabled="!can('org.update')" aria-describedby="require-mfa-hint">
            <div>
              <Label for="require-mfa">Require two-step verification for owners and admins</Label>
              <p id="require-mfa-hint" class="text-xs text-muted-foreground">
                Owners and admins without it cannot open this organization until they turn it on in
                <NuxtLink to="/account" class="underline">Account security</NuxtLink>. Turn it on for your own account first.
              </p>
              <p v-if="fields['settings.require_mfa']" class="text-xs text-destructive">{{ fields['settings.require_mfa'] }}</p>
            </div>
          </div>
          <Button v-if="can('org.update')" type="submit" :disabled="saving">Save changes</Button>
        </form>
      </CardContent>
    </Card>

    <Card v-if="can('audit.read')">
      <CardHeader>
        <CardTitle as="h2">Audit log</CardTitle>
        <CardDescription>See who changed what in this organization.</CardDescription>
      </CardHeader>
      <CardContent><Button variant="outline" as-child><NuxtLink :to="`/orgs/${orgId}/audit`">Open audit log</NuxtLink></Button></CardContent>
    </Card>

    <Card v-if="role === 'owner'">
      <CardHeader>
        <CardTitle as="h2">Transfer ownership</CardTitle>
        <CardDescription>The new owner must already be a member. You will become an admin.</CardDescription>
      </CardHeader>
      <CardContent class="flex flex-col gap-3 sm:flex-row sm:items-end">
        <FormField id="new-owner" label="New owner" class="flex-1">
          <Select v-model="newOwner">
            <SelectTrigger id="new-owner" class="w-full"><SelectValue placeholder="Choose a member" /></SelectTrigger>
            <SelectContent>
              <SelectItem v-for="m in members" :key="m.user_id" :value="m.user_id">{{ m.display_name }} ({{ m.email }})</SelectItem>
            </SelectContent>
          </Select>
        </FormField>
        <ConfirmDialog title="Transfer ownership?" description="You will no longer be able to delete the organization or transfer it again." confirm-label="Transfer" :disabled="!newOwner" @confirm="transfer">
          <template #trigger><Button variant="outline" :disabled="!newOwner">Transfer</Button></template>
        </ConfirmDialog>
      </CardContent>
    </Card>

    <Card v-if="can('org.delete')" class="border-destructive/40">
      <CardHeader>
        <CardTitle as="h2" class="text-destructive">Delete organization</CardTitle>
        <CardDescription>All members lose access. This cannot be undone from the dashboard.</CardDescription>
      </CardHeader>
      <CardContent>
        <ConfirmDialog title="Delete this organization?" :description="`Type ${form.slug} to confirm.`" confirm-label="Delete" :disabled="confirmSlug !== form.slug" @confirm="remove">
          <template #trigger><Button variant="destructive">Delete organization</Button></template>
          <div class="space-y-1.5">
            <Label for="confirm-slug">Organization URL name</Label>
            <Input id="confirm-slug" v-model="confirmSlug" autocomplete="off" />
          </div>
        </ConfirmDialog>
      </CardContent>
    </Card>
  </div>
</template>
