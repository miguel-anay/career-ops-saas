// LlmEvaluator — provider-agnostic adapter implementing EvaluatorPort. Builds the
// prompt and calls an injected `evaluate` function (Anthropic native OR any
// OpenAI-compatible client), then extracts the raw response text regardless of
// provider shape:
//   - Anthropic Messages:  response.content[0].text
//   - OpenAI / compatible: response.choices[0].message.content
//
// Provider selection lives in the wiring (worker/jobs/evaluate.mjs); this adapter
// stays agnostic. Replaces the former AnthropicEvaluator.

export class LlmEvaluator {
  #tenantQuery
  #buildEvaluationPrompt
  #evaluate

  /**
   * @param {object} deps
   * @param {Function} deps.tenantQuery - RLS-scoped query function (worker/lib/db.mjs)
   * @param {Function} deps.buildEvaluationPrompt - worker/lib/prompt.mjs buildEvaluationPrompt
   * @param {Function} deps.evaluate - a model call: anthropic.mjs or openai-compat.mjs `evaluate`
   */
  constructor({ tenantQuery, buildEvaluationPrompt, evaluate }) {
    this.#tenantQuery = tenantQuery
    this.#buildEvaluationPrompt = buildEvaluationPrompt
    this.#evaluate = evaluate
  }

  /**
   * @param {string} userId
   * @param {string} jobId
   * @returns {Promise<string>} raw model response text
   */
  async evaluate(userId, jobId) {
    const promptData = await this.#buildEvaluationPrompt(userId, jobId, { tenantQuery: this.#tenantQuery })

    const response = await this.#evaluate(promptData.system, promptData.messages?.[0]?.content || '')

    return response.content?.[0]?.text ?? response.choices?.[0]?.message?.content ?? ''
  }
}
