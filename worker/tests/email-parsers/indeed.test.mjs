import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import parser from '../../email-parsers/indeed.mjs'

const fixtureHtml = readFileSync(
  fileURLToPath(new URL('../../email-parsers/__fixtures__/indeed.html', import.meta.url)),
  'utf8'
)

describe('email-parsers/indeed', () => {
  it('has id "indeed"', () => {
    expect(parser.id).toBe('indeed')
  })

  it('senderMatch: matches a registered Indeed sender', () => {
    expect(parser.senderMatch('Indeed <alert@indeed.com>')).toBe(true)
  })

  it('senderMatch: rejects an unrelated sender', () => {
    expect(parser.senderMatch('someone@example.com')).toBe(false)
  })

  it('parse: extracts [{title, company, url}] from fixture HTML', () => {
    const result = parser.parse({ subject: 'New jobs', html: fixtureHtml, text: '' })

    expect(result).toEqual([
      {
        title: 'Software Engineer',
        company: 'Remote Co',
        url: 'https://www.indeed.com/rc/clk?jk=abcdef123456&from=serp&utm_source=email',
      },
      {
        title: 'Data Analyst',
        company: 'Insights Inc',
        url: 'https://www.indeed.com/rc/clk?jk=fedcba654321&from=serp&utm_source=email',
      },
    ])
  })

  it('parse: returns [] for empty/no-match html', () => {
    expect(parser.parse({ subject: '', html: '<html><body>no jobs</body></html>', text: '' })).toEqual([])
  })
})
