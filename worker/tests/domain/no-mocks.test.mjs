import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const domainDir = path.resolve(__dirname, '../../domain')
const testsDomainDir = __dirname

const FORBIDDEN_IMPORTS = ['lib/db.mjs', 'lib/anthropic.mjs', 'lib/prompt.mjs']

describe('domain layer purity guard', () => {
  const domainFiles = readdirSync(domainDir).filter((f) => f.endsWith('.mjs'))

  it('domain/ contains at least one file (sanity check the guard runs against real files)', () => {
    expect(domainFiles.length).toBeGreaterThan(0)
  })

  it.each(domainFiles)('domain/%s imports none of lib/db.mjs, lib/anthropic.mjs, lib/prompt.mjs', (file) => {
    const source = readFileSync(path.join(domainDir, file), 'utf8')
    for (const forbidden of FORBIDDEN_IMPORTS) {
      expect(source).not.toContain(forbidden)
    }
  })

  const domainTestFiles = readdirSync(testsDomainDir).filter(
    (f) => f.endsWith('.test.mjs') && f !== 'no-mocks.test.mjs'
  )

  it.each(domainTestFiles)('tests/domain/%s uses no vi.mock', (file) => {
    const source = readFileSync(path.join(testsDomainDir, file), 'utf8')
    expect(source).not.toContain('vi.mock')
  })
})
