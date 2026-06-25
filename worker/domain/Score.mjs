// Score — a pure value object for an evaluation score in the inclusive
// [0, 5] range, or `null` when unscored (e.g. parse error).
//
// Deliberately does NOT implement threshold/recommendation logic
// (`isRecommended()`, `RECOMMEND_THRESHOLD`) — that behavior does not exist
// today and must not be introduced by this refactor.

export class Score {
  #value

  constructor(value) {
    this.#value = value
  }

  /**
   * @param {number | null} value
   * @returns {Score}
   * @throws {TypeError} if value is neither `null` nor a finite number
   * @throws {RangeError} if value is a number outside [0, 5]
   */
  static of(value) {
    if (value === null) {
      return new Score(null)
    }

    if (typeof value !== 'number' || Number.isNaN(value)) {
      throw new TypeError(`Score.of expects a number or null, got ${typeof value}`)
    }

    if (value < 0 || value > 5) {
      throw new RangeError(`Score.of expects a value within [0, 5], got ${value}`)
    }

    return new Score(value)
  }

  get value() {
    return this.#value
  }
}
