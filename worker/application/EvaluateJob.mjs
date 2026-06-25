// EvaluateJob — application use case orchestrating a single job evaluation.
//
// Pure orchestration: no regex, no SQL. Depends only on the two ports
// declared in `worker/adapters/_ports.js` (constructor-injected, duck-typed),
// and on the pure `EvaluationParser` domain service. This is what makes the
// use case testable with fakes instead of `vi.mock`-ing relative paths.

import { EvaluationParser } from '../domain/EvaluationParser.mjs'

export class EvaluateJob {
  #evaluator
  #repository

  /**
   * @param {object} deps
   * @param {import('../adapters/_ports.js').EvaluatorPort} deps.evaluator
   * @param {import('../adapters/_ports.js').EvaluationRepository} deps.repository
   */
  constructor({ evaluator, repository }) {
    this.#evaluator = evaluator
    this.#repository = repository
  }

  /**
   * @param {object} params
   * @param {string} params.userId
   * @param {string} params.jobId
   * @returns {Promise<void>}
   */
  async run({ userId, jobId }) {
    const rawText = await this.#evaluator.evaluate(userId, jobId)
    const evaluation = EvaluationParser.parse(rawText)
    await this.#repository.save(userId, jobId, evaluation)
  }
}
