import { test, expect } from '@playwright/test'
import { seedAuth, mockJobs } from './fixtures'

test.describe('Dashboard (Job Pipeline)', () => {
  test.beforeEach(async ({ page }) => {
    await seedAuth(page)

    await page.route('**/api/jobs**', route =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ jobs: mockJobs, total: 2, page: 1 }),
      })
    )
  })

  test('shows job pipeline table with mocked data', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByText('Senior Software Engineer')).toBeVisible()
    await expect(page.getByText('Acme Corp')).toBeVisible()
    await expect(page.getByText('Frontend Developer')).toBeVisible()
  })

  test('Add Job form submits URL and calls POST /api/jobs', async ({ page }) => {
    await page.route('**/api/jobs', route => {
      if (route.request().method() === 'POST') {
        return route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ id: 'job-new', url: 'https://example.com/job', status: 'pending', platform: 'ashby' }),
        })
      }
      return route.continue()
    })

    await page.goto('/')

    const input = page.getByPlaceholder(/job url/i)
    await input.fill('https://greenhouse.io/jobs/12345')
    await page.getByRole('button', { name: /^add$/i }).click()

    await expect(page.getByText(/job added successfully/i)).toBeVisible({ timeout: 5000 })
  })

  test('Add Job button is disabled when URL is empty', async ({ page }) => {
    await page.goto('/')
    const addBtn = page.getByRole('button', { name: /^add$/i })
    await expect(addBtn).toBeDisabled()
  })

  test('Scan Now button triggers scan and shows progress banner', async ({ page }) => {
    await page.route('**/api/scan', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    )

    await page.goto('/')
    await page.getByRole('button', { name: /scan now/i }).click()

    await expect(page.getByText(/scanning portals/i)).toBeVisible({ timeout: 3000 })
  })

  test('job row links to job detail page', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'Senior Software Engineer' }).click()
    await expect(page).toHaveURL(/\/jobs\/job-1/)
  })

  test('navigation links to Tracker and Companies pages', async ({ page }) => {
    await page.route('**/api/applications**', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ applications: [], total: 0 }) })
    )
    await page.route('**/api/companies**', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ companies: [], total: 0 }) })
    )
    await page.route('**/api/providers', route =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    )

    await page.goto('/')
    await page.getByRole('link', { name: 'Tracker' }).click()
    await expect(page).toHaveURL('/tracker')

    await page.goto('/')
    await page.getByRole('link', { name: 'Companies' }).click()
    await expect(page).toHaveURL('/companies')
  })
})
