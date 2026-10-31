<script setup lang="ts">
import { formatMoney, publishLabels, publishVariant, type Product } from '~/utils/products'

definePageMeta({ middleware: 'org' })
useHead({ title: 'Sản phẩm' })

const api = useApi()
const route = useRoute()
const { orgId, can } = useOrg()
const products = ref<Product[]>([])
const next = ref<string | null>(null)
const loading = ref(true)
const error = ref('')
const q = ref('')
const selected = ref<string[]>([])
const publishing = ref(false)
const batch = computed(() => (typeof route.query.batch === 'string' ? route.query.batch : undefined))

async function load(more = false) {
  loading.value = true
  error.value = ''
  try {
    const res = unwrap(await api.GET('/orgs/{orgID}/products', {
      params: { path: { orgID: orgId.value }, query: { q: q.value.trim() || undefined, batch_id: batch.value, cursor: more ? next.value ?? undefined : undefined } },
    }))
    products.value = more ? [...products.value, ...res.data] : res.data
    next.value = res.next_cursor
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(batch, () => load())
let timer: ReturnType<typeof setTimeout> | undefined
watch(q, () => {
  clearTimeout(timer)
  timer = setTimeout(() => load(), 300)
})

const allSelected = computed(() => products.value.length > 0 && selected.value.length === products.value.length)
function toggleAll() {
  selected.value = allSelected.value ? [] : products.value.map((p) => p.id)
}
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <PageHeader title="Sản phẩm" description="Danh sách sản phẩm của bạn. Chọn sản phẩm rồi đăng lên một hoặc nhiều shop.">
      <template v-if="can('products.write')" #actions>
        <Button variant="outline" as-child><NuxtLink :to="`/orgs/${orgId}/imports`">Nhập sản phẩm</NuxtLink></Button>
        <Button as-child><NuxtLink :to="`/orgs/${orgId}/products/new`">Thêm sản phẩm</NuxtLink></Button>
      </template>
    </PageHeader>
    <FormError :message="error" />
    <Alert v-if="batch"><AlertDescription>Đang hiển thị sản phẩm của một lô nhập. <NuxtLink :to="`/orgs/${orgId}/products`" class="underline">Xem tất cả</NuxtLink></AlertDescription></Alert>

    <div class="flex flex-wrap items-end gap-3">
      <div class="w-full max-w-xs space-y-1.5">
        <Label for="product-search">Tìm theo tên hoặc SKU</Label>
        <Input id="product-search" v-model="q" type="search" maxlength="100" />
      </div>
      <Button v-if="can('workflows.run')" :disabled="!selected.length" @click="publishing = true">
        Đăng {{ selected.length || '' }} sản phẩm đã chọn
      </Button>
    </div>

    <PublishPanel v-if="publishing" :product-ids="selected" :label="`${selected.length} sản phẩm`" @close="publishing = false" />

    <p v-if="loading && !products.length" role="status" class="text-sm text-muted-foreground">Đang tải…</p>
    <EmptyState v-else-if="!products.length" title="Chưa có sản phẩm" description="Thêm sản phẩm hoặc nhập từ file CSV hoặc Excel." />
    <div v-else class="overflow-x-auto rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead class="w-10"><input type="checkbox" class="size-4" :checked="allSelected" aria-label="Chọn tất cả sản phẩm" @change="toggleAll"></TableHead>
            <TableHead>Sản phẩm</TableHead>
            <TableHead>Giá</TableHead>
            <TableHead>Tồn kho</TableHead>
            <TableHead>Phân loại hàng</TableHead>
            <TableHead>Shop</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-for="p in products" :key="p.id">
            <TableCell><input v-model="selected" type="checkbox" class="size-4" :value="p.id" :aria-label="`Chọn ${p.sku}`"></TableCell>
            <TableCell>
              <div class="flex items-center gap-3">
                <img v-if="p.images[0]" :src="p.images[0].url" alt="" class="size-10 rounded border object-cover">
                <div>
                  <NuxtLink :to="`/orgs/${orgId}/products/${p.id}`" class="font-medium underline-offset-4 hover:underline">{{ p.name }}</NuxtLink>
                  <div class="text-xs text-muted-foreground">{{ p.sku }}</div>
                </div>
              </div>
            </TableCell>
            <TableCell>{{ formatMoney(p.price) }}</TableCell>
            <TableCell>{{ p.stock }}</TableCell>
            <TableCell>{{ p.variations.length || '—' }}</TableCell>
            <TableCell class="space-x-1">
              <Badge v-for="m in p.mappings" :key="m.shop_id" :variant="publishVariant(m.publish_state)">{{ publishLabels[m.publish_state] }}</Badge>
              <span v-if="!p.mappings.length" class="text-sm text-muted-foreground">Chưa đăng</span>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </div>
    <div v-if="next" class="flex justify-center"><Button variant="outline" :disabled="loading" @click="load(true)">Tải thêm</Button></div>
  </div>
</template>
