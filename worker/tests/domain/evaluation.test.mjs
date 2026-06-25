import { describe, it, expect } from 'vitest'
import { Evaluation } from '../../domain/Evaluation.mjs'

describe('Evaluation', () => {
  describe('fromBlocks', () => {
    it('exposes blocks, score, contentMd and is not a parse error', () => {
      const blocks = { blockA: { title: 'Role Fit', content: '...', score: 4.2 } }
      const evaluation = Evaluation.fromBlocks(blocks, 4.1, '## Block A...')

      expect(evaluation.blocks).toEqual(blocks)
      expect(evaluation.score).toBe(4.1)
      expect(evaluation.contentMd).toBe('## Block A...')
      expect(evaluation.isParseError).toBe(false)
      expect(evaluation.statusNote).toBeNull()
    })

    it('accepts a null score (partial blocks, no overall score)', () => {
      const blocks = { blockA: { title: 'Role Fit', content: '...', score: 4.2 } }
      const evaluation = Evaluation.fromBlocks(blocks, null, '## Block A...')

      expect(evaluation.score).toBeNull()
      expect(evaluation.isParseError).toBe(false)
      expect(evaluation.statusNote).toBeNull()
    })
  })

  describe('parseError', () => {
    it('exposes the parse_error sentinel shape (T-58)', () => {
      const raw = 'garbled response text'
      const evaluation = Evaluation.parseError(raw)

      expect(evaluation.blocks).toEqual({ parse_error: true, raw })
      expect(evaluation.score).toBeNull()
      expect(evaluation.contentMd).toBe(raw)
      expect(evaluation.isParseError).toBe(true)
      expect(evaluation.statusNote).toBe('Evaluation completed (parse error in blocks)')
    })

    it('handles an empty string as raw text', () => {
      const evaluation = Evaluation.parseError('')

      expect(evaluation.blocks).toEqual({ parse_error: true, raw: '' })
      expect(evaluation.contentMd).toBe('')
      expect(evaluation.isParseError).toBe(true)
    })
  })

  describe('T-58 invariant: impossible to construct an unpersistable Evaluation', () => {
    it('fromBlocks rejects empty blocks (would violate the persistence invariant)', () => {
      expect(() => Evaluation.fromBlocks({}, 4.0, 'text')).toThrow()
    })

    it('fromBlocks rejects null/undefined contentMd', () => {
      expect(() => Evaluation.fromBlocks({ blockA: {} }, 4.0, null)).toThrow()
      expect(() => Evaluation.fromBlocks({ blockA: {} }, 4.0, undefined)).toThrow()
    })

    it('there is no public constructor that bypasses the factories', () => {
      expect(() => new Evaluation()).toThrow()
    })
  })
})
