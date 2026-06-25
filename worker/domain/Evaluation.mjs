// Evaluation — a pure domain type representing the parsed result of an
// Anthropic evaluation response.
//
// There are exactly two ways to construct an Evaluation:
//   - fromBlocks(blocks, score, contentMd) — successful parse with at least
//     one extracted block.
//   - parseError(rawText)                  — T-58 sentinel: the response
//     could not be parsed (empty, malformed, or an internal parser
//     exception), but the row must NEVER be lost.
//
// Both factories guarantee a persistable shape (blocks/score/contentMd are
// always present), so it is impossible to construct an Evaluation that
// violates T-58 by construction — there is no public constructor.

const PARSE_ERROR_STATUS_NOTE = 'Evaluation completed (parse error in blocks)'

export class Evaluation {
  #blocks
  #score
  #contentMd
  #isParseError

  /** @private — use `Evaluation.fromBlocks` or `Evaluation.parseError` */
  constructor(blocks, score, contentMd, isParseError) {
    if (Evaluation.#constructing !== true) {
      throw new TypeError(
        'Evaluation has no public constructor — use Evaluation.fromBlocks(...) or Evaluation.parseError(...)'
      )
    }

    this.#blocks = blocks
    this.#score = score
    this.#contentMd = contentMd
    this.#isParseError = isParseError
  }

  static #constructing = false

  static #build(blocks, score, contentMd, isParseError) {
    Evaluation.#constructing = true
    try {
      return new Evaluation(blocks, score, contentMd, isParseError)
    } finally {
      Evaluation.#constructing = false
    }
  }

  /**
   * @param {object} blocks - extracted A-G blocks; must contain at least one key
   * @param {number | null} score - overall score, or null if not found
   * @param {string} contentMd - raw response text
   * @returns {Evaluation}
   */
  static fromBlocks(blocks, score, contentMd) {
    if (!blocks || typeof blocks !== 'object' || Object.keys(blocks).length === 0) {
      throw new TypeError('Evaluation.fromBlocks requires a non-empty blocks object (T-58 invariant)')
    }
    if (typeof contentMd !== 'string') {
      throw new TypeError('Evaluation.fromBlocks requires contentMd to be a string (T-58 invariant)')
    }

    return Evaluation.#build(blocks, score, contentMd, false)
  }

  /**
   * @param {string} rawText - the unparseable raw response text
   * @returns {Evaluation}
   */
  static parseError(rawText) {
    const contentMd = rawText || ''
    return Evaluation.#build({ parse_error: true, raw: rawText }, null, contentMd, true)
  }

  get blocks() {
    return this.#blocks
  }

  get score() {
    return this.#score
  }

  get contentMd() {
    return this.#contentMd
  }

  get isParseError() {
    return this.#isParseError
  }

  get statusNote() {
    return this.#isParseError ? PARSE_ERROR_STATUS_NOTE : null
  }
}
