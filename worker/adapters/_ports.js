// Port contracts for the evaluate-job use case.
//
// This file is documentation-only — pure JSDoc @typedef annotations, mirroring
// the style of `worker/providers/_types.js`. The project is plain ESM
// JavaScript with no build step; adapter authors can reference these types via
// `/** @typedef {import('./_ports.js').EvaluatorPort} EvaluatorPort */` at the
// top of a `// @ts-check`-enabled file to get IDE hints. The runtime contract
// is duck-typed and enforced by the caller (constructor DI in
// `worker/application/EvaluateJob.mjs`), not by these annotations.
//
// Files prefixed with _ are never loaded as adapters/providers automatically.

/**
 * Calls the LLM (or any evaluator) to produce a raw evaluation response for
 * a given user/job pair. Implementations own prompt-building and the actual
 * model call; the use case only sees the resulting raw text.
 *
 * @typedef {object} EvaluatorPort
 * @property {(userId: string, jobId: string) => Promise<string>} evaluate
 */

/**
 * Persists a parsed `Evaluation` (see `worker/domain/Evaluation.mjs`) for a
 * given user/job pair. Implementations own all storage side effects (SQL
 * writes, external APIs, etc.) — the use case only calls `save` once with the
 * domain object.
 *
 * @typedef {object} EvaluationRepository
 * @property {(userId: string, jobId: string, evaluation: import('../domain/Evaluation.mjs').Evaluation) => Promise<void>} save
 */

export {};
