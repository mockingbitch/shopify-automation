<script setup lang="ts">
import type { components } from '~/types/api'
import { assignableRoles, canAssign, canManageMember, roleLabels, type Role } from '~/utils/rbac'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Members' })

type Member = components['schemas']['Member']
type Invitation = components['schemas']['Invitation']

const api = useApi()
const session = useSessionStore()
const { orgId, membership, role, can } = useOrg()

const members = ref<Member[]>([])
const invitations = ref<Invitation[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')

const invite = reactive<{ email: string; role: Role }>({ email: '', role: 'operator' })
const inviteFields = ref<Record<string, string>>({})
const inviteError = ref('')

const myMemberId = computed(() => members.value.find((m) => m.user_id === session.user?.id)?.id)
const path = computed(() => ({ orgID: orgId.value }))

async function load() {
  loading.value = true
  error.value = ''
  try {
    members.value = unwrap(await api.GET('/orgs/{orgID}/members', { params: { path: path.value } })).data
    if (can('members.manage')) {
      invitations.value = unwrap(await api.GET('/orgs/{orgID}/invitations', { params: { path: path.value } })).data
    }
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function run(action: () => Promise<unknown>, success: string) {
  error.value = ''
  notice.value = ''
  try {
    await action()
    notice.value = success
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}

function changeRole(m: Member, value: unknown) {
  const next = value as Role
  if (next === m.role) return
  void run(async () => unwrap(await api.PATCH('/orgs/{orgID}/members/{memberID}', {
    params: { path: { orgID: orgId.value, memberID: m.id } }, body: { role: next as 'admin' | 'operator' | 'viewer' },
  })), `${m.display_name} is now ${roleLabels[next]}.`)
}

function remove(m: Member) {
  const self = m.id === myMemberId.value
  void run(async () => {
    unwrap(await api.DELETE('/orgs/{orgID}/members/{memberID}', { params: { path: { orgID: orgId.value, memberID: m.id } } }))
    if (self) {
      await session.load(true)
      await navigateTo('/orgs')
    }
  }, `${m.display_name} was removed.`)
}

async function sendInvite() {
  inviteFields.value = {}
  inviteError.value = ''
  try {
    unwrap(await api.POST('/orgs/{orgID}/invitations', {
      params: { path: path.value }, body: { email: invite.email, role: invite.role as 'admin' | 'operator' | 'viewer' },
    }))
    notice.value = `Invitation sent to ${invite.email}.`
    invite.email = ''
    await load()
  } catch (e) {
    const err = toApiError(e)
    inviteFields.value = err.fields
    if (!Object.keys(err.fields).length) inviteError.value = err.message
  }
}

function revoke(i: Invitation) {
  void run(async () => unwrap(await api.DELETE('/orgs/{orgID}/invitations/{invitationID}', {
    params: { path: { orgID: orgId.value, invitationID: i.id } },
  })), `Invitation to ${i.email} was revoked.`)
}

const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' })
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-8">
    <PageHeader title="Members" :description="`People with access to ${membership?.organization.name ?? 'this organization'}.`" />

    <div aria-live="polite">
      <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
    </div>
    <FormError :message="error" />

    <section aria-labelledby="members-heading">
      <h2 id="members-heading" class="sr-only">Members</h2>
      <p v-if="loading && !members.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
      <div v-else class="overflow-x-auto rounded-lg border bg-background">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="m in members" :key="m.id">
              <TableCell class="font-medium">{{ m.display_name }}<span v-if="m.id === myMemberId" class="text-muted-foreground"> (you)</span></TableCell>
              <TableCell class="text-muted-foreground">{{ m.email }}</TableCell>
              <TableCell>
                <Select
                  v-if="m.id !== myMemberId && canManageMember(role, m.role)"
                  :model-value="m.role"
                  @update:model-value="(v) => changeRole(m, v)"
                >
                  <SelectTrigger class="w-36" :aria-label="`Role for ${m.display_name}`"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem v-for="r in assignableRoles.filter((r) => canAssign(role, r))" :key="r" :value="r">{{ roleLabels[r] }}</SelectItem>
                  </SelectContent>
                </Select>
                <Badge v-else variant="secondary">{{ roleLabels[m.role] }}</Badge>
              </TableCell>
              <TableCell class="text-right">
                <ConfirmDialog
                  v-if="m.role !== 'owner' && (m.id === myMemberId || canManageMember(role, m.role))"
                  :title="m.id === myMemberId ? 'Leave organization?' : `Remove ${m.display_name}?`"
                  :description="m.id === myMemberId ? 'You will lose access until someone invites you again.' : 'They will lose access to this organization immediately.'"
                  :confirm-label="m.id === myMemberId ? 'Leave' : 'Remove'"
                  @confirm="remove(m)"
                >
                  <template #trigger>
                    <Button variant="ghost" size="sm">{{ m.id === myMemberId ? 'Leave' : 'Remove' }}</Button>
                  </template>
                </ConfirmDialog>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </section>

    <section v-if="can('members.manage')" aria-labelledby="invite-heading" class="space-y-4">
      <h2 id="invite-heading" class="text-lg font-medium">Invite someone</h2>
      <Card>
        <CardContent class="pt-6">
          <form class="grid gap-4 sm:grid-cols-[1fr_12rem_auto] sm:items-end" novalidate @submit.prevent="sendInvite">
            <FormField id="invite-email" label="Email" :error="inviteFields.email">
              <template #default="{ describedBy, invalid }">
                <Input id="invite-email" v-model="invite.email" type="email" :aria-invalid="invalid" :aria-describedby="describedBy" required />
              </template>
            </FormField>
            <FormField id="invite-role" label="Role" :error="inviteFields.role">
              <Select v-model="invite.role">
                <SelectTrigger id="invite-role" class="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="r in assignableRoles.filter((r) => canAssign(role, r))" :key="r" :value="r">{{ roleLabels[r] }}</SelectItem>
                </SelectContent>
              </Select>
            </FormField>
            <Button type="submit">Send invitation</Button>
          </form>
          <div class="mt-3"><FormError :message="inviteError" /></div>
        </CardContent>
      </Card>

      <div v-if="invitations.length" class="overflow-x-auto rounded-lg border bg-background">
        <Table>
          <TableCaption class="sr-only">Pending invitations</TableCaption>
          <TableHeader>
            <TableRow>
              <TableHead>Pending invitation</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead><span class="sr-only">Actions</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="i in invitations" :key="i.id">
              <TableCell>{{ i.email }}</TableCell>
              <TableCell>{{ roleLabels[i.role] }}</TableCell>
              <TableCell><time :datetime="i.expires_at">{{ fmt.format(new Date(i.expires_at)) }}</time></TableCell>
              <TableCell class="text-right">
                <ConfirmDialog :title="`Revoke invitation to ${i.email}?`" description="The link in their email will stop working." confirm-label="Revoke" @confirm="revoke(i)">
                  <template #trigger><Button variant="ghost" size="sm">Revoke</Button></template>
                </ConfirmDialog>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </div>
    </section>
  </div>
</template>
