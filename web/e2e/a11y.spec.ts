/// <reference types="node" />
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { latestLink } from './helpers'

// Accessibility audit: every main page is checked with axe-core against
// WCAG 2.0/2.1/2.2 A and AA rules. Needs the API, worker and Mailpit.
const tags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']

async function audit(page: Page, name: string, found: string[]) {
  await page.waitForLoadState('networkidle')
  const r = await new AxeBuilder({ page }).withTags(tags).analyze()
  for (const v of r.violations) {
    found.push(`${name}: ${v.id} (${v.impact}) ${v.help} — ${v.nodes.slice(0, 3).map((n) => `${n.target.join(' ')} [${n.any[0]?.message ?? ''}]`).join(' | ')}`)
  }
}

test('main pages have no WCAG A/AA violations', async ({ page, request }) => {
  test.setTimeout(180_000)
  const found: string[] = []
  const stamp = Date.now()
  const email = `a11y-${stamp}@example.com`
  const password = 'correct horse battery staple'

  await page.goto('/login')
  await audit(page, 'login', found)
  await page.goto('/register')
  await audit(page, 'register', found)
  await page.getByLabel('Tên của bạn').fill('A11y')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Mật khẩu').fill(password)
  await page.getByRole('button', { name: 'Tạo tài khoản' }).click()
  await page.goto(await latestLink(request, email, '/verify-email'))
  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Mật khẩu').fill(password)
  await page.getByRole('button', { name: 'Đăng nhập' }).click()
  await expect(page).toHaveURL(/\/orgs\/new$/)
  await audit(page, 'new organization', found)
  await page.getByLabel('Tên', { exact: true }).fill(`A11y ${stamp}`)
  await page.getByRole('button', { name: 'Tạo tổ chức' }).click()
  await expect(page.getByRole('heading', { level: 1, name: `A11y ${stamp}` })).toBeVisible()
  const base = new URL(page.url()).pathname

  // Some content so lists and forms are not empty.
  await page.goto(`${base}/products/new`)
  await page.getByLabel('SKU', { exact: true }).fill('A11Y-1')
  await page.getByLabel('Tên sản phẩm', { exact: true }).fill('Accessible tee')
  await page.getByLabel('Giá', { exact: true }).fill('100000')
  await page.getByRole('button', { name: 'Tạo sản phẩm' }).click()
  await expect(page.getByText('Đã tạo sản phẩm.')).toBeVisible()
  await audit(page, 'product detail', found)
  await page.goto(`${base}/templates`)
  await page.getByLabel('Tên mẫu shop mới').fill('A11y template')
  await page.getByRole('button', { name: 'Tạo mẫu shop' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'A11y template' })).toBeVisible()
  await audit(page, 'template editor', found)

  for (const [name, path] of [
    ['overview', ''], ['organizations', '/../'], ['shops', '/shops'], ['templates', '/templates'], ['products', '/products'],
    ['new product', '/products/new'], ['imports', '/imports'], ['workflows', '/workflows'], ['logs', '/logs'],
    ['extension', '/extension'], ['account', '/../../account'], ['members', '/members'], ['settings', '/settings'], ['audit', '/audit'],
  ] as const) {
    await page.goto(name === 'organizations' ? '/orgs' : name === 'account' ? '/account' : `${base}${path}`)
    await audit(page, name, found)
  }
  await page.goto(base)
  await page.getByRole('button', { name: /Thông báo/ }).click()
  await audit(page, 'notifications panel', found)

  console.log(found.length ? `Violations:\n${found.join('\n')}` : 'No violations')
  expect(found).toEqual([])
})
