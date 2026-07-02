import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import parser from '../../email-parsers/computrabajo.mjs'

const fixtureHtml = readFileSync(
  fileURLToPath(new URL('../../email-parsers/__fixtures__/computrabajo.html', import.meta.url)),
  'utf8'
)

describe('email-parsers/computrabajo', () => {
  it('has id "computrabajo"', () => {
    expect(parser.id).toBe('computrabajo')
  })

  it('senderMatch: matches a registered Computrabajo sender', () => {
    expect(parser.senderMatch('Computrabajo <no-reply@computrabajo.com>')).toBe(true)
  })

  it('senderMatch: rejects an unrelated sender', () => {
    expect(parser.senderMatch('someone@example.com')).toBe(false)
  })

  it('parse: extracts [{title, company, url}] from fixture HTML', () => {
    const result = parser.parse({ subject: 'Nuevas ofertas', html: fixtureHtml, text: '' })

    expect(result).toEqual([
      {
        title: 'Desarrollador Backend',
        company: 'Tech Peru SAC',
        url: 'https://www.computrabajo.com.pe/empleos/oferta/desarrollador-backend-123?utm_source=email&utm_campaign=alert',
      },
      {
        title: 'QA Analyst',
        company: 'Datalatam',
        url: 'https://www.computrabajo.com.pe/empleos/oferta/qa-analyst-456?utm_source=email&utm_campaign=alert',
      },
    ])
  })

  it('parse: returns [] for empty/no-match html', () => {
    expect(parser.parse({ subject: '', html: '<html><body>no jobs</body></html>', text: '' })).toEqual([])
  })
})
