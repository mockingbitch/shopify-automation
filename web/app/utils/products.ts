import type { components } from '~/types/api'

export type Product = components['schemas']['Product']
export type ProductInput = components['schemas']['ProductInput']
export type ImportBatch = components['schemas']['ImportBatch']
export type ImportRow = components['schemas']['ImportRow']
export type PublishState = components['schemas']['ProductMapping']['publish_state']

export const currencies = ['VND', 'SGD', 'MYR', 'THB', 'PHP', 'IDR', 'TWD'] as const

export const publishLabels: Record<PublishState, string> = {
  PLANNED: 'Chờ đăng',
  CREATING: 'Đang tạo',
  CREATED: 'Đã tạo',
  VERIFIED: 'Đã đăng',
  FAILED: 'Thất bại',
  UNKNOWN: 'Đang kiểm tra',
}

export function publishVariant(s: PublishState): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (s === 'VERIFIED') return 'default'
  if (s === 'FAILED') return 'destructive'
  if (s === 'UNKNOWN') return 'outline'
  return 'secondary'
}

export const batchLabels: Record<ImportBatch['status'], string> = {
  UPLOADED: 'Đang chờ xử lý',
  PARSING: 'Đang đọc file',
  IMPORTING: 'Đang nhập',
  COMPLETED: 'Hoàn tất',
  FAILED: 'Thất bại',
}

export const rowLabels: Record<ImportRow['status'], string> = {
  PENDING: 'Đang chờ',
  VALID: 'Hợp lệ',
  INVALID: 'Có lỗi',
  IMPORTED: 'Đã nhập',
  FAILED: 'Thất bại',
}

/** importColumnHelp explains each import-file column (the keys stay in English). */
export const importColumnHelp: Record<string, string> = {
  sku: 'Mã SKU',
  name: 'Tên sản phẩm',
  description: 'Mô tả',
  category: 'ID ngành hàng Shopee',
  brand: 'Thương hiệu',
  condition: 'Tình trạng: NEW (mới) hoặc USED (đã sử dụng)',
  price: 'Giá',
  currency: 'Tiền tệ, ví dụ VND',
  stock: 'Tồn kho',
  weight_kg: 'Cân nặng (kg)',
  length_cm: 'Chiều dài gói hàng (cm)',
  width_cm: 'Chiều rộng gói hàng (cm)',
  height_cm: 'Chiều cao gói hàng (cm)',
  days_to_ship: 'Số ngày chuẩn bị hàng',
  image_urls: 'Link ảnh (URL), cách nhau bằng dấu |',
  tier1_name: 'Tên nhóm phân loại 1, ví dụ Màu sắc',
  tier1_option: 'Lựa chọn của nhóm 1, ví dụ Đỏ',
  tier2_name: 'Tên nhóm phân loại 2, ví dụ Kích cỡ',
  tier2_option: 'Lựa chọn của nhóm 2, ví dụ M',
  variation_sku: 'SKU của phân loại hàng',
}

export function isBatchActive(s: ImportBatch['status']): boolean {
  return s === 'UPLOADED' || s === 'PARSING' || s === 'IMPORTING'
}

export function formatMoney(m: { amount: string; currency: string }): string {
  const n = Number(m.amount)
  if (!Number.isFinite(n)) return `${m.amount} ${m.currency}`
  return new Intl.NumberFormat('vi-VN', { style: 'currency', currency: m.currency }).format(n)
}

/** toInput turns a product into the body PUT expects. */
export function toInput(p: Product): ProductInput {
  return {
    sku: p.sku, name: p.name, description: p.description, shopee_category_id: p.shopee_category_id,
    category_label: p.category_label, brand_name: p.brand_name, condition: p.condition as ProductInput['condition'],
    attributes: p.attributes, price: p.price.amount, currency: p.price.currency, stock: p.stock, weight_kg: p.weight_kg,
    package_cm: p.package_cm, days_to_ship: p.days_to_ship, tiers: p.tiers,
    variations: p.variations.map((v) => ({ tier_index: v.tier_index, sku: v.sku, price: v.price.amount, stock: v.stock })),
  }
}
