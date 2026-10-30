<script setup lang="ts">
import type { components } from '~/types/api'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Shop Templates' })

type Template = components['schemas']['Template']

const api = useApi()
const { orgId, can } = useOrg()
const templates = ref<Template[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')
const form = reactive({ name: '', description: '' })
const fields = ref<Record<string, string>>({})
const busy = ref(false)

async function load() {
  loading.value = true
  try {
    templates.value = unwrap(await api.GET('/orgs/{orgID}/templates', { params: { path: { orgID: orgId.value } } })).data
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)

async function create() {
  busy.value = true
  fields.value = {}
  error.value = ''
  try {
    const res = unwrap(await api.POST('/orgs/{orgID}/templates', {
      params: { path: { orgID: orgId.value } }, body: { name: form.name, description: form.description },
    }))
    await navigateTo(`/orgs/${orgId.value}/templates/${res.data.template.id}`)
  } catch (e) {
    const err = toApiError(e)
    fields.value = err.fields
    if (!Object.keys(err.fields).length) error.value = err.message
  } finally {
    busy.value = false
  }
}

async function clone(t: Template) {
  error.value = ''
  try {
    const res = unwrap(await api.POST('/orgs/{orgID}/templates/{templateID}/clone', {
      params: { path: { orgID: orgId.value, templateID: t.id } }, body: { name: `${t.name} (copy)` },
    }))
    notice.value = `Created “${res.data.template.name}”.`
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function remove(t: Template) {
  error.value = ''
  try {
    unwrap(await api.DELETE('/orgs/{orgID}/templates/{templateID}', { params: { path: { orgID: orgId.value, templateID: t.id } } }))
    notice.value = `“${t.name}” was deleted.`
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}

const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Shop Templates" description="Reusable shop configurations. Publish a version, then preview what it would change on each shop." />
    <div aria-live="polite">
      <Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert>
    </div>
    <FormError :message="error" />

    <form v-if="can('templates.write')" class="grid gap-3 rounded-lg border bg-background p-4 sm:grid-cols-[1fr_2fr_auto] sm:items-end" novalidate @submit.prevent="create">
      <FormField id="tpl-name" label="New template name" :error="fields.name"><Input id="tpl-name" v-model="form.name" required /></FormField>
      <FormField id="tpl-desc" label="Description (optional)" :error="fields.description"><Input id="tpl-desc" v-model="form.description" /></FormField>
      <Button type="submit" :disabled="busy || !form.name.trim()">Create template</Button>
    </form>

    <p v-if="loading && !templates.length" role="status" class="text-sm text-muted-foreground">Loading…</p>
    <EmptyState v-else-if="!templates.length" title="No templates yet" description="A template holds your shop name, description, branding, addresses, shipping channels and product defaults." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Template</TableHead>
            <TableHead>Published</TableHead>
            <TableHead>Draft</TableHead>
            <TableHead>Updated</TableHead>
            <TableHead><span class="sr-only">Actions</span></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="t in templates" :key="t.id">
            <TableCell>
              <NuxtLink :to="`/orgs/${orgId}/templates/${t.id}`" class="font-medium underline-offset-4 hover:underline">{{ t.name }}</NuxtLink>
              <div v-if="t.description" class="text-xs text-muted-foreground">{{ t.description }}</div>
            </TableCell>
            <TableCell>
              <Badge v-if="t.current_published_version_id" variant="default">Yes</Badge>
              <span v-else class="text-sm text-muted-foreground">Not yet</span>
            </TableCell>
            <TableCell>{{ t.draft_version_id ? `v${t.latest_version_no}` : '—' }}</TableCell>
            <TableCell>{{ fmt.format(new Date(t.updated_at)) }}</TableCell>
            <TableCell class="space-x-2 text-right">
              <Button v-if="can('templates.write')" variant="outline" size="sm" @click="clone(t)">Clone</Button>
              <ConfirmDialog v-if="can('templates.publish')" :title="`Delete “${t.name}”?`"
                             description="The template and its versions are hidden. Shops already configured are not changed." confirm-label="Delete" @confirm="remove(t)">
                <template #trigger><Button variant="outline" size="sm">Delete</Button></template>
              </ConfirmDialog>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
  </div>
</template>
