/// <reference types="node" />
import { expect, test, type APIRequestContext } from '@playwright/test'

const mailpit = process.env.MAILPIT_URL ?? 'http://localhost:8025'

// latestLink returns the first link with `path` in the newest email to `to`.
async function latestLink(request: APIRequestContext, to: string, path: string): Promise<string> {
  for (let i = 0; i < 20; i++) {
    const search = await (await request.get(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:${to}`)}`)).json()
    for (const m of search.messages ?? []) {
      const msg = await (await request.get(`${mailpit}/api/v1/message/${m.ID}`)).json()
      const match = (msg.Text as string).match(new RegExp(`https?://[^\\s]*${path}[^\\s]*`))
      if (match) return match[0]
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`no email with ${path} to ${to}`)
}

test('sign up, create an organization and invite a member', async ({ page, request }) => {
  const stamp = Date.now()
  const email = `owner-${stamp}@example.com`
  const password = 'correct horse battery staple'

  await page.goto('/register')
  await page.getByLabel('Your name').fill('Owner')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page.getByText('for a link to verify your email address')).toBeVisible()

  await page.goto(await latestLink(request, email, '/verify-email'))
  await expect(page.getByText('Your email address is verified.')).toBeVisible()

  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/orgs\/new$/)

  await page.getByLabel('Name', { exact: true }).fill(`Acme ${stamp}`)
  await expect(page.getByLabel('URL name')).toHaveValue(`acme-${stamp}`)
  await page.getByRole('button', { name: 'Create organization' }).click()
  await expect(page.getByRole('heading', { level: 1, name: `Acme ${stamp}` })).toBeVisible()

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Members' }).click()
  await page.getByLabel('Email').fill(`invitee-${stamp}@example.com`)
  await page.getByRole('button', { name: 'Send invitation' }).click()
  await expect(page.getByText(`Invitation sent to invitee-${stamp}@example.com.`)).toBeVisible()
  await expect(page.getByRole('cell', { name: `invitee-${stamp}@example.com` })).toBeVisible()

  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Shops' }).click()
  await expect(page.getByRole('heading', { name: 'No shops yet' })).toBeVisible()
  // The test stack has no Shopee partner app registered, so connecting explains why.
  await page.getByRole('button', { name: 'Connect shop' }).first().click()
  await expect(page.getByText('No Shopee app is configured yet')).toBeVisible()
  // Errors from the Shopee redirect are shown on the shop list.
  await page.goto(`${new URL(page.url()).pathname}?connect_error=state_expired`)
  await expect(page.getByRole('alert').filter({ hasText: 'expired after 10 minutes' })).toBeVisible()

  // Templates: create, edit a section, validate, publish.
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Shop Templates' }).click()
  await page.getByLabel('New template name').fill('Vietnam default')
  await page.getByRole('button', { name: 'Create template' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Vietnam default' })).toBeVisible()
  await page.getByLabel('Target market').selectOption('VN')
  await page.getByLabel('Shop name').fill('Cửa hàng Minh An')
  await page.getByRole('button', { name: 'Save section' }).click()
  await expect(page.getByText('Section saved.')).toBeVisible()
  await page.getByRole('navigation', { name: 'Sections' }).getByRole('button', { name: /Contact/ }).click()
  await page.getByLabel('Phone').fill('0901234567')
  await page.getByRole('button', { name: 'Save section' }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'does not match pattern' })).toBeVisible()
  await page.getByLabel('Phone').fill('+84901234567')
  await page.getByRole('button', { name: 'Save section' }).click()
  await expect(page.getByText('Section saved.')).toBeVisible()
  await page.getByRole('button', { name: 'Validate' }).click()
  await expect(page.getByText('Ready to publish')).toBeVisible()
  await page.getByRole('button', { name: 'Publish' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Publish' }).click()
  await expect(page.getByText('is published and can no longer be changed')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save section' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login/)
})
