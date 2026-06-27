import { tenantQuery } from '../lib/db.mjs'
import { buildEvaluationPrompt } from '../lib/prompt.mjs'
import { evaluate as anthropicEvaluate } from '../lib/anthropic.mjs'
import { evaluate as openaiCompatEvaluate } from '../lib/openai-compat.mjs'
import { LlmEvaluator } from '../adapters/LlmEvaluator.mjs'
import { PgEvaluationRepository } from '../adapters/PgEvaluationRepository.mjs'
import { EvaluateJob } from '../application/EvaluateJob.mjs'

/**
 * Handle an "evaluate-job" pg-boss job.
 *
 * Payload: { user_id, job_id }
 *
 * This is a thin wiring shim — it constructs the real adapters (LlmEvaluator,
 * PgEvaluationRepository) and delegates all orchestration to the `EvaluateJob`
 * application use case (`worker/application/EvaluateJob.mjs`). See
 * `worker/domain/EvaluationParser.mjs` for the response-parsing logic (T-58
 * parse-error guard: never lose the row) and
 * `worker/adapters/PgEvaluationRepository.mjs` for the exact 4 SQL writes
 * (applications, reports, usage, jobs).
 *
 * Model provider is selected by the `EVALUATOR` env var. Default 'anthropic' uses
 * native Claude (keeps prompt caching). Any other value (qwen | deepseek |
 * minimax | openai) routes through the OpenAI-compatible client, configured via
 * LLM_API_KEY / LLM_MODEL / LLM_BASE_URL. The LlmEvaluator adapter is
 * provider-agnostic and reads either response shape.
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - Job payload
 */
export async function handleEvaluateJob(job) {
  const { user_id, job_id } = job.data

  const useAnthropic = (process.env.EVALUATOR || 'anthropic') === 'anthropic'
  const evaluate = useAnthropic ? anthropicEvaluate : openaiCompatEvaluate

  const evaluator = new LlmEvaluator({ tenantQuery, buildEvaluationPrompt, evaluate })
  const repository = new PgEvaluationRepository({ tenantQuery })
  const useCase = new EvaluateJob({ evaluator, repository })

  await useCase.run({ userId: user_id, jobId: job_id })
}
