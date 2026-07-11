import { describe, it, expect } from 'vitest'
import { normalizeJobUrl, isHostAllowed } from '../../lib/url-normalize.mjs'

// FU-2: direct coverage for isHostAllowed (the SSRF gate re-checked by
// worker/jobs/fetch-job-content.mjs before Playwright navigates). Previously
// only exercised indirectly via normalizeJobUrl's HOST_RULES checks.
describe('isHostAllowed', () => {
  const allowed = [
    'linkedin.com',
    'www.linkedin.com',
    'indeed.com',
    'www.indeed.com',
    'pe.indeed.com',
    'computrabajo.com',
    'computrabajo.com.pe',
    'www.computrabajo.com.pe',
    'bumeran.com',
    'bumeran.com.pe',
    'www.bumeran.com.ar',
  ]

  it.each(allowed)('allows %s', (hostname) => {
    expect(isHostAllowed(hostname)).toBe(true)
  })

  const rejected = [
    'evil.com',
    'tracking.evil-redirect.com',
    'boards.greenhouse.io',
    'jobs.lever.co',
    'localhost',
    '192.168.1.1',
    'linkedin.com.evil.com', // suffix-spoofing attempt
    '',
  ]

  it.each(rejected)('rejects %s', (hostname) => {
    expect(isHostAllowed(hostname)).toBe(false)
  })
})

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

  describe('security: protocol + hostname validation (CRITICAL fix)', () => {
    it('rejects non-http(s) protocols (ftp)', () => {
      expect(normalizeJobUrl('computrabajo', 'ftp://malware.example/payload.exe')).toBeNull()
    })

    it('rejects javascript: URLs', () => {
      expect(normalizeJobUrl('linkedin', 'javascript:alert(1)')).toBeNull()
    })

    it('rejects data: URLs', () => {
      expect(normalizeJobUrl('bumeran', 'data:text/html,<script>alert(1)</script>')).toBeNull()
    })

    it('does NOT rewrite a linkedin-shaped path on a non-linkedin host to a fabricated linkedin canonical URL', () => {
      const result = normalizeJobUrl('linkedin', 'https://tracking.evil-redirect.com/jobs/view/999888777')
      expect(result).not.toBe('https://www.linkedin.com/jobs/view/999888777')
      expect(result).toBeNull()
    })

    it('does NOT accept an indeed jk param from a non-indeed host', () => {
      const result = normalizeJobUrl('indeed', 'https://evil-redirect.example.com/x?jk=abcdef123456')
      expect(result).toBeNull()
    })

    it('rejects a computrabajo-claimed URL on an untrusted host', () => {
      expect(normalizeJobUrl('computrabajo', 'https://phishing.example.com/empleos/oferta/x')).toBeNull()
    })

    it('rejects a bumeran-claimed URL on an untrusted host', () => {
      expect(normalizeJobUrl('bumeran', 'https://phishing.example.com/empleos/x.html')).toBeNull()
    })

    it('still normalizes a legit linkedin subdomain (www.linkedin.com)', () => {
      expect(normalizeJobUrl('linkedin', 'https://www.linkedin.com/jobs/view/42?trk=x'))
        .toBe('https://www.linkedin.com/jobs/view/42')
    })

    it('still normalizes a legit indeed country subdomain (pe.indeed.com)', () => {
      expect(normalizeJobUrl('indeed', 'https://pe.indeed.com/rc/clk?jk=abc123&utm_source=email'))
        .toBe('https://www.indeed.com/viewjob?jk=abc123')
    })

    it('still normalizes a legit computrabajo country domain (computrabajo.com.pe)', () => {
      expect(normalizeJobUrl('computrabajo', 'https://www.computrabajo.com.pe/empleos/oferta/x?utm_source=email'))
        .toBe('https://www.computrabajo.com.pe/empleos/oferta/x')
    })

    it('still normalizes a legit bumeran country domain (bumeran.com.pe)', () => {
      expect(normalizeJobUrl('bumeran', 'https://www.bumeran.com.pe/empleos/x.html?utm_source=email'))
        .toBe('https://www.bumeran.com.pe/empleos/x.html')
    })
  })
})
