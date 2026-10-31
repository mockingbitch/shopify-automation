/// <reference types="node" />
import { expect, test } from '@playwright/test'
import { latestLink, totp } from './helpers'

const onePixelPNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

test('sign up, create an organization and invite a member', async ({ page, request }) => {
  const stamp = Date.now()
  test.setTimeout(150_000) // includes waiting for a fresh 30-second code
  const email = `owner-${stamp}@example.com`
  const password = 'correct horse battery staple'

  await page.goto('/register')
  await page.getByLabel('Tên của bạn').fill('Owner')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Mật khẩu').fill(password)
  await page.getByRole('button', { name: 'Tạo tài khoản' }).click()
  await expect(page.getByText('và bấm vào liên kết để xác minh địa chỉ email')).toBeVisible()

  await page.goto(await latestLink(request, email, '/verify-email'))
  await expect(page.getByText('Địa chỉ email của bạn đã được xác minh.')).toBeVisible()

  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Mật khẩu').fill(password)
  await page.getByRole('button', { name: 'Đăng nhập' }).click()
  await expect(page).toHaveURL(/\/orgs\/new$/)

  await page.getByLabel('Tên', { exact: true }).fill(`Acme ${stamp}`)
  await expect(page.getByLabel('Tên trong URL')).toHaveValue(`acme-${stamp}`)
  await page.getByRole('button', { name: 'Tạo tổ chức' }).click()
  await expect(page.getByRole('heading', { level: 1, name: `Acme ${stamp}` })).toBeVisible()

  await page.getByRole('navigation', { name: 'Điều hướng chính' }).getByRole('link', { name: 'Thành viên' }).click()
  await page.getByLabel('Email').fill(`invitee-${stamp}@example.com`)
  await page.getByRole('button', { name: 'Gửi lời mời' }).click()
  await expect(page.getByText(`Đã gửi lời mời tới invitee-${stamp}@example.com.`)).toBeVisible()
  await expect(page.getByRole('cell', { name: `invitee-${stamp}@example.com` })).toBeVisible()

  await page.getByRole('navigation', { name: 'Điều hướng chính' }).getByRole('link', { name: 'Shop', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Chưa có shop nào' })).toBeVisible()
  // Connecting leaves for Shopee (or explains that no Shopee app is set up);
  // the connect flow itself is covered by the Go integration tests.
  await expect(page.getByRole('button', { name: 'Kết nối shop' }).first()).toBeEnabled()
  // Errors from the Shopee redirect are shown on the shop list.
  await page.goto(`${new URL(page.url()).pathname}?connect_error=state_expired`)
  await expect(page.getByRole('alert').filter({ hasText: 'đã hết hạn sau 10 phút' })).toBeVisible()

  // Templates: create, edit a section, validate, publish.
  await page.getByRole('navigation', { name: 'Điều hướng chính' }).getByRole('link', { name: 'Mẫu shop' }).click()
  await page.getByLabel('Tên mẫu shop mới').fill('Vietnam default')
  await page.getByRole('button', { name: 'Tạo mẫu shop' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Vietnam default' })).toBeVisible()
  await page.getByLabel('Thị trường').selectOption('VN')
  await page.getByLabel('Tên shop').fill('Cửa hàng Minh An')
  await page.getByRole('button', { name: 'Lưu mục' }).click()
  await expect(page.getByText('Đã lưu mục.')).toBeVisible()
  await page.getByRole('navigation', { name: 'Các mục' }).getByRole('button', { name: /Liên hệ/ }).click()
  await page.getByLabel('Số điện thoại').fill('0901234567')
  await page.getByRole('button', { name: 'Lưu mục' }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Không đúng định dạng yêu cầu' })).toBeVisible()
  await page.getByLabel('Số điện thoại').fill('+84901234567')
  await page.getByRole('button', { name: 'Lưu mục' }).click()
  await expect(page.getByText('Đã lưu mục.')).toBeVisible()
  await page.getByRole('button', { name: 'Kiểm tra' }).click()
  await expect(page.getByText('Sẵn sàng phát hành')).toBeVisible()
  await page.getByRole('button', { name: 'Phát hành' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Phát hành' }).click()
  await expect(page.getByText('đã được phát hành và không thể thay đổi nữa')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Lưu mục' })).toHaveCount(0)

  // Products: create one and add an image.
  const nav = page.getByRole('navigation', { name: 'Điều hướng chính' })
  await nav.getByRole('link', { name: 'Sản phẩm', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Chưa có sản phẩm' })).toBeVisible()
  await page.getByRole('link', { name: 'Thêm sản phẩm' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Thêm sản phẩm' })).toBeVisible()
  await page.getByLabel('SKU', { exact: true }).fill('E2E-TEE')
  await page.getByLabel('Tên sản phẩm', { exact: true }).fill('Áo thun E2E')
  await page.getByLabel('Giá', { exact: true }).fill('159000')
  await page.getByLabel('Tồn kho', { exact: true }).fill('5')
  await page.getByRole('button', { name: 'Tạo sản phẩm' }).click()
  await expect(page.getByText('Đã tạo sản phẩm.')).toBeVisible()
  await page.locator('#pd-file').setInputFiles({ name: 'tee.png', mimeType: 'image/png', buffer: Buffer.from(onePixelPNG, 'base64') })
  await expect(page.getByRole('img', { name: 'Ảnh 1' })).toBeVisible()

  // Imports: rows are checked; a row is fixed in place and checked again.
  await nav.getByRole('link', { name: 'Nhập sản phẩm' }).click()
  await page.locator('#imp-file').setInputFiles({ name: 'e2e.csv', mimeType: 'text/csv',
    buffer: Buffer.from('sku,name,description,price,currency,stock,weight_kg\nE2E-1,Mug,Gốm sứ,89000,VND,3,0.4\n') })
  await page.getByRole('button', { name: 'Nhập file' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'e2e.csv' })).toBeVisible()
  await expect(page.getByText('Cần có ngành hàng.')).toBeVisible({ timeout: 20_000 })
  await page.getByRole('button', { name: 'Sửa dòng 2' }).click()
  await page.getByLabel('category, dòng 2').fill('100017')
  await page.getByRole('button', { name: 'Lưu', exact: true }).click()
  await expect(page.getByText('Cần có ngành hàng.')).toHaveCount(0)
  await expect(page.getByText('Cần ít nhất một URL hình ảnh.')).toBeVisible()

  // Browser extension: a one-time pairing code is shown.
  await nav.getByRole('link', { name: 'Tiện ích trình duyệt' }).click()
  await expect(page.getByRole('heading', { name: 'Chưa ghép nối trình duyệt nào' })).toBeVisible()
  await page.getByRole('button', { name: 'Tạo mã ghép nối' }).click()
  await expect(page.getByText(/^[A-Z2-9]{5}-[A-Z2-9]{5}$/)).toBeVisible()

  // Two-step verification: turn it on, then sign in with a code.
  await page.getByRole('link', { name: /Bảo mật tài khoản/ }).click()
  await page.getByRole('button', { name: 'Bật' }).click()
  const secret = (await page.locator('code').first().textContent()) ?? ''
  await page.getByLabel('Mã', { exact: true }).fill(await totp(secret))
  await page.getByRole('button', { name: 'Xác nhận' }).click()
  await expect(page.getByRole('heading', { name: 'Mã khôi phục' })).toBeVisible()

  await page.getByRole('button', { name: 'Đăng xuất' }).click()
  await expect(page).toHaveURL(/\/login/)
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Mật khẩu').fill(password)
  await page.getByRole('button', { name: 'Đăng nhập' }).click()
  // A new code: the one used to confirm cannot be replayed.
  await page.waitForTimeout(Math.max(0, 30_000 - (Date.now() % 30_000)) + 500)
  await page.getByLabel('Mã xác minh').fill(await totp(secret))
  await page.getByRole('button', { name: 'Xác minh' }).click()
  await expect(page).not.toHaveURL(/\/login/)

  await page.getByRole('button', { name: 'Đăng xuất' }).click()
  await expect(page).toHaveURL(/\/login/)
})
