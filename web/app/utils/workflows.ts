import type { components } from '~/types/api'

export type RunStatus = components['schemas']['RunStatus']
export type RunSummary = components['schemas']['RunSummary']
export type WorkflowStep = components['schemas']['WorkflowStep']
export type RunView = components['schemas']['RunView']
export type WorkflowEvent = components['schemas']['WorkflowEvent']

export const runStatusLabels: Record<RunStatus, string> = {
  PENDING: 'Đang xếp hàng',
  RUNNING: 'Đang chạy',
  WAITING_APPROVAL: 'Chờ duyệt',
  WAITING_USER: 'Chờ bạn xử lý',
  COMPLETED: 'Hoàn tất',
  PARTIALLY_COMPLETED: 'Hoàn tất một phần',
  FAILED: 'Thất bại',
  CANCELLED: 'Đã hủy',
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
  validate_connection: 'Kiểm tra kết nối Shopee',
  fetch_current_config: 'Đọc cấu hình hiện tại của shop',
  validate_template: 'Kiểm tra mẫu shop cho shop này',
  generate_diff: 'So sánh mẫu shop với shop',
  approve_changes: 'Duyệt thay đổi',
  apply_profile: 'Cập nhật tên và mô tả shop',
  apply_branding: 'Việc làm thủ công (trang trí shop, kho hàng, địa chỉ)',
  apply_logistics: 'Cập nhật đơn vị vận chuyển và địa chỉ',
  verify_configuration: 'Kiểm tra kết quả',
  generate_report: 'Lập báo cáo',
  prepare_products: 'Kiểm tra sản phẩm với shop',
  approve_products: 'Duyệt sản phẩm',
  create_products: 'Tạo sản phẩm trên Shopee',
}

export const skipReasons: Record<string, string> = {
  nothing_to_change: 'Shop đã khớp với mẫu shop.',
  not_selected: 'Không có thay đổi loại này được duyệt.',
  no_manual_changes: 'Không cần làm thủ công.',
  nothing_to_publish: 'Không có sản phẩm nào sẵn sàng để đăng.',
}

export const stepStatusLabels: Record<WorkflowStep['status'], string> = {
  PENDING: 'Chưa chạy',
  RUNNING: 'Đang chạy',
  SUCCEEDED: 'Xong',
  FAILED: 'Thất bại',
  SKIPPED: 'Bỏ qua',
  CANCELLED: 'Đã hủy',
  WAITING_APPROVAL: 'Chờ duyệt',
  WAITING_USER: 'Chờ bạn xử lý',
}

/** Text for the reason a step waits for a person (server wait codes). */
export const waitReasons: Record<string, string> = {
  manual_tasks: 'Có việc làm thủ công đang chờ bạn.',
  reconnect_required: 'Shopee không còn chấp nhận ủy quyền của shop này. Hãy kết nối lại shop rồi bấm Tiếp tục.',
  shop_not_normal: 'Shop đang không hoạt động bình thường trên Shopee. Hãy xử lý trong Kênh Người Bán rồi bấm Tiếp tục.',
  duplicate_items: 'Shopee có nhiều sản phẩm trùng SKU. Hãy giữ lại một sản phẩm trong Kênh Người Bán rồi bấm Tiếp tục.',
  ambiguous: 'Chưa rõ thao tác trên Shopee đã thành công hay chưa. Hãy kiểm tra shop trước khi bấm Tiếp tục.',
}

export const eventTypeLabels: Record<string, string> = {
  RUN_CREATED: 'Đã tạo lượt chạy',
  RUN_STATUS_CHANGED: 'Trạng thái lượt chạy thay đổi',
  STEP_ATTEMPT_STARTED: 'Bắt đầu một bước',
  STEP_ATTEMPT_FINISHED: 'Một bước đã chạy xong',
  STEP_STATUS_CHANGED: 'Trạng thái bước thay đổi',
  APPROVAL_REQUESTED: 'Đang chờ duyệt',
  APPROVAL_DECIDED: 'Đã có quyết định duyệt',
  CANCEL_REQUESTED: 'Đã yêu cầu hủy',
  USER_ACTION_COMPLETED: 'Bạn đã đánh dấu việc thủ công là xong',
  LOG: 'Ghi chú',
}

export interface WaitOutput {
  reason?: string
  message?: string
  tasks?: { key: string; title: string; instructions: string; capability?: string; data?: unknown; browser_task_id?: string }[]
  completed_by?: string
  note?: string
}
