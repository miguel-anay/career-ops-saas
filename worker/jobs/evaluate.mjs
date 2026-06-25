import { tenantQuery } from '../lib/db.mjs'
import { buildEvaluationPrompt } from '../lib/prompt.mjs'
import { evaluate } from '../lib/anthropic.mjs'
import { AnthropicEvaluator } from '../adapters/AnthropicEvaluator.mjs'
import { PgEvaluationRepository } from '../adapters/PgEvaluationRepository.mjs'
import { EvaluateJob } from '../application/EvaluateJob.mjs'

/**
 * Handle an "evaluate-job" pg-boss job.
 *
 * Payload: { user_id, job_id }
 *
 * This is a thin wiring shim — it constructs the real adapters
 * (AnthropicEvaluator, PgEvaluationRepository) and delegates all
 * orchestration to the `EvaluateJob` application use case
 * (`worker/application/EvaluateJob.mjs`). See `worker/domain/EvaluationParser
 * .mjs` for the response-parsing logic (T-58 parse-error guard: never lose
 * the row) and `worker/adapters/PgEvaluationRepository.mjs` for the exact 4
 * SQL writes (applications, reports, usage, jobs).
 *
 * @param {object} job - pg-boss job object
 * @param {object} job.data - Job payload
 */
export async function handleEvaluateJob(job) {
  const { user_id, job_id } = job.data

  const evaluator = new AnthropicEvaluator({ tenantQuery, buildEvaluationPrompt, evaluate })
  const repository = new PgEvaluationRepository({ tenantQuery })
  const useCase = new EvaluateJob({ evaluator, repository })

  await useCase.run({ userId: user_id, jobId: job_id })
}
