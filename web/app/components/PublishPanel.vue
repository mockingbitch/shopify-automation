<script setup lang="ts">
import type { Shop } from '~/utils/shops'

// Starts PRODUCT_PUBLISH runs for products (or an import) on chosen shops.
// Each run checks the products against the shop and waits for approval.
const props = defineProps<{ productIds?: string[]; importBatchId?: string; label: string }>()
const emit = defineEmits<{ close: [] }>()

const api = useApi()
const { orgId } = useOrg()
const shops = ref<Shop[]>([])
const selected = ref<string[]>([])
const status = ref<'UNLIST' | 'NORMAL'>('UNLIST')
const error = ref('')
const busy = ref(false)
// One key per opened panel, so a double click does not start runs twice.
const key = crypto.randomUUID()

onMounted(async () => {
  try {
    shops.value = unwrap(await api.GET('/orgs/{orgID}/shops', { params: { path: { orgID: orgId.value } } })).data
      .filter((s) => s.connection_status === 'ACTIVE')
  } catch (e) {
    error.value = toApiError(e).message
  }
})

async function start() {
  busy.value = true
  error.value = ''
  try {
    const res = unwrap(await api.POST('/orgs/{orgID}/products/publish', {
      params: { path: { orgID: orgId.value }, header: { 'Idempotency-Key': key } },
      body: { shop_ids: selected.value, item_status: status.value, ...(props.importBatchId ? { import_batch_id: props.importBatchId } : { product_ids: props.productIds }) },
    }))
    await navigateTo(`/orgs/${orgId.value}/workflows?batch=${res.data.batch_id}`)
  } catch (e) {
    const err = toApiError(e)
    error.value = Object.values(err.fields).join(' ') || err.message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <section class="space-y-4 rounded-lg border bg-background p-4" aria-labelledby="publish-title">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h2 id="publish-title" class="text-base font-semibold">Publish {{ label }}</h2>
        <p class="text-sm text-muted-foreground">
          Each shop gets its own run. It checks every product against the shop's categories, attributes, brands and shipping,
          then waits for your approval before creating anything on Shopee.
        </p>
      </div>
      <Button variant="outline" size="sm" @click="emit('close')">Close</Button>
    </div>
    <FormError :message="error" />
    <fieldset class="space-y-2">
      <legend class="text-sm font-medium">Shops</legend>
      <p v-if="!shops.length" class="text-sm text-muted-foreground">No connected shop. Connect a shop first.</p>
      <label v-for="s in shops" :key="s.id" class="flex items-center gap-2 text-sm">
        <input v-model="selected" type="checkbox" :value="s.id" class="size-4">
        {{ s.shop_name ?? `Shop ${s.shopee_shop_id}` }} <span class="text-muted-foreground">({{ s.region }})</span>
      </label>
    </fieldset>
    <fieldset class="space-y-2">
      <legend class="text-sm font-medium">After creation</legend>
      <label class="flex items-center gap-2 text-sm"><input v-model="status" type="radio" value="UNLIST" class="size-4"> Keep unlisted so I can review it in Seller Centre</label>
      <label class="flex items-center gap-2 text-sm"><input v-model="status" type="radio" value="NORMAL" class="size-4"> List for sale immediately</label>
    </fieldset>
    <div class="flex justify-end">
      <Button :disabled="busy || !selected.length" @click="start">Check and publish</Button>
    </div>
  </section>
</template>
