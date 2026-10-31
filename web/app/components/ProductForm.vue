<script setup lang="ts">
import { currencies, type ProductInput } from '~/utils/products'

// Product editor. Variations are generated from the tiers (every option
// combination); values typed for a combination are kept when tiers change.
const props = defineProps<{ initial?: ProductInput; errors?: Record<string, string>; busy?: boolean; submitLabel: string; readonly?: boolean }>()
const emit = defineEmits<{ submit: [input: ProductInput] }>()

const i = props.initial
const f = reactive({
  sku: i?.sku ?? '', name: i?.name ?? '', description: i?.description ?? '',
  category: i?.shopee_category_id ?? '', categoryLabel: i?.category_label ?? '', brand: i?.brand_name ?? '',
  condition: i?.condition ?? '', price: i?.price ?? '', currency: i?.currency ?? 'VND', stock: i?.stock ?? 0,
  weight: i?.weight_kg ?? '', length: i?.package_cm?.length as number | undefined, width: i?.package_cm?.width as number | undefined,
  height: i?.package_cm?.height as number | undefined, days: (i?.days_to_ship ?? undefined) as number | undefined,
})
const attributes = ref((i?.attributes ?? []).map((a) => ({ id: String(a.attribute_id), value: String(a.value_id), name: a.original_value_name ?? '', unit: a.value_unit ?? '' })))
const tiers = ref((i?.tiers ?? []).map((t) => ({ name: t.name, options: t.options.join(', ') })))
type Row = { sku: string; price: string; stock: number }
const rows = reactive<Record<string, Row>>({})
for (const v of i?.variations ?? []) rows[v.tier_index.join('.')] = { sku: v.sku ?? '', price: v.price, stock: v.stock }

const parsedTiers = computed(() => tiers.value.map((t) => ({ name: t.name.trim(), options: t.options.split(',').map((o) => o.trim()).filter(Boolean) })))
const combos = computed(() => {
  const ts = parsedTiers.value.filter((t) => t.options.length)
  if (!ts.length || ts.length !== parsedTiers.value.length) return []
  let out: { index: number[]; label: string }[] = [{ index: [], label: '' }]
  for (const t of ts) {
    out = out.flatMap((c) => t.options.map((o, k) => ({ index: [...c.index, k], label: c.label ? `${c.label} / ${o}` : o })))
  }
  return out
})
function row(index: number[]): Row {
  const key = index.join('.')
  if (!rows[key]) rows[key] = { sku: '', price: f.price, stock: 0 }
  return rows[key]!
}

function pickCategory(id: string, label: string) {
  f.category = id
  if (!f.categoryLabel) f.categoryLabel = label
}
function pickAttribute(attributeId: number, valueId: number, name: string) {
  const row = { id: String(attributeId), value: String(valueId), name, unit: '' }
  const i = attributes.value.findIndex((a) => a.id === row.id)
  if (i >= 0) attributes.value[i] = row
  else attributes.value.push(row)
}

const err = (k: string) => props.errors?.[k]
const variationError = computed(() => Object.entries(props.errors ?? {}).filter(([k]) => k.startsWith('variations') || k.startsWith('tiers')).map(([, v]) => v).join(' '))
const attributeError = computed(() => Object.entries(props.errors ?? {}).filter(([k]) => k.startsWith('attributes')).map(([, v]) => v).join(' '))

function submit() {
  const num = (v: number | string | undefined) => (v === undefined || v === '' || Number.isNaN(Number(v)) ? null : Number(v))
  const pkg = num(f.length) && num(f.width) && num(f.height) ? { length: num(f.length)!, width: num(f.width)!, height: num(f.height)! } : null
  const ts = parsedTiers.value.filter((t) => t.name)
  emit('submit', {
    sku: f.sku.trim(), name: f.name.trim(), description: f.description, shopee_category_id: f.category.trim() || null,
    category_label: f.categoryLabel.trim() || null, brand_name: f.brand.trim() || null,
    condition: (f.condition || null) as ProductInput['condition'], price: String(f.price).trim(), currency: f.currency,
    stock: Number(f.stock), weight_kg: String(f.weight).trim() || null, package_cm: pkg, days_to_ship: num(f.days),
    attributes: attributes.value.filter((a) => a.id).map((a) => ({
      attribute_id: Number(a.id), value_id: Number(a.value || 0),
      ...(a.name ? { original_value_name: a.name } : {}), ...(a.unit ? { value_unit: a.unit } : {}),
    })),
    tiers: ts,
    variations: ts.length ? combos.value.map((c) => {
      const r = row(c.index)
      return { tier_index: c.index, sku: r.sku.trim() || null, price: String(r.price).trim(), stock: Number(r.stock) }
    }) : [],
  })
}
const inputClass = 'w-full rounded-md border bg-background px-3 py-2 text-sm'
</script>

<template>
  <form class="space-y-6" novalidate @submit.prevent="submit">
    <fieldset :disabled="readonly" class="space-y-6">
      <section class="grid gap-4 rounded-lg border bg-background p-4 sm:grid-cols-2" aria-labelledby="pf-basic">
        <h2 id="pf-basic" class="text-base font-semibold sm:col-span-2">Thông tin cơ bản</h2>
        <FormField id="pf-sku" label="SKU" :error="err('sku')" hint="Không được trùng trong tổ chức của bạn. Dùng để nhận ra sản phẩm trên Shopee.">
          <Input id="pf-sku" v-model="f.sku" required maxlength="100" />
        </FormField>
        <FormField id="pf-name" label="Tên sản phẩm" :error="err('name')"><Input id="pf-name" v-model="f.name" required maxlength="255" /></FormField>
        <div class="sm:col-span-2">
          <FormField id="pf-desc" label="Mô tả sản phẩm" :error="err('description')">
            <textarea id="pf-desc" v-model="f.description" rows="5" maxlength="10000" :class="inputClass" />
          </FormField>
        </div>
        <div class="sm:col-span-2">
          <CatalogPicker v-if="!readonly" :category-id="f.category" @category="pickCategory" @attribute="pickAttribute" @brand="(b) => (f.brand = b)" />
        </div>
        <FormField id="pf-cat" label="ID ngành hàng Shopee" :error="err('shopee_category_id')" hint="Ngành hàng cấp cuối cùng, theo thị trường của shop.">
          <Input id="pf-cat" v-model="f.category" inputmode="numeric" />
        </FormField>
        <FormField id="pf-catl" label="Tên ngành hàng (không bắt buộc)" :error="err('category_label')"><Input id="pf-catl" v-model="f.categoryLabel" /></FormField>
        <FormField id="pf-brand" label="Thương hiệu" :error="err('brand_name')" hint="Để trống nếu sản phẩm không có thương hiệu (No Brand)."><Input id="pf-brand" v-model="f.brand" /></FormField>
        <FormField id="pf-cond" label="Tình trạng" :error="err('condition')">
          <select id="pf-cond" v-model="f.condition" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
            <option value="">Chưa chọn</option><option value="NEW">Mới</option><option value="USED">Đã sử dụng</option>
          </select>
        </FormField>
      </section>

      <section class="grid gap-4 rounded-lg border bg-background p-4 sm:grid-cols-3" aria-labelledby="pf-sale">
        <h2 id="pf-sale" class="text-base font-semibold sm:col-span-3">Giá, tồn kho và vận chuyển</h2>
        <FormField id="pf-price" label="Giá" :error="err('price')"><Input id="pf-price" v-model="f.price" inputmode="decimal" required /></FormField>
        <FormField id="pf-cur" label="Tiền tệ" :error="err('currency')" hint="Phải khớp với thị trường của shop.">
          <select id="pf-cur" v-model="f.currency" class="h-9 w-full rounded-md border bg-background px-3 text-sm">
            <option v-for="c in currencies" :key="c" :value="c">{{ c }}</option>
          </select>
        </FormField>
        <FormField id="pf-stock" label="Tồn kho" :error="err('stock')"><Input id="pf-stock" v-model.number="f.stock" type="number" min="0" /></FormField>
        <FormField id="pf-weight" label="Cân nặng (kg)" :error="err('weight_kg')"><Input id="pf-weight" v-model="f.weight" inputmode="decimal" /></FormField>
        <FormField id="pf-days" label="Số ngày chuẩn bị hàng (không bắt buộc)" :error="err('days_to_ship')"><Input id="pf-days" v-model.number="f.days" type="number" min="1" /></FormField>
        <div />
        <FormField id="pf-len" label="Chiều dài gói hàng (cm)" :error="err('package_cm')"><Input id="pf-len" v-model.number="f.length" type="number" min="1" /></FormField>
        <FormField id="pf-wid" label="Chiều rộng gói hàng (cm)"><Input id="pf-wid" v-model.number="f.width" type="number" min="1" /></FormField>
        <FormField id="pf-hei" label="Chiều cao gói hàng (cm)"><Input id="pf-hei" v-model.number="f.height" type="number" min="1" /></FormField>
      </section>

      <section class="space-y-3 rounded-lg border bg-background p-4" aria-labelledby="pf-attr">
        <div class="flex items-center justify-between">
          <h2 id="pf-attr" class="text-base font-semibold">Thuộc tính Shopee</h2>
          <Button type="button" variant="outline" size="sm" @click="attributes.push({ id: '', value: '', name: '', unit: '' })">Thêm thuộc tính</Button>
        </div>
        <p class="text-sm text-muted-foreground">Thuộc tính bắt buộc tùy theo ngành hàng. Khi đăng sản phẩm, hệ thống sẽ báo các thuộc tính còn thiếu.</p>
        <p v-if="attributeError" class="text-xs text-destructive">{{ attributeError }}</p>
        <div v-for="(a, k) in attributes" :key="k" class="grid gap-2 sm:grid-cols-[1fr_1fr_2fr_1fr_auto] sm:items-end">
          <FormField :id="`pf-a${k}`" label="ID thuộc tính"><Input :id="`pf-a${k}`" v-model="a.id" inputmode="numeric" /></FormField>
          <FormField :id="`pf-av${k}`" label="ID giá trị" hint="Nhập 0 nếu bạn tự điền giá trị"><Input :id="`pf-av${k}`" v-model="a.value" inputmode="numeric" /></FormField>
          <FormField :id="`pf-an${k}`" label="Giá trị tự điền"><Input :id="`pf-an${k}`" v-model="a.name" /></FormField>
          <FormField :id="`pf-au${k}`" label="Đơn vị"><Input :id="`pf-au${k}`" v-model="a.unit" /></FormField>
          <Button type="button" variant="outline" size="sm" :aria-label="`Xóa thuộc tính ${k + 1}`" @click="attributes.splice(k, 1)">Xóa</Button>
        </div>
      </section>

      <section class="space-y-3 rounded-lg border bg-background p-4" aria-labelledby="pf-var">
        <div class="flex items-center justify-between">
          <h2 id="pf-var" class="text-base font-semibold">Phân loại hàng</h2>
          <Button v-if="tiers.length < 2" type="button" variant="outline" size="sm" @click="tiers.push({ name: '', options: '' })">Thêm nhóm phân loại</Button>
        </div>
        <p class="text-sm text-muted-foreground">
          Shopee chỉ chấp nhận tên phân loại chuẩn của ngành hàng (ví dụ "Màu sắc" hoặc "Kích cỡ") và tên lựa chọn đúng theo danh sách của Shopee.
        </p>
        <p v-if="variationError" class="text-xs text-destructive">{{ variationError }}</p>
        <div v-for="(t, k) in tiers" :key="k" class="grid gap-2 sm:grid-cols-[1fr_3fr_auto] sm:items-end">
          <FormField :id="`pf-t${k}`" :label="`Tên nhóm phân loại ${k + 1}`"><Input :id="`pf-t${k}`" v-model="t.name" maxlength="50" /></FormField>
          <FormField :id="`pf-to${k}`" label="Các lựa chọn (cách nhau bằng dấu phẩy)"><Input :id="`pf-to${k}`" v-model="t.options" /></FormField>
          <Button type="button" variant="outline" size="sm" :aria-label="`Xóa nhóm phân loại ${k + 1}`" @click="tiers.splice(k, 1)">Xóa</Button>
        </div>
        <div v-if="combos.length" class="overflow-x-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow><TableHead>Phân loại hàng</TableHead><TableHead>SKU</TableHead><TableHead>Giá</TableHead><TableHead>Tồn kho</TableHead></TableRow>
            </TableHeader>
            <TableBody>
              <TableRow v-for="c in combos" :key="c.index.join('.')">
                <TableCell>{{ c.label }}</TableCell>
                <TableCell><Input v-model="row(c.index).sku" :aria-label="`SKU của ${c.label}`" /></TableCell>
                <TableCell><Input v-model="row(c.index).price" inputmode="decimal" :aria-label="`Giá của ${c.label}`" /></TableCell>
                <TableCell><Input v-model.number="row(c.index).stock" type="number" min="0" :aria-label="`Tồn kho của ${c.label}`" /></TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </section>
    </fieldset>
    <div v-if="!readonly" class="flex justify-end gap-2">
      <slot name="actions" />
      <Button type="submit" :disabled="busy">{{ submitLabel }}</Button>
    </div>
  </form>
</template>
