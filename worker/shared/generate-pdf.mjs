import { chromium } from 'playwright'

/**
 * Render an HTML string to a PDF Buffer using Playwright Chromium.
 *
 * Uses --no-sandbox for container compatibility (Docker/Kubernetes).
 * Returns the raw PDF as a Buffer suitable for upload to R2.
 *
 * @param {string} htmlContent - Full HTML document as a string
 * @param {object} [opts]
 * @param {string} [opts.format] - Page format: 'a4' (default) or 'letter'
 * @returns {Promise<Buffer>}
 */
export async function renderPDF(htmlContent, { format = 'a4' } = {}) {
  const browser = await chromium.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'],
  })

  try {
    const page = await browser.newPage()

    await page.setContent(htmlContent, {
      waitUntil: 'networkidle',
    })

    // Wait for fonts to load
    await page.evaluate(() => document.fonts.ready)

    const pdfBuffer = await page.pdf({
      format,
      printBackground: true,
      margin: {
        top: '0.6in',
        right: '0.6in',
        bottom: '0.6in',
        left: '0.6in',
      },
      preferCSSPageSize: false,
    })

    return Buffer.from(pdfBuffer)
  } finally {
    await browser.close()
  }
}
