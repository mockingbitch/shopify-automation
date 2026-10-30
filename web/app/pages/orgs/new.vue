<script setup lang="ts">
useHead({ title: 'New organization' })

const form = reactive({ name: '', slug: '' })
const slugEdited = ref(false)
const fields = ref<Record<string, string>>({})
const error = ref('')
const busy = ref(false)

function slugify(s: string) {
  return s.normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/đ/gi, 'd')
    .toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40)
}
watch(() => form.name, (n) => { if (!slugEdited.value) form.slug = slugify(n) })

async function submit() {
  error.value = ''
  fields.value = {}
  busy.value = true
  try {
    const res = unwrap(await useApi().POST('/orgs', { body: { ...form } }))
    await useSessionStore().load(true)
    await navigateTo(`/orgs/${res.data.id}`)
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
  <div class="mx-auto max-w-lg">
    <PageHeader title="New organization" description="You will be its owner. You can invite members afterwards." />
    <Card>
      <CardContent class="pt-6">
        <form class="space-y-4" novalidate @submit.prevent="submit">
          <FormError :message="error" />
          <FormField id="name" label="Name" :error="fields.name">
            <template #default="{ describedBy, invalid }">
              <Input id="name" v-model="form.name" :aria-invalid="invalid" :aria-describedby="describedBy" required />
            </template>
          </FormField>
          <FormField id="slug" label="URL name" :error="fields.slug" hint="Lowercase letters, digits and hyphens.">
            <template #default="{ describedBy, invalid }">
              <Input id="slug" v-model="form.slug" :aria-invalid="invalid" :aria-describedby="describedBy" required @input="slugEdited = true" />
            </template>
          </FormField>
          <Button type="submit" :disabled="busy">Create organization</Button>
        </form>
      </CardContent>
    </Card>
  </div>
</template>
