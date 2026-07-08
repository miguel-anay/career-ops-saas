import { chromium } from 'playwright'

/**
 * Navigate to a URL and extract visible text content using Playwright.
 *
 * Launches a headless Chromium instance per call (same pattern as
 * generate-pdf.mjs), navigates to the URL, waits for the page to settle,
 * and extracts `document.body.innerText`.
 *
 * @param {string} url - The URL to navigate to
 * @returns {Promise<string>} The visible text content of the page
 */
export async function fetchPageText(url) {
  const browser = await chromium.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'],
  })

  try {
    const page = await browser.newPage()
    await page.goto(url, { waitUntil: 'networkidle', timeout: 30000 })
    const text = await page.evaluate(() => document.body.innerText)
    return text
  } finally {
    await browser.close()
  }
}
