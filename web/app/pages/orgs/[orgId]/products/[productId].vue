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
const notice = ref(route.query.created ? 'Đã tạo sản phẩm. Hãy thêm hình ảnh rồi đăng sản phẩm lên shop.' : '')
const busy = ref(false)
const formKey = ref(0)
const publishing = ref(false)
useHead({ title: () => product.value?.name ?? 'Sản phẩm' })

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
    notice.value = 'Đã lưu.'
  } catch (e) {
    const err = toApiError(e)
    errors.value = err.fields
    if (err.status === 412) {
      error.value = 'Người khác vừa sửa sản phẩm này. Phiên bản mới nhất đã được tải lại; bạn hãy nhập lại thay đổi của mình.'
      await load()
      formKey.value++
    } else {
      error.value = Object.keys(err.fields).length ? 'Vui lòng sửa các ô được đánh dấu bên dưới.' : err.message
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
const fmt = new Intl.DateTimeFormat('vi-VN', { dateStyle: 'medium', timeStyle: 'short' })
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <PageHeader :title="product?.name ?? 'Sản phẩm'" :description="product ? `SKU ${product.sku}` : undefined">
      <template v-if="product" #actions>
        <Button v-if="can('workflows.run')" @click="publishing = true">Đăng lên shop</Button>
        <ConfirmDialog v-if="can('products.write')" title="Xóa sản phẩm này?"
                       description="Sản phẩm sẽ bị xóa khỏi danh sách của bạn. Sản phẩm đã tạo trên Shopee vẫn được giữ nguyên." confirm-label="Xóa" @confirm="remove">
          <template #trigger><Button variant="outline">Xóa</Button></template>
        </ConfirmDialog>
      </template>
    </PageHeader>
    <div aria-live="polite"><Alert v-if="notice"><AlertDescription>{{ notice }}</AlertDescription></Alert></div>
    <FormError :message="error" />
    <PublishPanel v-if="publishing && product" :product-ids="[product.id]" :label="product.sku" @close="publishing = false" />

    <template v-if="product">
      <section class="space-y-3 rounded-lg border bg-background p-4" aria-labelledby="pd-images">
        <div class="flex items-center justify-between">
          <h2 id="pd-images" class="text-base font-semibold">Hình ảnh</h2>
          <template v-if="can('products.write')">
            <input id="pd-file" ref="fileInput" type="file" accept="image/jpeg,image/png,image/webp" multiple class="sr-only" @change="addImages">
            <Button as-child variant="outline" size="sm" :disabled="uploading"><label for="pd-file" class="cursor-pointer">{{ uploading ? 'Đang tải lên…' : 'Thêm hình ảnh' }}</label></Button>
          </template>
        </div>
        <p class="text-sm text-muted-foreground">JPEG, PNG hoặc WebP, tối đa 10 MB mỗi ảnh. Ảnh đầu tiên là ảnh bìa.</p>
        <ul v-if="product.images.length" class="grid grid-cols-3 gap-3 sm:grid-cols-5">
          <li v-for="im in product.images" :key="im.id" class="space-y-1">
            <img :src="im.url" :alt="`Ảnh ${im.position + 1}`" class="aspect-square w-full rounded border object-cover">
            <div class="flex items-center justify-between text-xs text-muted-foreground">
              <span>{{ im.width }}×{{ im.height }}</span>
              <button v-if="can('products.write')" type="button" class="underline" :aria-label="`Xóa ảnh ${im.position + 1}`" @click="removeImage(im.id)">Xóa</button>
            </div>
          </li>
        </ul>
        <p v-else class="text-sm text-muted-foreground">Chưa có hình ảnh. Shopee yêu cầu ít nhất một ảnh.</p>
      </section>

      <section v-if="product.mappings.length" class="space-y-2 rounded-lg border bg-background p-4" aria-labelledby="pd-shops">
        <h2 id="pd-shops" class="text-base font-semibold">Trên Shopee</h2>
        <ul class="divide-y text-sm">
          <li v-for="m in product.mappings" :key="m.shop_id" class="flex flex-wrap items-center justify-between gap-2 py-2">
            <span>{{ shopName(m.shop_id) }}</span>
            <span class="flex items-center gap-2">
              <span v-if="m.shopee_item_id" class="text-muted-foreground">ID trên Shopee: {{ m.shopee_item_id }}</span>
              <Badge :variant="publishVariant(m.publish_state)">{{ publishLabels[m.publish_state] }}</Badge>
              <span class="text-xs text-muted-foreground">{{ fmt.format(new Date(m.updated_at)) }}</span>
            </span>
          </li>
        </ul>
      </section>

      <ProductForm :key="formKey + etag" :initial="toInput(product)" :errors="errors" :busy="busy" :readonly="!can('products.write')" submit-label="Lưu thay đổi" @submit="save" />
    </template>
  </div>
</template>
