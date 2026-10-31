# Vietnamese UI glossary

The product's user-facing language is Vietnamese (decision 2026-10-31). Code, identifiers, API field names, logs and engineering docs stay in English. Every user-visible string (dashboard, extension popup, API error messages, emails, notifications, template section labels) uses the terms below so that the same thing always has the same name. Prefer the words Shopee Vietnam uses in Kênh Người Bán, because sellers already know them.

## Style

- Address the user as **bạn**. Short, plain sentences; no jargon. Explain what happens and what to do next.
- Sentence case for buttons and headings ("Kết nối shop", not "Kết Nối Shop").
- Keep these as is: Shopee, SKU, CSV, Excel, API, email, OTP, CAPTCHA, ID, URL, VND.
- Numbers and dates use `vi-VN` formatting (`Intl.*('vi-VN', …)`), e.g. `31/10/2026 14:05`, `159.000 ₫`.
- Error messages say what is wrong and how to fix it: "Giá phải lớn hơn 0." not "Giá trị không hợp lệ."

## Terms

| English | Tiếng Việt |
|---|---|
| Organization | Tổ chức |
| Member / Invite | Thành viên / Mời |
| Owner / Admin / Operator / Viewer | Chủ sở hữu / Quản trị viên / Nhân viên vận hành / Người xem |
| Platform admin | Quản trị nền tảng |
| Settings | Cài đặt |
| Overview (dashboard home) | Tổng quan |
| Shop | Shop |
| Connect / Reconnect / Disconnect a shop | Kết nối shop / Kết nối lại / Ngắt kết nối |
| Authorization (Shopee consent) | Ủy quyền |
| Sync | Đồng bộ |
| Capability | Chức năng |
| Shopee app (partner app) | Ứng dụng Shopee |
| Shop template | Mẫu shop |
| Template version / Draft / Publish (a version) | Phiên bản / Bản nháp / Phát hành |
| Section (of a template) | Mục |
| Override (per-shop value) | Giá trị riêng cho shop |
| Preview / Apply | Xem trước / Áp dụng |
| Workflow (run) | Quy trình (lượt chạy) |
| Automation workflows | Quy trình tự động |
| Step | Bước |
| Approval / Approve / Reject | Duyệt / Duyệt / Từ chối |
| Waiting for approval / Waiting for you | Chờ duyệt / Chờ bạn xử lý |
| Manual task | Việc làm thủ công |
| Retry / Resume / Cancel | Thử lại / Tiếp tục / Hủy |
| Completed / Partially completed / Failed | Hoàn tất / Hoàn tất một phần / Thất bại |
| Execution logs | Nhật ký thực thi |
| Audit log | Nhật ký hoạt động |
| Notification | Thông báo |
| Product | Sản phẩm |
| Category (Shopee) | Ngành hàng |
| Attribute / Brand | Thuộc tính / Thương hiệu |
| Variation tier / Variation (option combination) | Nhóm phân loại / Phân loại hàng |
| Stock / Price / Weight / Package size | Tồn kho / Giá / Cân nặng / Kích thước đóng gói |
| Shipping channel | Đơn vị vận chuyển |
| Pickup / Return address | Địa chỉ lấy hàng / Địa chỉ trả hàng |
| Warehouse | Kho hàng |
| Shop banner / Shop decoration | Banner shop / Trang trí shop |
| Import (products) / Import batch / Row | Nhập sản phẩm / Lô nhập / Dòng |
| Publish products (to a shop) | Đăng sản phẩm |
| Unlisted (item status) | Ẩn (chưa hiển thị) |
| Seller Centre | Kênh Người Bán |
| Browser extension | Tiện ích trình duyệt |
| Pair / Pairing code / Device | Ghép nối / Mã ghép nối / Trình duyệt đã ghép nối |
| Recipe (browser steps) | Kịch bản thao tác |
| Two-step verification / Authenticator app / Recovery code | Xác minh hai bước / Ứng dụng xác thực / Mã khôi phục |
| Sign in / Sign out / Create account | Đăng nhập / Đăng xuất / Tạo tài khoản |
| Kill switch | Công tắc tắt khẩn cấp |
