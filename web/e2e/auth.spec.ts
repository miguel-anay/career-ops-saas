import { test, expect } from '@playwright/test'

test.describe('Authentication', () => {
  test.beforeEach(async ({ context }) => {
    await context.clearCookies()
    await context.addInitScript(() => localStorage.clear())
  })

  test('login page renders correctly', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: /sign in|login|career ops/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /sign in with google/i })).toBeVisible()
  })

  test('unauthenticated user is redirected to login', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveURL(/\/login/)
  })

  test('unauthenticated user cannot access tracker', async ({ page }) => {
    await page.goto('/tracker')
    await expect(page).toHaveURL(/\/login/)
  })

  test('unauthenticated user cannot access companies', async ({ page }) => {
    await page.goto('/companies')
    await expect(page).toHaveURL(/\/login/)
  })
})
