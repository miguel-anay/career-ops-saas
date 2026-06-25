// AnthropicEvaluator — adapter implementing EvaluatorPort by wrapping
// `buildEvaluationPrompt` (worker/lib/prompt.mjs) and `evaluate`
// (worker/lib/anthropic.mjs). This is a behavior-preserving extraction of the
// prompt-building + model-call steps that previously lived inline in
// `worker/jobs/evaluate.mjs`'s `handleEvaluateJob`.

export class AnthropicEvaluator {
  #tenantQuery
  #buildEvaluationPrompt
  #evaluate

  /**
   * @param {object} deps
   * @param {Function} deps.tenantQuery - RLS-scoped query function (worker/lib/db.mjs)
   * @param {Function} deps.buildEvaluationPrompt - worker/lib/prompt.mjs buildEvaluationPrompt
   * @param {Function} deps.evaluate - worker/lib/anthropic.mjs evaluate
   */
  constructor({ tenantQuery, buildEvaluationPrompt, evaluate }) {
    this.#tenantQuery = tenantQuery
    this.#buildEvaluationPrompt = buildEvaluationPrompt
    this.#evaluate = evaluate
  }

  /**
   * @param {string} userId
   * @param {string} jobId
   * @returns {Promise<string>} raw Anthropic response text
   */
  async evaluate(userId, jobId) {
    const promptData = await this.#buildEvaluationPrompt(userId, jobId, { tenantQuery: this.#tenantQuery })

    const response = await this.#evaluate(promptData.system, promptData.messages?.[0]?.content || '')

    return response.content?.[0]?.text || ''
  }
}
