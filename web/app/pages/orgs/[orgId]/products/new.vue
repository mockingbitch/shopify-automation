<script setup lang="ts">
import type { ProductInput } from '~/utils/products'

definePageMeta({ middleware: 'org' })
useHead({ title: 'New product' })

const api = useApi()
const { orgId } = useOrg()
const errors = ref<Record<string, string>>({})
const error = ref('')
const busy = ref(false)

async function create(input: ProductInput) {
  busy.value = true
  errors.value = {}
  error.value = ''
  try {
    const res = unwrap(await api.POST('/orgs/{orgID}/products', { params: { path: { orgID: orgId.value } }, body: input }))
    await navigateTo(`/orgs/${orgId.value}/products/${res.data.id}?created=1`)
  } catch (e) {
    const err = toApiError(e)
    errors.value = err.fields
    error.value = Object.keys(err.fields).length ? 'Some fields need attention.' : err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <PageHeader title="New product" description="Add images after saving." />
    <FormError :message="error" />
    <ProductForm :errors="errors" :busy="busy" submit-label="Create product" @submit="create">
      <template #actions><Button variant="outline" as-child><NuxtLink :to="`/orgs/${orgId}/products`">Cancel</NuxtLink></Button></template>
    </ProductForm>
  </div>
</template>
