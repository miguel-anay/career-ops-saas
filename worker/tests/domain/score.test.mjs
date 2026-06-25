import { describe, it, expect } from 'vitest'
import { Score } from '../../domain/Score.mjs'

describe('Score', () => {
  it('accepts values within the inclusive 0..5 range', () => {
    expect(Score.of(0).value).toBe(0)
    expect(Score.of(5).value).toBe(5)
    expect(Score.of(4.1).value).toBe(4.1)
  })

  it('accepts null (unscored)', () => {
    expect(Score.of(null).value).toBeNull()
  })

  it('rejects values above 5 by throwing RangeError', () => {
    expect(() => Score.of(5.1)).toThrow(RangeError)
    expect(() => Score.of(100)).toThrow(RangeError)
  })

  it('rejects values below 0 by throwing RangeError', () => {
    expect(() => Score.of(-0.1)).toThrow(RangeError)
    expect(() => Score.of(-5)).toThrow(RangeError)
  })

  it('rejects non-numeric input by throwing TypeError (no coercion)', () => {
    expect(() => Score.of('4.2')).toThrow(TypeError)
    expect(() => Score.of(undefined)).toThrow(TypeError)
    expect(() => Score.of(NaN)).toThrow(TypeError)
  })

  it('does not expose threshold/recommendation logic', () => {
    const score = Score.of(4.5)
    expect(score.isRecommended).toBeUndefined()
    expect(Score.RECOMMEND_THRESHOLD).toBeUndefined()
  })
})
