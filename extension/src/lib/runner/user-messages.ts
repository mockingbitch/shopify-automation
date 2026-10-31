/**
 * Vietnamese text the seller sees in the popup for a finished run. The
 * technical English message still goes to the API (logs); the popup shows
 * what happened and what to do next, keyed by the machine error code.
 */

const RECIPE_BROKEN = 'Kịch bản thao tác của việc này bị lỗi hoặc không khớp với shop, nên Tiện ích không chạy. Hãy báo quản trị viên của bạn.';
const PAGE_CHANGED =
  'Không tìm thấy hoặc không thao tác được một mục trên trang Kênh Người Bán (có thể Shopee vừa đổi giao diện). Hãy kiểm tra tab, rồi bấm Chạy lại hoặc làm thủ công.';

const FAILURE_MESSAGES: Record<string, string> = {
  signature_invalid: 'Kịch bản thao tác không qua được bước kiểm tra an toàn, nên Tiện ích không chạy và chưa thay đổi gì trên shop. Hãy báo quản trị viên của bạn.',
  unknown_key: 'Kịch bản thao tác không qua được bước kiểm tra an toàn, nên Tiện ích không chạy và chưa thay đổi gì trên shop. Hãy cập nhật Tiện ích trình duyệt hoặc báo quản trị viên.',
  recipe_invalid: RECIPE_BROKEN,
  recipe_mismatch: RECIPE_BROKEN,
  origin_not_permitted: 'Phiên bản Tiện ích trình duyệt này không được phép mở trang mà việc này cần. Hãy cập nhật Tiện ích hoặc báo quản trị viên.',
  origin_not_allowed: 'Việc này định mở một trang ngoài Kênh Người Bán, nên Tiện ích đã dừng để an toàn. Hãy báo quản trị viên của bạn.',
  shop_not_asserted: 'Tiện ích đã dừng vì chưa kiểm tra được đúng shop trước khi thay đổi. Hãy báo quản trị viên của bạn.',
  missing_input: 'Việc này thiếu dữ liệu cần thiết (ví dụ ảnh hoặc nội dung). Hãy tạo lại việc trên trang quản lý.',
  input_fetch_failed: 'Không tải được tệp cho việc này. Hãy kiểm tra mạng, rồi bấm Chạy lại.',
  input_checksum_mismatch: 'Tệp tải về không giống tệp gốc nên Tiện ích không dùng. Hãy tải tệp lên lại trên trang quản lý.',
  target_not_found: PAGE_CHANGED,
  ambiguous_target: PAGE_CHANGED,
  invalid_selector: PAGE_CHANGED,
  target_disabled: PAGE_CHANGED,
  unsupported_target: PAGE_CHANGED,
  option_not_found: PAGE_CHANGED,
  too_many_files: PAGE_CHANGED,
  expect_failed: 'Tiện ích đã thao tác xong nhưng không thấy Kênh Người Bán xác nhận thay đổi. Hãy kiểm tra lại trong tab trước khi chạy lại.',
  shop_id_not_found: 'Không đọc được ID shop trên trang. Hãy mở trang chủ Kênh Người Bán của đúng shop trong tab này, rồi bấm Chạy lại.',
  tab_closed: 'Tab Kênh Người Bán đã bị đóng. Hãy mở lại tab, rồi bấm Chạy lại.',
  navigation_interrupted: 'Trang bị tải lại hoặc phản hồi quá lâu nên việc bị gián đoạn. Hãy kiểm tra tab, rồi bấm Chạy lại.',
  network_error: 'Mất kết nối tới máy chủ. Hãy kiểm tra mạng, rồi bấm Chạy lại.',
};

const FALLBACK = 'Có lỗi khi chạy việc này. Hãy kiểm tra tab, rồi bấm Chạy lại hoặc làm thủ công.';

/** Codes whose own message is already written in Vietnamese for the seller. */
const SELF_EXPLAINING = new Set(['shop_mismatch']);

export function failureMessage(code: string, detail: string): string {
  if (SELF_EXPLAINING.has(code) && detail) return detail;
  return FAILURE_MESSAGES[code] ?? FALLBACK;
}

export const STOPPED_MESSAGE = 'Bạn đã dừng việc này. Việc được trả lại danh sách để chạy sau.';
export const TAKEN_OVER_MESSAGE = 'Việc này đã được một trình duyệt khác nhận chạy. Tiện ích đã dừng và không thao tác thêm.';

const ACTION_LABELS: Record<string, string> = {
  goto: 'mở trang',
  click: 'bấm',
  fill: 'điền',
  select: 'chọn',
  upload: 'tải tệp lên',
  wait_for: 'chờ trang',
  expect: 'kiểm tra kết quả',
  assert_shop_id: 'kiểm tra ID shop',
  screenshot: 'chụp màn hình',
};

export function stepMessage(index: number, count: number, action: string): string {
  return `Bước ${index + 1}/${count}: ${ACTION_LABELS[action] ?? action}`;
}
