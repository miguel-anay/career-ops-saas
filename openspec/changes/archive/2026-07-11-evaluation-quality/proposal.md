# Proposal: Evaluation Quality

## Intent

The evaluator burns tokens producing garbage (verified 1.3/5 on a Bumeran job vs 3.9/5 from the user's prior agent workflow). Root causes are all input/plumbing, not model quality: (1) manual/email jobs never get `scraped_content` — only the 6 ATS providers populate it; (2) evaluations run with empty CV / `{}` profile; (3) zero content guards, so the LLM is called on empty inputs (`worker/lib/prompt.mjs:101` falls back to a placeholder and calls anyway); (4) an independent bug where `blocks_json` is stored as an object but the A-G UI expects an array (`PgEvaluationRepository.mjs:49` vs `web/app/jobs/[id]/page.tsx:214`) — the collapsible report has likely never rendered.

## Scope

### In Scope
- **Input guards** (`ErrCVMissing` / `ErrJobContentMissing` → 422): block evaluation with 0 tokens when CV or scraped JD is absent. Mirror `emailingest.ErrGmailNotConnected` (`service.go:21`, `handler.go:48`).
- **`blocks_json` array fix**: store as array so A-G renders; aligns with existing test mock.
- **Web 422 UX**: CV-missing and JD-unavailable states in `web/app/jobs/[id]/page.tsx`.
- **Prompt-only enrichment** (zero deps, 0-token-default preserved): pass `job.received_at` age as a Block-G signal; add STAR-mapping + negotiation prompt text (existing A-G schema unchanged).

### Out of Scope
- **`fetch-job-content` (Playwright JD scraping)** — deferred to a separate change `job-content-fetch` (see Scope Decision).
- Web-search / salary enrichment; live apply-button verification; LLM tool-use. All conflict with the 0-token default and add per-eval cost/infra.
- **CV visibility UI** (`GET /api/me/cv` + page rendering the ingested `cv_markdown`/`profile_json`) — tracked as issue [#45](https://github.com/miguel-anay/career-ops-saas/issues/45). The CV-missing 422 state shipped here should deep-link to it once built.

## Scope Decision: split, not phased

This change ships slices 1, 2, 4 — all zero-new-dep, zero-new-infra, fit one ≤400-line PR (or two small chained: Go+worker, then web). Slice 3 (Playwright fetch) becomes a **separate future change** because it introduces a new trust boundary (SSRF on arbitrary user URLs), needs an unresolved SPA-vs-SSR spike, adds a 6th async pg-boss job type, and alone threatens the 400-line budget. Slice 1's `ErrJobContentMissing` guard is the gate that slice 3 later satisfies — so this change intentionally makes manual/Bumeran jobs return an actionable 422 until `job-content-fetch` ships. That is the correct 0-token tradeoff: block clearly, never burn tokens on a garbage result.

## Capabilities

### New Capabilities
- `evaluation-input-guards`: API rejects evaluation with 422 + actionable message when CV or JD content is missing, before any enqueue/LLM call.

### Modified Capabilities
- `worker-evaluate-job`: R1.2 changes — `blocks_json` persisted as an array; prompt gains posting-age + STAR/negotiation text (A-G schema preserved).
- `web-frontend-structure`: job-detail page renders CV-missing / JD-unavailable 422 states and the array-shaped A-G blocks.

## Approach

Guard in `evaluate.Service.EnqueueEvaluation` (read job + user, check `scraped_content` / `cv_markdown` non-empty inside the existing tenant tx) → typed error → 422 in handler. Flip `blocks_json` to array at the repo write. Extend prompt text only. Web maps the new 422 codes to states.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `api/internal/evaluate/service.go` | Modified | CV/JD guards + typed errors |
| `api/internal/evaluate/handler.go` | Modified | 422 mapping |
| `worker/adapters/PgEvaluationRepository.mjs` | Modified | `blocks_json` → array |
| `worker/lib/prompt.mjs` | Modified | posting-age + STAR/negotiation text |
| `web/app/jobs/[id]/page.tsx` + tests | Modified | 422 states, array render |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Guard blocks legit jobs (empty scraped_content on evaluable ATS job) | Med | Guard on CV always; JD guard only when `scraped_content` NULL/empty — ATS jobs already populate it |
| PR size >400 lines | Med | Chain: PR-A Go+worker, PR-B web |
| blocks_json shape change breaks existing rows | Low | Old rows re-evaluate; UI tolerates missing/array |
| (deferred) SSRF, SPA spike | — | Owned by `job-content-fetch` change |

## Rollback Plan

Revert per-PR: the Go guard, the repo one-liner, and prompt text are independent commits. Reverting the guard restores prior (token-burning) behavior; reverting the `blocks_json` change restores the object shape. No schema migration involved.

## Dependencies

- None new. Playwright (needed by the deferred slice 3) already present in `worker/package.json`.

## Success Criteria

- [x] Evaluating a job with no CV returns 422 with an actionable message and burns 0 tokens.
- [x] Evaluating a manual/email job with NULL `scraped_content` returns 422 (JD unavailable), 0 tokens.
- [x] A-G collapsible blocks actually render on the job-detail page.
- [x] Prompt includes posting-age + STAR/negotiation guidance; A-G schema unchanged.
- [ ] (Future `job-content-fetch`) a Bumeran job added by URL gets its JD within N minutes, then evaluates without a 422.
