import { describe, it, expect } from 'vitest'
import { getParsers, allSenders, findParserForSender } from '../../email-parsers/index.mjs'

describe('email-parsers/index', () => {
  it('getParsers returns all 4 registered sender parsers', () => {
    const ids = getParsers().map((p) => p.id).sort()
    expect(ids).toEqual(['bumeran', 'computrabajo', 'indeed', 'linkedin'])
  })

  it('allSenders returns the union of every parser\'s senders[]', () => {
    const senders = allSenders()
    expect(senders).toEqual(expect.arrayContaining([
      'jobalerts-noreply@linkedin.com',
      'no-reply@computrabajo.com',
      'no-reply@bumeran.com.pe',
      'alert@indeed.com',
    ]))
    expect(senders).toHaveLength(4)
  })

  it('findParserForSender resolves the matching parser by from-address', () => {
    const parser = findParserForSender('Indeed <alert@indeed.com>')
    expect(parser?.id).toBe('indeed')
  })

  it('findParserForSender returns null for an unrecognized sender', () => {
    expect(findParserForSender('someone@example.com')).toBeNull()
  })
})
