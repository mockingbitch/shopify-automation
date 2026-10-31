<script setup lang="ts">
import type { Shop } from '~/utils/shops'

// Looks up Shopee categories, attributes and brands through a connected
// shop (they differ per market) and hands the chosen values to the form.
const props = defineProps<{ categoryId: string }>()
const emit = defineEmits<{
  category: [id: string, label: string]
  attribute: [attributeId: number, valueId: number, name: string]
  brand: [name: string]
}>()

type Category = { category_id: number; parent_category_id: number; original_category_name: string; display_category_name?: string; has_children: boolean }
type Attribute = { attribute_id: number; mandatory: boolean; name: string; attribute_value_list?: { value_id?: number; name?: string }[] | null }
type Brand = { brand_id: number; original_brand_name: string; display_brand_name?: string }

const api = useApi()
const { orgId } = useOrg()
const shops = ref<Shop[]>([])
const shopId = ref('')
const categories = ref<Category[]>([])
const attributes = ref<Attribute[]>([])
const brands = ref<{ list: Brand[]; mandatory: boolean }>({ list: [], mandatory: false })
const q = ref('')
const error = ref('')
const loading = ref(false)

onMounted(async () => {
  try {
    shops.value = unwrap(await api.GET('/orgs/{orgID}/shops', { params: { path: { orgID: orgId.value }, query: { status: 'ACTIVE' } } })).data
    shopId.value = shops.value[0]?.id ?? ''
  } catch (e) {
    error.value = toApiError(e).message
  }
})

async function loadCategories() {
  if (!shopId.value) return
  loading.value = true
  error.value = ''
  try {
    categories.value = unwrap(await api.GET('/orgs/{orgID}/shops/{shopID}/catalog/categories', {
      params: { path: { orgID: orgId.value, shopID: shopId.value } },
    })).data as Category[]
    if (props.categoryId) await loadDetails(Number(props.categoryId))
  } catch (e) {
    error.value = toApiError(e).message
  } finally {
    loading.value = false
  }
}
watch(shopId, loadCategories)

const byId = computed(() => new Map(categories.value.map((c) => [c.category_id, c])))
function pathOf(c: Category): string {
  const parts = [c.display_category_name || c.original_category_name]
  let p = byId.value.get(c.parent_category_id)
  for (let i = 0; p && i < 6; i++) {
    parts.unshift(p.display_category_name || p.original_category_name)
    p = byId.value.get(p.parent_category_id)
  }
  return parts.join(' › ')
}
const matches = computed(() => {
  const term = q.value.trim().toLowerCase()
  if (term.length < 2) return []
  return categories.value
    .filter((c) => !c.has_children && pathOf(c).toLowerCase().includes(term))
    .slice(0, 30)
})

async function loadDetails(id: number) {
  const path = { orgID: orgId.value, shopID: shopId.value, categoryID: String(id) }
  try {
    const [a, b] = await Promise.all([
      api.GET('/orgs/{orgID}/shops/{shopID}/catalog/categories/{categoryID}/attributes', { params: { path } }),
      api.GET('/orgs/{orgID}/shops/{shopID}/catalog/categories/{categoryID}/brands', { params: { path } }),
    ])
    attributes.value = unwrap(a).data as Attribute[]
    const bd = unwrap(b).data
    brands.value = { list: bd.brand_list as Brand[], mandatory: bd.is_mandatory }
  } catch (e) {
    error.value = toApiError(e).message
  }
}

async function choose(c: Category) {
  emit('category', String(c.category_id), pathOf(c))
  q.value = ''
  await loadDetails(c.category_id)
}
const mandatory = computed(() => attributes.value.filter((a) => a.mandatory))
</script>

<template>
  <section class="space-y-3 rounded-md border border-dashed p-3" aria-labelledby="picker-heading">
    <h3 id="picker-heading" class="text-sm font-semibold">Tra cứu trên Shopee</h3>
    <p v-if="!shops.length" class="text-sm text-muted-foreground">Kết nối một shop để xem ngành hàng, thuộc tính và thương hiệu của shop đó.</p>
    <template v-else>
      <div class="grid gap-3 sm:grid-cols-[1fr_2fr]">
        <div class="space-y-1.5">
          <Label for="picker-shop">Tra cứu theo thị trường của shop</Label>
          <select id="picker-shop" v-model="shopId" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
            <option v-for="s in shops" :key="s.id" :value="s.id">{{ s.shop_name ?? `Shop ${s.shopee_shop_id}` }} ({{ s.region }})</option>
          </select>
        </div>
        <div class="space-y-1.5">
          <Label for="picker-q">Tìm ngành hàng</Label>
          <Input id="picker-q" v-model="q" type="search" :disabled="!categories.length" :placeholder="categories.length ? 'Nhập ít nhất 2 ký tự' : ''" />
        </div>
      </div>
      <Button v-if="!categories.length" type="button" variant="outline" size="sm" :disabled="loading" @click="loadCategories">
        {{ loading ? 'Đang tải…' : 'Tải danh sách ngành hàng' }}
      </Button>
      <FormError :message="error" />
      <ul v-if="matches.length" class="max-h-56 overflow-y-auto rounded-md border text-sm" aria-label="Ngành hàng phù hợp">
        <li v-for="c in matches" :key="c.category_id">
          <button type="button" class="w-full px-3 py-1.5 text-left hover:bg-muted focus-visible:bg-muted" @click="choose(c)">
            {{ pathOf(c) }} <span class="text-xs text-muted-foreground">({{ c.category_id }})</span>
          </button>
        </li>
      </ul>
      <p v-else-if="q.trim().length >= 2" class="text-sm text-muted-foreground">Không tìm thấy ngành hàng cấp cuối nào phù hợp.</p>

      <div v-if="mandatory.length" class="space-y-2">
        <p class="text-sm font-medium">Thuộc tính bắt buộc</p>
        <div v-for="a in mandatory" :key="a.attribute_id" class="grid items-center gap-2 sm:grid-cols-[1fr_2fr]">
          <Label :for="`pick-attr-${a.attribute_id}`">{{ a.name }}</Label>
          <select v-if="a.attribute_value_list?.length" :id="`pick-attr-${a.attribute_id}`" class="h-9 rounded-md border bg-background px-3 text-sm"
                  @change="emit('attribute', a.attribute_id, Number(($event.target as HTMLSelectElement).value), '')">
            <option value="">Chọn…</option>
            <option v-for="v in a.attribute_value_list" :key="v.value_id" :value="v.value_id">{{ v.name }}</option>
          </select>
          <Input v-else :id="`pick-attr-${a.attribute_id}`" placeholder="Giá trị tự điền"
                 @change="emit('attribute', a.attribute_id, 0, ($event.target as HTMLInputElement).value)" />
        </div>
      </div>
      <div v-if="brands.list.length || brands.mandatory" class="grid items-center gap-2 sm:grid-cols-[1fr_2fr]">
        <Label for="pick-brand">Thương hiệu{{ brands.mandatory ? ' (bắt buộc với ngành hàng này)' : '' }}</Label>
        <select id="pick-brand" class="h-9 rounded-md border bg-background px-3 text-sm" @change="emit('brand', ($event.target as HTMLSelectElement).value)">
          <option value="">No Brand</option>
          <option v-for="b in brands.list" :key="b.brand_id" :value="b.original_brand_name">{{ b.display_brand_name || b.original_brand_name }}</option>
        </select>
      </div>
    </template>
  </section>
</template>
