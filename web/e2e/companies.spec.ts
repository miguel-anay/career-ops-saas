import { test, expect } from '@playwright/test'
import { seedAuth, mockCompanies } from './fixtures'

test.describe('Companies', () => {
  test.beforeEach(async ({ page }) => {
    await seedAuth(page)

    await page.route('**/api/companies', route => {
      if (route.request().method() === 'GET') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ companies: mockCompanies }),
        })
      }
      return route.continue()
    })
  })

  test('shows companies list with mocked data', async ({ page }) => {
    await page.goto('/companies')
    await expect(page.getByText('Acme Corp')).toBeVisible()
    await expect(page.getByText('https://jobs.acme.com')).toBeVisible()
    await expect(page.getByText('greenhouse')).toBeVisible()
  })

  test('add company form submits and creates company', async ({ page }) => {
    let postBody = ''

    await page.route('**/api/companies', route => {
      if (route.request().method() === 'POST') {
        postBody = route.request().postData() ?? ''
        return route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ id: 'company-new', name: 'New Corp', careers_url: 'https://jobs.newcorp.com', provider_id: 'ashby', enabled: true }),
        })
      }
      // GET already handled by beforeEach
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ companies: mockCompanies }),
      })
    })

    await page.goto('/companies')
    await expect(page.getByText('Acme Corp')).toBeVisible()

    await page.getByPlaceholder('Company name').fill('New Corp')
    await page.getByPlaceholder('Careers URL').fill('https://jobs.newcorp.com')

    // Provider is a shadcn Select (not native <select>) — click the trigger then select item
    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: 'Ashby' }).click()

    await page.getByRole('button', { name: /add company/i }).click()

    await expect(page.getByText(/new corp added/i)).toBeVisible({ timeout: 5000 })

    const body = JSON.parse(postBody)
    expect(body.name).toBe('New Corp')
    expect(body.careers_url).toBe('https://jobs.newcorp.com')
    expect(body.provider_id).toBe('ashby')
  })

  test('remove button opens confirmation dialog', async ({ page }) => {
    await page.goto('/companies')
    await expect(page.getByText('Acme Corp')).toBeVisible()

    await page.getByRole('button', { name: /remove/i }).first().click()

    await expect(page.getByRole('dialog')).toBeVisible({ timeout: 3000 })
    await expect(page.getByText(/remove acme corp\?/i)).toBeVisible()
  })

  test('remove company confirmed calls DELETE /api/companies/:id', async ({ page }) => {
    let deleteUrl = ''

    await page.route('**/api/companies/**', route => {
      if (route.request().method() === 'DELETE') {
        deleteUrl = route.request().url()
        return route.fulfill({ status: 204 })
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ companies: mockCompanies }),
      })
    })

    await page.goto('/companies')
    await expect(page.getByText('Acme Corp')).toBeVisible()

    await page.getByRole('button', { name: /remove/i }).first().click()
    await expect(page.getByRole('dialog')).toBeVisible()

    const deleteResponse = page.waitForResponse(resp =>
      resp.url().includes('/api/companies/') && resp.request().method() === 'DELETE'
    )
    await page.getByRole('button', { name: /remove/i }).last().click()
    await deleteResponse

    expect(deleteUrl).toContain('company-1')
  })

  test('empty state shown when no companies', async ({ page }) => {
    await page.route('**/api/companies', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ companies: [] }),
      })
    )

    await page.goto('/companies')
    await expect(page.getByText('No companies watched yet.')).toBeVisible()
  })
})
