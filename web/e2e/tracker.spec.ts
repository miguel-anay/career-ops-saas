import { test, expect } from '@playwright/test'
import { seedAuth, mockApplications } from './fixtures'

test.describe('Application Tracker', () => {
  test.beforeEach(async ({ page }) => {
    await seedAuth(page)

    await page.route('**/api/applications**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ applications: mockApplications, total: 1 }),
      })
    )
  })

  test('shows applications table with mocked data', async ({ page }) => {
    await page.goto('/tracker')
    await expect(page.getByText('Acme Corp')).toBeVisible()
    await expect(page.getByText('Senior Software Engineer')).toBeVisible()
    await expect(page.getByRole('combobox').first()).toHaveValue('Evaluated')
  })

  test('status select change calls PATCH /api/applications/:id', async ({ page }) => {
    let patchBody = ''

    await page.route('**/api/applications/app-1', route => {
      if (route.request().method() === 'PATCH') {
        patchBody = route.request().postData() ?? ''
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ...mockApplications[0], status: 'Applied' }),
        })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })

    await page.goto('/tracker')
    await expect(page.getByText('Acme Corp')).toBeVisible()

    const patchResponse = page.waitForResponse(resp =>
      resp.url().includes('/api/applications/app-1') && resp.request().method() === 'PATCH'
    )
    await page.selectOption('select', 'Applied')
    await patchResponse

    expect(JSON.parse(patchBody)).toMatchObject({ status: 'Applied' })
  })

  test('notes textarea saves on blur', async ({ page }) => {
    let patchBody = ''

    await page.route('**/api/applications/app-1', route => {
      if (route.request().method() === 'PATCH') {
        patchBody = route.request().postData() ?? ''
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ...mockApplications[0], notes: 'Updated notes' }),
        })
      }
      return route.continue()
    })

    await page.goto('/tracker')
    await expect(page.getByText('Acme Corp')).toBeVisible()

    const textarea = page.getByPlaceholder('Notes…')
    await textarea.fill('Updated notes')
    await textarea.blur()

    await page.waitForTimeout(200)
    expect(JSON.parse(patchBody)).toMatchObject({ notes: 'Updated notes' })
  })

  test('empty state shows when no applications', async ({ page }) => {
    await page.route('**/api/applications**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ applications: [], total: 0 }),
      })
    )

    await page.goto('/tracker')
    await expect(page.getByText('No applications yet.')).toBeVisible()
  })

  test('← Pipeline link navigates back to dashboard', async ({ page }) => {
    await page.route('**/api/jobs**', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ jobs: [], total: 0, page: 1 }) })
    )

    await page.goto('/tracker')
    await page.getByRole('link', { name: /← pipeline/i }).click()
    await expect(page).toHaveURL('/')
  })
})
