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

const OVERALL_SCORE_PATTERN = /\*\*Overall Score:\s*(\d+(?:\.\d+)?)\s*\/\s*5\*\*/i

export const BLOCK_LETTERS = ['A', 'B', 'C', 'D', 'E', 'F', 'G']

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
      const blocksByLetter = {}

      let match
      // BLOCK_PATTERN carries the `g` flag and is a module-level constant —
      // reset lastIndex before each parse call so consecutive invocations
      // don't skip matches based on a previous call's cursor position.
      BLOCK_PATTERN.lastIndex = 0
      while ((match = BLOCK_PATTERN.exec(responseText)) !== null) {
        const letter = match[1].toUpperCase()
        const blockTitle = match[2].trim()
        const blockContent = match[3].trim()

        // Per-block score is parsed but dropped from the emitted shape
        // (YAGNI — no consumer reads it; the overall score below is kept).
        blocksByLetter[letter] = { label: blockTitle, content: blockContent }
      }

      if (Object.keys(blocksByLetter).length === 0) {
        return Evaluation.parseError(responseText)
      }

      // The LLM may emit blocks out of order; collect by letter above, then
      // emit a fixed A→G array so the web client's collapsible sections
      // render in a stable order regardless of model output order.
      const blocks = BLOCK_LETTERS.filter((letter) => blocksByLetter[letter]).map(
        (letter) => blocksByLetter[letter]
      )

      const overallMatch = responseText.match(OVERALL_SCORE_PATTERN)
      const overallScore = overallMatch ? parseFloat(overallMatch[1]) : null

      return Evaluation.fromBlocks(blocks, overallScore, responseText)
    } catch (err) {
      // T-58: parse error guard — never lose the row, never throw
      return Evaluation.parseError(responseText)
    }
  },
}
