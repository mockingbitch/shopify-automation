<script setup lang="ts">
import { publishLabels, publishVariant, toInput, type Product, type ProductInput } from '~/utils/products'
import type { Shop } from '~/utils/shops'

definePageMeta({ middleware: 'org' })

const api = useApi()
const upload = useUpload()
const route = useRoute()
const { orgId, can } = useOrg()
const id = computed(() => String(route.params.productId))
const product = ref<Product | null>(null)
const etag = ref('')
const shops = ref<Record<string, Shop>>({})
const errors = ref<Record<string, string>>({})
const error = ref('')
const notice = ref(route.query.created ? 'Product created. Add images, then publish it.' : '')
const busy = ref(false)
const formKey = ref(0)
const publishing = ref(false)
useHead({ title: () => product.value?.name ?? 'Product' })

async function load() {
  try {
    const res = await api.GET('/orgs/{orgID}/products/{productID}', { params: { path: { orgID: orgId.value, productID: id.value } } })
    product.value = unwrap(res).data
    etag.value = res.response.headers.get('ETag') ?? ''
  } catch (e) {
    error.value = toApiError(e).message
  }
}
onMounted(async () => {
  await load()
  try {
    shops.value = Object.fromEntries(unwrap(await api.GET('/orgs/{orgID}/shops', { params: { path: { orgID: orgId.value } } })).data.map((s) => [s.id, s]))
  } catch { /* names are optional */ }
})

async function save(input: ProductInput) {
  busy.value = true
  errors.value = {}
  error.value = ''
  notice.value = ''
  try {
    const res = await api.PUT('/orgs/{orgID}/products/{productID}', {
      params: { path: { orgID: orgId.value, productID: id.value }, header: { 'If-Match': etag.value } }, body: input,
    })
    product.value = unwrap(res).data
    etag.value = res.response.headers.get('ETag') ?? ''
    notice.value = 'Saved.'
  } catch (e) {
    const err = toApiError(e)
    errors.value = err.fields
    if (err.status === 412) {
      error.value = 'Someone else changed this product. The latest version was loaded; make your changes again.'
      await load()
      formKey.value++
    } else {
      error.value = Object.keys(err.fields).length ? 'Some fields need attention.' : err.message
    }
  } finally {
    busy.value = false
  }
}

const fileInput = ref<HTMLInputElement>()
const uploading = ref(false)
async function addImages(ev: Event) {
  const files = Array.from((ev.target as HTMLInputElement).files ?? [])
  uploading.value = true
  error.value = ''
  for (const file of files) {
    try {
      await upload(`/orgs/${orgId.value}/products/${id.value}/images`, file)
    } catch (e) {
      error.value = `${file.name}: ${toApiError(e).message}`
      break
    }
  }
  uploading.value = false
  if (fileInput.value) fileInput.value.value = ''
  await load()
}
async function removeImage(imageID: string) {
  try {
    unwrap(await api.DELETE('/orgs/{orgID}/products/{productID}/images/{imageID}', { params: { path: { orgID: orgId.value, productID: id.value, imageID } } }))
    await load()
  } catch (e) {
    error.value = toApiError(e).message
  }
}
async function remove() {
  try {
    unwrap(await api.DELETE('/orgs/{orgID}/products/{productID}', { params: { path: { orgID: orgId.value, productID: id.value } } }))
    await navigateTo(`/orgs/${orgId.value}/products`)
  } catch (e) {
    error.value = toApiError(e).message
  }
}
const shopName = (sid: string) => shops.value[sid]?.shop_name ?? (shops.value[sid] ? `Shop ${shops.value[sid]!.shopee_shop_id}` : 'Shop')
const fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <PageHeader :title="product?.name ?? 'Product'" :description="product ? `SKU ${product.sku}` : undefined">
      <template v-if="product" #actions>
        <Button v-if="can('workflows.run')" @click="publishing = true">Publish</Button>
        <ConfirmDialog v-if="can('products.write')" title="Delete this product?"
                       description="It is removed from your catalog. Items already created on Shopee are not deleted." confirm-label="Delete" @confirm="remove">
          <template #trigger><Button variant="outline">Delete</Button></template>
        </ConfirmDialog>
      </template>
    </PageHeader>
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />
    <PublishPanel v-if="publishing && product" :product-ids="[product.id]" :label="product.sku" @close="publishing = false" />

    <template v-if="product">
      <section class="space-y-3 rounded-lg border bg-background p-4" aria-labelledby="pd-images">
        <div class="flex items-center justify-between">
          <h2 id="pd-images" class="text-base font-semibold">Images</h2>
          <template v-if="can('products.write')">
            <input id="pd-file" ref="fileInput" type="file" accept="image/jpeg,image/png,image/webp" multiple class="sr-only" @change="addImages">
            <Button as-child variant="outline" size="sm" :disabled="uploading"><label for="pd-file" class="cursor-pointer">{{ uploading ? 'Uploading…' : 'Add images' }}</label></Button>
          </template>
        </div>
        <p class="text-sm text-muted-foreground">JPEG, PNG or WebP, up to 10 MB each. The first image is the cover.</p>
        <ul v-if="product.images.length" class="grid grid-cols-3 gap-3 sm:grid-cols-5">
          <li v-for="im in product.images" :key="im.id" class="space-y-1">
            <img :src="im.url" :alt="`Image ${im.position + 1}`" class="aspect-square w-full rounded border object-cover">
            <div class="flex items-center justify-between text-xs text-muted-foreground">
              <span>{{ im.width }}×{{ im.height }}</span>
              <button v-if="can('products.write')" type="button" class="underline" @click="removeImage(im.id)">Remove</button>
            </div>
          </li>
        </ul>
        <p v-else class="text-sm text-muted-foreground">No images yet. Shopee requires at least one.</p>
      </section>

      <section v-if="product.mappings.length" class="space-y-2 rounded-lg border bg-background p-4" aria-labelledby="pd-shops">
        <h2 id="pd-shops" class="text-base font-semibold">On Shopee</h2>
        <ul class="divide-y text-sm">
          <li v-for="m in product.mappings" :key="m.shop_id" class="flex flex-wrap items-center justify-between gap-2 py-2">
            <span>{{ shopName(m.shop_id) }}</span>
            <span class="flex items-center gap-2">
              <span v-if="m.shopee_item_id" class="text-muted-foreground">Item {{ m.shopee_item_id }}</span>
              <Badge :variant="publishVariant(m.publish_state)">{{ publishLabels[m.publish_state] }}</Badge>
              <span class="text-xs text-muted-foreground">{{ fmt.format(new Date(m.updated_at)) }}</span>
            </span>
          </li>
        </ul>
      </section>

      <ProductForm :key="formKey + etag" :initial="toInput(product)" :errors="errors" :busy="busy" :readonly="!can('products.write')" submit-label="Save changes" @submit="save" />
    </template>
  </div>
</template>
