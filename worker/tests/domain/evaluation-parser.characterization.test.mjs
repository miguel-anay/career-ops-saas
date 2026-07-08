import { describe, it, expect } from 'vitest'
import { EvaluationParser } from '../../domain/EvaluationParser.mjs'

/**
 * ORACLE — verbatim copy of `parseEvaluationResponse` from
 * `worker/jobs/evaluate.mjs` AS IT EXISTS TODAY. This is a frozen snapshot,
 * not a live import: the refactor must not change the production file
 * during PR1, and this test pins the oracle's exact current output as the
 * behavior-preservation gate for the new `EvaluationParser` domain type.
 *
 * Do NOT "clean up" or refactor this function. It exists to fail loudly if
 * `EvaluationParser.parse` ever diverges from the legacy behavior.
 */
function oracle(responseText) {
  if (!responseText || !responseText.trim()) {
    return {
      blocks: { parse_error: true, raw: responseText },
      score: null,
      contentMd: responseText || '',
    }
  }

  try {
    const blocks = {}

    const blockPattern = /##\s+Block\s+([A-G])\s*[—–-]\s*([^\n]+)([\s\S]*?)(?=##\s+Block\s+[A-G]|##\s+Overall|\*\*Overall Score|$)/gi
    let match
    while ((match = blockPattern.exec(responseText)) !== null) {
      const blockKey = `block${match[1].toUpperCase()}`
      const blockTitle = match[2].trim()
      const blockContent = match[3].trim()

      const scoreMatch = blockContent.match(/Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5/i)
      const score = scoreMatch ? parseFloat(scoreMatch[1]) : null

      blocks[blockKey] = {
        title: blockTitle,
        content: blockContent,
        score,
      }
    }

    const overallMatch = responseText.match(/\*\*Overall Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5\*\*/i)
    const overallScore = overallMatch ? parseFloat(overallMatch[1]) : null

    if (Object.keys(blocks).length === 0) {
      return {
        blocks: { parse_error: true, raw: responseText },
        score: null,
        contentMd: responseText,
      }
    }

    return {
      blocks,
      score: overallScore,
      contentMd: responseText,
    }
  } catch (err) {
    return {
      blocks: { parse_error: true, raw: responseText },
      score: null,
      contentMd: responseText,
    }
  }
}

const FULL_RESPONSE = `## Block A — Role & Company Fit
Score: 4.2/5
Strong alignment with AI engineering background.

## Block B — Technical Match
Score: 4.5/5
All required skills present.

## Block C — Compensation
Score: 3.8/5
Base salary within target range.

## Block D — Growth & Impact
Score: 4.0/5
Good growth trajectory.

## Block E — Culture & Location
Score: 3.5/5
Remote-first culture.

## Block F — Red Flags
Score: 4.5/5
No significant concerns.

## Block G — Posting Legitimacy
Tier: 1 — Verified Direct

**Overall Score: 4.1/5**`

const PARTIAL_RESPONSE = `## Block A — Role & Company Fit
Score: 4.2/5
Strong alignment.

## Block B — Technical Match
Score: 4.5/5
All required skills present.`

const EMPTY_RESPONSE = ''

const MALFORMED_RESPONSE = 'This is not a valid block format at all — just random text.'

const FIXTURES = [
  ['full A-G blocks + overall score', FULL_RESPONSE],
  ['partial blocks, no overall score', PARTIAL_RESPONSE],
  ['empty/blank response', EMPTY_RESPONSE],
  ['malformed/no-headers response', MALFORMED_RESPONSE],
]

// oracleBlocksToArray converts the oracle's legacy letter-keyed object
// (`{blockA: {title, content, score}, ...}`) into the array shape
// `EvaluationParser.parse` now produces: `[{label, content}]` sorted A→G,
// per design decision 2 (evaluation-quality). Per-block `score` is dropped
// (YAGNI — nothing consumes it); the overall `score` field is unaffected.
const BLOCK_LETTERS = ['A', 'B', 'C', 'D', 'E', 'F', 'G']

function oracleBlocksToArray(oracleBlocks) {
  return BLOCK_LETTERS.filter((letter) => oracleBlocks[`block${letter}`]).map((letter) => {
    const { title, content } = oracleBlocks[`block${letter}`]
    return { label: title, content }
  })
}

describe('EvaluationParser characterization (oracle = current parseEvaluationResponse)', () => {
  describe('sanity: oracle is self-consistent', () => {
    it('full response — oracle extracts 7 blocks + overall score', () => {
      const result = oracle(FULL_RESPONSE)
      expect(Object.keys(result.blocks)).toHaveLength(7)
      expect(result.score).toBe(4.1)
    })

    it('empty response — oracle returns parse_error sentinel', () => {
      const result = oracle(EMPTY_RESPONSE)
      expect(result.blocks).toEqual({ parse_error: true, raw: EMPTY_RESPONSE })
      expect(result.score).toBeNull()
    })
  })

  describe.each([FIXTURES[0], FIXTURES[1]])('fixture: %s (successful parse)', (_label, responseText) => {
    it('EvaluationParser.parse emits an A→G array of {label, content}, sorted, matching the oracle content', () => {
      const golden = oracle(responseText)
      const evaluation = EvaluationParser.parse(responseText)

      expect(Array.isArray(evaluation.blocks)).toBe(true)
      expect(evaluation.blocks).toEqual(oracleBlocksToArray(golden.blocks))
      expect(evaluation.score).toEqual(golden.score)
      expect(evaluation.contentMd).toEqual(golden.contentMd)
    })

    it('EvaluationParser.parse never throws', () => {
      expect(() => EvaluationParser.parse(responseText)).not.toThrow()
    })
  })

  describe.each([FIXTURES[2], FIXTURES[3]])('fixture: %s (parse error)', (_label, responseText) => {
    it('EvaluationParser.parse keeps the parse_error sentinel object, array-safe for the web guard', () => {
      const golden = oracle(responseText)
      const evaluation = EvaluationParser.parse(responseText)

      expect(evaluation.blocks).toEqual(golden.blocks)
      expect(evaluation.score).toEqual(golden.score)
      expect(evaluation.contentMd).toEqual(golden.contentMd)

      // "array-safe": the web client's `Array.isArray(blocks) && blocks.length > 0`
      // guard must be false, not throw, for this shape (spec: LLM output
      // fails to parse into blocks).
      expect(Array.isArray(evaluation.blocks) && evaluation.blocks.length > 0).toBe(false)
    })

    it('EvaluationParser.parse never throws', () => {
      expect(() => EvaluationParser.parse(responseText)).not.toThrow()
    })
  })

  describe('parse-error guard never throws (T-166)', () => {
    it('empty string never throws and is flagged as parse error', () => {
      expect(() => EvaluationParser.parse('')).not.toThrow()
      const evaluation = EvaluationParser.parse('')
      expect(evaluation.isParseError).toBe(true)
    })

    it('garbled/no-headers text never throws and is flagged as parse error', () => {
      expect(() => EvaluationParser.parse(MALFORMED_RESPONSE)).not.toThrow()
      const evaluation = EvaluationParser.parse(MALFORMED_RESPONSE)
      expect(evaluation.isParseError).toBe(true)
    })
  })
})
