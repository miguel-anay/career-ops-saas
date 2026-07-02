import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import parser from '../../email-parsers/bumeran.mjs'

const fixtureHtml = readFileSync(
  fileURLToPath(new URL('../../email-parsers/__fixtures__/bumeran.html', import.meta.url)),
  'utf8'
)

describe('email-parsers/bumeran', () => {
  it('has id "bumeran"', () => {
    expect(parser.id).toBe('bumeran')
  })

  it('senderMatch: matches a registered Bumeran sender', () => {
    expect(parser.senderMatch('Bumeran <no-reply@bumeran.com.pe>')).toBe(true)
  })

  it('senderMatch: rejects an unrelated sender', () => {
    expect(parser.senderMatch('someone@example.com')).toBe(false)
  })

  it('parse: extracts [{title, company, url}] from fixture HTML', () => {
    const result = parser.parse({ subject: 'Empleos para ti', html: fixtureHtml, text: '' })

    expect(result).toEqual([
      {
        title: 'Desarrollador Fullstack',
        company: 'Innova SAC',
        url: 'https://www.bumeran.com.pe/empleos/desarrollador-fullstack-789.html?utm_source=email',
      },
      {
        title: 'Product Owner',
        company: 'Fintech Peru',
        url: 'https://www.bumeran.com.pe/empleos/product-owner-321.html?utm_source=email',
      },
    ])
  })

  it('parse: returns [] for empty/no-match html', () => {
    expect(parser.parse({ subject: '', html: '<html><body>no jobs</body></html>', text: '' })).toEqual([])
  })
})
