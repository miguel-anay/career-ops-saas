import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import parser from '../../email-parsers/linkedin.mjs'

const fixtureHtml = readFileSync(
  fileURLToPath(new URL('../../email-parsers/__fixtures__/linkedin.html', import.meta.url)),
  'utf8'
)

describe('email-parsers/linkedin', () => {
  it('has id "linkedin"', () => {
    expect(parser.id).toBe('linkedin')
  })

  it('senderMatch: matches a registered LinkedIn sender', () => {
    expect(parser.senderMatch('LinkedIn Job Alerts <jobalerts-noreply@linkedin.com>')).toBe(true)
  })

  it('senderMatch: rejects an unrelated sender', () => {
    expect(parser.senderMatch('someone@example.com')).toBe(false)
  })

  it('parse: extracts [{title, company, url}] from fixture HTML', () => {
    const result = parser.parse({ subject: 'Jobs for you', html: fixtureHtml, text: '' })

    expect(result).toEqual([
      {
        title: 'Senior Backend Engineer',
        company: 'Acme Corp',
        url: 'https://www.linkedin.com/jobs/view/3812345678?trk=email_jobs_alert',
      },
      {
        title: 'Platform Engineer',
        company: 'Globex',
        url: 'https://www.linkedin.com/jobs/view/3812345999?trk=email_jobs_alert',
      },
    ])
  })

  it('parse: returns [] for empty/no-match html', () => {
    expect(parser.parse({ subject: '', html: '<html><body>no jobs</body></html>', text: '' })).toEqual([])
  })
})
