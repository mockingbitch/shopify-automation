import type { components } from '~/types/api'

export type Shop = components['schemas']['Shop']
export type ConnectionStatus = components['schemas']['ConnectionStatus']
export type Capability = components['schemas']['Capability']

export const statusLabels: Record<ConnectionStatus, string> = {
  PENDING: 'Đang chờ',
  ACTIVE: 'Đã kết nối',
  RECONNECT_REQUIRED: 'Cần kết nối lại',
  REVOKED: 'Người bán đã thu hồi',
  EXPIRED: 'Ủy quyền đã hết hạn',
  DISCONNECTED: 'Đã ngắt kết nối',
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
  BROWSER: 'Tiện ích trình duyệt',
  MANUAL: 'Việc làm thủ công',
  UNSUPPORTED: 'Chưa hỗ trợ',
}

export const reasonText: Record<string, string> = {
  not_supported: 'Phiên bản này chưa hỗ trợ.',
  disabled_by_platform: 'Quản trị nền tảng đã tắt chức năng này.',
  region_not_supported: 'Chưa có ở thị trường của shop này.',
  app_type_not_allowed: 'Loại ứng dụng Shopee đang dùng không cho phép việc này.',
  shop_not_active: 'Hãy kết nối lại shop để dùng chức năng này.',
  not_sandbox_verified: 'Đang chờ kiểm chứng trên môi trường sandbox của Shopee.',
  browser_extension_unavailable: 'Shopee chưa có API cho việc này. Hãy ghép nối tiện ích trình duyệt (trong Cài đặt) để làm tự động; trước đó, đây là việc làm thủ công.',
  browser_recipe_unavailable: 'Shopee chưa có API cho việc này và chưa có kịch bản thao tác đã kiểm chứng cho thị trường này, nên bạn cần làm thủ công.',
}

/** Messages for ?connect_error=<code> after the Shopee redirect. */
export const connectErrors: Record<string, string> = {
  invalid_state: 'Liên kết kết nối không hợp lệ. Hãy bắt đầu lại từ trang Shop.',
  state_expired: 'Liên kết kết nối đã hết hạn sau 10 phút. Hãy bắt đầu lại.',
  state_used: 'Liên kết kết nối này đã được dùng. Nếu shop chưa được kết nối, hãy bắt đầu lại.',
  authorization_denied: 'Shopee không trả về ủy quyền. Có thể người bán đã hủy.',
  token_exchange_failed: 'Shopee đã từ chối ủy quyền. Hãy thử lại sau vài phút.',
  shop_connected_elsewhere: 'Shop Shopee này đã được kết nối với một tổ chức khác. Hãy ngắt kết nối ở tổ chức đó trước.',
  shop_mismatch: 'Bạn đã ủy quyền cho một shop khác với shop đang kết nối lại. Hãy đăng nhập Shopee bằng đúng shop.',
  shopee_app_disabled: 'Ứng dụng Shopee đang bị tắt. Hãy liên hệ quản trị nền tảng.',
  permission_denied: 'Bạn không còn quyền kết nối shop trong tổ chức này.',
  internal_error: 'Đã xảy ra lỗi khi kết nối. Hãy thử lại.',
}

/** Days until the authorization expires (negative when past), or null. */
export function daysUntil(iso: string | null | undefined): number | null {
  if (!iso) return null
  return Math.floor((new Date(iso).getTime() - Date.now()) / 86_400_000)
}
