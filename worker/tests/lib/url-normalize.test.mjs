import { describe, it, expect } from 'vitest'
import { normalizeJobUrl } from '../../lib/url-normalize.mjs'

describe('normalizeJobUrl', () => {
  const cases = [
    {
      platform: 'linkedin',
      raw: 'https://www.linkedin.com/jobs/view/3812345678/?trackingId=abc&refId=xyz&trk=email',
      expected: 'https://www.linkedin.com/jobs/view/3812345678',
    },
    {
      platform: 'linkedin',
      raw: 'https://www.linkedin.com/comm/jobs/view/3812345678',
      expected: 'https://www.linkedin.com/jobs/view/3812345678',
    },
    {
      platform: 'indeed',
      raw: 'https://www.indeed.com/rc/clk?jk=abcdef123456&from=serp&vjs=3&utm_source=email',
      expected: 'https://www.indeed.com/viewjob?jk=abcdef123456',
    },
    {
      platform: 'computrabajo',
      raw: 'https://www.computrabajo.com.pe/empleos/oferta/dev-123?utm_source=email&utm_campaign=alert&Data=abc',
      expected: 'https://www.computrabajo.com.pe/empleos/oferta/dev-123',
    },
    {
      platform: 'bumeran',
      raw: 'https://www.bumeran.com.pe/empleos/dev-456.html?utm_source=email&utm_medium=alert',
      expected: 'https://www.bumeran.com.pe/empleos/dev-456.html',
    },
  ]

  it.each(cases)('$platform: strips tracking params to canonical form', ({ platform, raw, expected }) => {
    expect(normalizeJobUrl(platform, raw)).toBe(expected)
  })

  it('two differently-tracked LinkedIn links for the same job dedup to the same canonical URL', () => {
    const a = normalizeJobUrl('linkedin', 'https://www.linkedin.com/jobs/view/999?trk=email_alert')
    const b = normalizeJobUrl('linkedin', 'https://www.linkedin.com/jobs/view/999?refId=other&trackingId=x')
    expect(a).toBe(b)
  })

  it('unknown platform falls back to stripping utm_* and gclid, keeping path', () => {
    const result = normalizeJobUrl('unknown-sender', 'https://example.com/jobs/42?utm_source=x&gclid=y&keep=1')
    expect(result).toBe('https://example.com/jobs/42?keep=1')
  })

  it('returns null for an unparsable URL instead of throwing', () => {
    expect(normalizeJobUrl('linkedin', 'not-a-url')).toBeNull()
  })
})
