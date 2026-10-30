import type { components } from '~/types/api'

export type Shop = components['schemas']['Shop']
export type ConnectionStatus = components['schemas']['ConnectionStatus']
export type Capability = components['schemas']['Capability']

export const statusLabels: Record<ConnectionStatus, string> = {
  PENDING: 'Pending',
  ACTIVE: 'Connected',
  RECONNECT_REQUIRED: 'Reconnect required',
  REVOKED: 'Revoked by seller',
  EXPIRED: 'Authorization expired',
  DISCONNECTED: 'Disconnected',
}

export function statusVariant(s: ConnectionStatus): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (s === 'ACTIVE') return 'default'
  if (s === 'RECONNECT_REQUIRED' || s === 'REVOKED' || s === 'EXPIRED') return 'destructive'
  return 'secondary'
}

/** Statuses that a new authorization can fix. */
export function needsReconnect(s: ConnectionStatus): boolean {
  return s === 'RECONNECT_REQUIRED' || s === 'REVOKED' || s === 'EXPIRED' || s === 'DISCONNECTED'
}

export const modeLabels: Record<Capability['mode'], string> = {
  API: 'Shopee API',
  BROWSER: 'Browser extension',
  MANUAL: 'Manual task',
  UNSUPPORTED: 'Not supported',
}

export const reasonText: Record<string, string> = {
  not_supported: 'Not supported in this version.',
  disabled_by_platform: 'Turned off by the platform administrator.',
  region_not_supported: 'Not available in this shop’s market.',
  app_type_not_allowed: 'The Shopee app type does not allow this.',
  shop_not_active: 'Reconnect the shop to use this.',
  not_sandbox_verified: 'Waiting for verification in the Shopee sandbox.',
  browser_extension_unavailable: 'No Open API exists. Until the browser extension ships, this becomes a manual task.',
}

/** Messages for ?connect_error=<code> after the Shopee redirect. */
export const connectErrors: Record<string, string> = {
  invalid_state: 'The connection link is not valid. Start again from the Shops page.',
  state_expired: 'The connection link expired after 10 minutes. Start again.',
  state_used: 'This connection link was already used. Start again if the shop is not connected.',
  authorization_denied: 'Shopee did not return an authorization. The seller may have cancelled.',
  token_exchange_failed: 'Shopee rejected the authorization. Try again in a few minutes.',
  shop_connected_elsewhere: 'This Shopee shop is already connected to another organization. Disconnect it there first.',
  shop_mismatch: 'You authorized a different shop than the one being reconnected. Sign in to Shopee with the right shop.',
  shopee_app_disabled: 'The Shopee app is disabled. Contact the platform administrator.',
  permission_denied: 'You no longer have permission to connect shops in this organization.',
  internal_error: 'Something went wrong while connecting. Try again.',
}

/** Days until the authorization expires (negative when past), or null. */
export function daysUntil(iso: string | null | undefined): number | null {
  if (!iso) return null
  return Math.floor((new Date(iso).getTime() - Date.now()) / 86_400_000)
}
