import type { components } from '~/types/api'

export type RunStatus = components['schemas']['RunStatus']
export type RunSummary = components['schemas']['RunSummary']
export type WorkflowStep = components['schemas']['WorkflowStep']
export type RunView = components['schemas']['RunView']
export type WorkflowEvent = components['schemas']['WorkflowEvent']

export const runStatusLabels: Record<RunStatus, string> = {
  PENDING: 'Queued',
  RUNNING: 'Running',
  WAITING_APPROVAL: 'Waiting for approval',
  WAITING_USER: 'Waiting for you',
  COMPLETED: 'Completed',
  PARTIALLY_COMPLETED: 'Partially completed',
  FAILED: 'Failed',
  CANCELLED: 'Cancelled',
}

export function runVariant(s: RunStatus): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (s === 'COMPLETED') return 'default'
  if (s === 'FAILED') return 'destructive'
  if (s === 'WAITING_APPROVAL' || s === 'WAITING_USER' || s === 'PARTIALLY_COMPLETED') return 'outline'
  return 'secondary'
}

export function isTerminal(s: RunStatus): boolean {
  return s === 'COMPLETED' || s === 'PARTIALLY_COMPLETED' || s === 'FAILED' || s === 'CANCELLED'
}

export const stepLabels: Record<string, string> = {
  validate_connection: 'Check the Shopee connection',
  fetch_current_config: 'Read the current shop configuration',
  validate_template: 'Validate the template for this shop',
  generate_diff: 'Compare template and shop',
  approve_changes: 'Approve changes',
  apply_profile: 'Update shop name and description',
  apply_branding: 'Manual tasks (branding, warehouse, addresses)',
  apply_logistics: 'Update shipping channels and addresses',
  verify_configuration: 'Verify the result',
  generate_report: 'Write the report',
  prepare_products: 'Check products against the shop',
  approve_products: 'Approve products',
  create_products: 'Create products on Shopee',
}

export const skipReasons: Record<string, string> = {
  nothing_to_change: 'The shop already matches the template.',
  not_selected: 'No change of this kind was approved.',
  no_manual_changes: 'No manual work needed.',
  nothing_to_publish: 'No product was ready to publish.',
}

export interface WaitOutput {
  reason?: string
  message?: string
  tasks?: { key: string; title: string; instructions: string; capability?: string; data?: unknown }[]
  completed_by?: string
  note?: string
}
