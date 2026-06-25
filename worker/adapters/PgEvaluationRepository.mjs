// PgEvaluationRepository — adapter implementing EvaluationRepository by
// reproducing, EXACTLY, the 4 SQL writes (same order, same column values)
// that previously lived inline in `worker/jobs/evaluate.mjs`'s
// `handleEvaluateJob`:
//   1. INSERT into applications (score, status 'Evaluated', notes)
//   2. INSERT into reports (content_md, blocks_json)
//   3. UPSERT usage (evaluations_count +1 for current month)
//   4. UPDATE jobs.status to 'evaluated'
//
// All writes go through the RLS-scoped `tenantQuery` (worker/lib/db.mjs).

export class PgEvaluationRepository {
  #tenantQuery

  /**
   * @param {object} deps
   * @param {Function} deps.tenantQuery - RLS-scoped query function (worker/lib/db.mjs)
   */
  constructor({ tenantQuery }) {
    this.#tenantQuery = tenantQuery
  }

  /**
   * @param {string} userId
   * @param {string} jobId
   * @param {import('../domain/Evaluation.mjs').Evaluation} evaluation
   * @returns {Promise<void>}
   */
  async save(userId, jobId, evaluation) {
    const currentMonth = new Date().toISOString().slice(0, 7) // YYYY-MM

    // 1. INSERT into applications
    const appResult = await this.#tenantQuery(
      userId,
      `INSERT INTO applications (user_id, job_id, score, status, notes)
       VALUES ($1::uuid, $2::uuid, $3, 'Evaluated', $4)
       RETURNING id`,
      [userId, jobId, evaluation.score, evaluation.statusNote]
    )

    const applicationId = appResult.rows[0]?.id

    // 2. INSERT into reports (always — T-58 ensures blocks_json is set even on parse error)
    await this.#tenantQuery(
      userId,
      `INSERT INTO reports (user_id, application_id, content_md, blocks_json)
       VALUES ($1::uuid, $2::uuid, $3, $4::jsonb)
       RETURNING id`,
      [userId, applicationId, evaluation.contentMd, JSON.stringify(evaluation.blocks)]
    )

    // 3. UPSERT usage — increment evaluations_count for current month
    await this.#tenantQuery(
      userId,
      `INSERT INTO usage (user_id, month, evaluations_count, pdfs_count)
       VALUES ($1::uuid, $2, 1, 0)
       ON CONFLICT (user_id, month)
       DO UPDATE SET evaluations_count = usage.evaluations_count + 1`,
      [userId, currentMonth]
    )

    // 4. UPDATE jobs.status to 'evaluated'
    await this.#tenantQuery(
      userId,
      `UPDATE jobs SET status = 'evaluated' WHERE id = $1::uuid AND user_id = $2::uuid`,
      [jobId, userId]
    )
  }
}
