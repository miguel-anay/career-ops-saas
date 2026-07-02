import { describe, it, expect } from 'vitest'
import { parseJobCards } from '../../email-parsers/_shared.mjs'

describe('email-parsers/_shared parseJobCards (DoS hardening, CRITICAL fix)', () => {
  it('rejects HTML larger than the size cap instead of parsing it', () => {
    const oversized = 'x'.repeat(512 * 1024 + 1)
    expect(() => parseJobCards(oversized)).toThrow(/exceeds|too large|cap/i)
  })

  it('accepts HTML at or under the size cap', () => {
    const atCap = 'x'.repeat(512 * 1024)
    expect(() => parseJobCards(atCap)).not.toThrow()
  })

  it('completes quickly on adversarial HTML with many repeated anchors and no company span, at the size cap (previously O(n^2): ~577ms measured with the unbounded regex at this exact size)', () => {
    // Sized right up to (but under) the 512KB cap — the worst case that can
    // ever reach the regex now that finding 2a's cap is in place. Isolates
    // the regex complexity fix (2b) from the size-cap fix (2a).
    const block = '<a href="https://example.com/x">Some Job Title Here</a>'
    const reps = Math.floor((512 * 1024) / block.length)
    const adversarial = block.repeat(reps) // zero <span class="company"> anywhere in the whole document

    const start = performance.now()
    const result = parseJobCards(adversarial)
    const durationMs = performance.now() - start

    expect(result).toEqual([]) // no company span anywhere -> no job cards, correctness preserved
    expect(durationMs).toBeLessThan(200) // was ~577ms measured with the unbounded regex at this size
  }, 3000)

  it('still extracts correctly-formed job cards after the regex rewrite', () => {
    const html = `
      <div class="job-alert"><a href="https://example.com/a">Title A</a> - <span class="company">Co A</span></div>
      <div class="job-alert"><a href="https://example.com/b">Title B</a> - <span class="company">Co B</span></div>
    `
    expect(parseJobCards(html)).toEqual([
      { title: 'Title A', company: 'Co A', url: 'https://example.com/a' },
      { title: 'Title B', company: 'Co B', url: 'https://example.com/b' },
    ])
  })
})
