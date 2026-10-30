import type { components } from '~/types/api'

export type Product = components['schemas']['Product']
export type ProductInput = components['schemas']['ProductInput']
export type ImportBatch = components['schemas']['ImportBatch']
export type ImportRow = components['schemas']['ImportRow']
export type PublishState = components['schemas']['ProductMapping']['publish_state']

export const currencies = ['VND', 'SGD', 'MYR', 'THB', 'PHP', 'IDR', 'TWD'] as const

export const publishLabels: Record<PublishState, string> = {
  PLANNED: 'Planned',
  CREATING: 'Creating',
  CREATED: 'Created',
  VERIFIED: 'Published',
  FAILED: 'Failed',
  UNKNOWN: 'Checking',
}

export function publishVariant(s: PublishState): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (s === 'VERIFIED') return 'default'
  if (s === 'FAILED') return 'destructive'
  if (s === 'UNKNOWN') return 'outline'
  return 'secondary'
}

export const batchLabels: Record<ImportBatch['status'], string> = {
  UPLOADED: 'Queued',
  PARSING: 'Reading the file',
  IMPORTING: 'Importing',
  COMPLETED: 'Completed',
  FAILED: 'Failed',
}

export const rowLabels: Record<ImportRow['status'], string> = {
  PENDING: 'Pending',
  VALID: 'Valid',
  INVALID: 'Invalid',
  IMPORTED: 'Imported',
  FAILED: 'Failed',
}

export function isBatchActive(s: ImportBatch['status']): boolean {
  return s === 'UPLOADED' || s === 'PARSING' || s === 'IMPORTING'
}

export function formatMoney(m: { amount: string; currency: string }): string {
  const n = Number(m.amount)
  if (!Number.isFinite(n)) return `${m.amount} ${m.currency}`
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: m.currency }).format(n)
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
