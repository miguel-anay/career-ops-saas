// EvaluationParser — pure domain parser for the 7-block (A-G) Anthropic
// evaluation response, plus the overall score line.
//
// This is a behavior-preserving port of `parseEvaluationResponse` from
// `worker/jobs/evaluate.mjs` (see `worker/tests/domain/evaluation-parser
// .characterization.test.mjs` for the oracle proving equivalence). It
// returns a domain `Evaluation` instance instead of a plain object, and
// NEVER throws (T-58): any internal exception is caught and converted to
// `Evaluation.parseError(responseText)`.

import { Evaluation } from './Evaluation.mjs'

const BLOCK_PATTERN =
  /##\s+Block\s+([A-G])\s*[—–-]\s*([^\n]+)([\s\S]*?)(?=##\s+Block\s+[A-G]|##\s+Overall|\*\*Overall Score|$)/gi

const BLOCK_SCORE_PATTERN = /Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5/i

const OVERALL_SCORE_PATTERN = /\*\*Overall Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5\*\*/i

export const EvaluationParser = {
  /**
   * @param {string} responseText - raw text from the Anthropic response
   * @returns {Evaluation} - never throws
   */
  parse(responseText) {
    if (!responseText || !responseText.trim()) {
      return Evaluation.parseError(responseText)
    }

    try {
      const blocks = {}

      let match
      // BLOCK_PATTERN carries the `g` flag and is a module-level constant —
      // reset lastIndex before each parse call so consecutive invocations
      // don't skip matches based on a previous call's cursor position.
      BLOCK_PATTERN.lastIndex = 0
      while ((match = BLOCK_PATTERN.exec(responseText)) !== null) {
        const blockKey = `block${match[1].toUpperCase()}`
        const blockTitle = match[2].trim()
        const blockContent = match[3].trim()

        const scoreMatch = blockContent.match(BLOCK_SCORE_PATTERN)
        const score = scoreMatch ? parseFloat(scoreMatch[1]) : null

        blocks[blockKey] = {
          title: blockTitle,
          content: blockContent,
          score,
        }
      }

      if (Object.keys(blocks).length === 0) {
        return Evaluation.parseError(responseText)
      }

      const overallMatch = responseText.match(OVERALL_SCORE_PATTERN)
      const overallScore = overallMatch ? parseFloat(overallMatch[1]) : null

      return Evaluation.fromBlocks(blocks, overallScore, responseText)
    } catch (err) {
      // T-58: parse error guard — never lose the row, never throw
      return Evaluation.parseError(responseText)
    }
  },
}
