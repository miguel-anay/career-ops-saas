# Spec Delta: evaluation-quality

## Domain: evaluation-input-guards (NEW)

### Requirement: Block evaluation when CV is missing

The system MUST reject `POST /api/jobs/{id}/evaluate` with HTTP 422 and machine-readable code `cv_missing` when `users.cv_markdown` is NULL or empty for the requesting user, BEFORE enqueueing `evaluate-job` or spending any LLM tokens. Mirrors `emailingest.ErrGmailNotConnected` (`api/internal/emailingest/service.go:21-24`, `handler.go:48-51`).

#### Scenario: User has no CV

- GIVEN an authenticated user whose `users.cv_markdown` is NULL
- WHEN they POST `/api/jobs/{id}/evaluate` for a job they own
- THEN the API responds 422 with `{"error": "...", "code": "cv_missing"}`
- AND no pg-boss `evaluate-job` is enqueued
- AND zero LLM tokens are spent

#### Scenario: User has a CV

- GIVEN an authenticated user whose `users.cv_markdown` is a non-empty string
- WHEN they POST `/api/jobs/{id}/evaluate` for a job with non-empty `scraped_content`
- THEN the guard passes and evaluation proceeds to the existing usage-limit check and enqueue

### Requirement: Block evaluation when job description content is missing

The system MUST reject `POST /api/jobs/{id}/evaluate` with HTTP 422 and code `job_content_missing` when the target job's `scraped_content` is NULL or empty, BEFORE enqueue. This guard applies regardless of ingestion source (manual, email, or ATS scan) — the check is purely on `scraped_content` presence.

#### Scenario: Manual/email job with no scraped content

- GIVEN a job added manually or via email ingestion whose `scraped_content` is NULL
- WHEN the owning user POSTs `/api/jobs/{id}/evaluate`
- THEN the API responds 422 with `{"error": "...", "code": "job_content_missing"}`
- AND no `evaluate-job` is enqueued

#### Scenario: ATS-scraped job with content present

- GIVEN a job populated by one of the 6 ATS providers with non-empty `scraped_content`
- WHEN the owning user POSTs `/api/jobs/{id}/evaluate` and their CV is present
- THEN the guard passes and evaluation is enqueued as before

### Requirement: Existing error precedence is preserved

The new content guards MUST run only after existing ownership (`ErrNotFound`) and usage-limit (`ErrUsageLimitExceeded`) checks continue to behave identically for jobs that fail those checks — this change MUST NOT alter their status codes or response shape (404 `not_found`, 402 `usage_limit_exceeded`).

#### Scenario: Job not owned by user

- GIVEN a job ID that exists but belongs to another user
- WHEN the requester POSTs `/api/jobs/{id}/evaluate`
- THEN the API responds 404 with code `not_found`, unaffected by the CV/JD guards

## Domain: worker-evaluate-job (MODIFIED)

### Requirement: `blocks_json` persisted as an array

`PgEvaluationRepository.save` MUST persist `evaluation.blocks` such that `reports.blocks_json` is read back by the API and rendered by the web client as a JSON array of block objects (each with at least a `label` and content), not a plain object. (Previously: `blocks_json` was written as an object keyed by block letter, which the web client's `.map()` silently ignored via the `report.blocks_json.length > 0` guard, so the A-G collapsible UI never rendered.)

#### Scenario: New evaluation persists array-shaped blocks

- GIVEN a completed LLM evaluation with 7 parsed blocks (A-G)
- WHEN `PgEvaluationRepository.save` writes the report
- THEN `reports.blocks_json` is valid JSON array of length 7
- AND each array element carries a block label resolvable by the web client

#### Scenario: Re-evaluation replaces the prior report

- GIVEN an application already has one `reports` row (from a prior evaluation)
- WHEN the job is re-evaluated (e.g., after the user's CV is later ingested)
- THEN the stale report row is deleted and a new one is inserted (per the existing DELETE-then-INSERT flow in `PgEvaluationRepository.save`)
- AND `GetReportByApplicationID` (LIMIT 1, no ORDER BY) returns exactly the new array-shaped report, since only one row exists for the application

#### Scenario: LLM output fails to parse into blocks

- GIVEN the LLM response cannot be parsed into the 7-block structure
- WHEN `PgEvaluationRepository.save` runs
- THEN `blocks_json` is still persisted as a value the web client can safely check with `Array.isArray` / `.length` (e.g., an empty array), never a bare object
- AND the report row is still written (`content_md` retains the raw/fallback text)

### Requirement: Prompt includes posting-age signal and STAR/negotiation guidance

`worker/lib/prompt.mjs` MUST include the job's `received_at` age (time since the job was ingested, in human-readable form) as a data point available to Block G, and MUST instruct the model to map CV experience to STAR-format achievements and to include negotiation guidance in its output. This is a prompt-text-only change: the A-G block schema and field names MUST remain unchanged.

#### Scenario: Prompt built for a job with a known `received_at`

- GIVEN a job row with a non-null `received_at` timestamp
- WHEN `buildEvaluationPrompt` constructs the messages array
- THEN the user-content message includes the posting age (e.g., "posted 5 days ago")
- AND the system prompt instructs STAR-mapping and negotiation-guidance generation
- AND the resulting prompt still requests exactly 7 blocks (A-G) with the same field names as before

## Domain: web-frontend-structure (MODIFIED)

### Requirement: Job-detail page renders CV-missing and JD-unavailable states

`web/app/jobs/[id]/page.tsx` MUST detect the `cv_missing` and `job_content_missing` 422 codes returned from `POST /api/jobs/{id}/evaluate` and render a distinct, actionable message for each, instead of a generic error. (Previously: no distinction existed; any evaluate failure showed the same generic error state.)

#### Scenario: Evaluate fails with cv_missing

- GIVEN the user clicks "Evaluate" on a job
- WHEN the API responds 422 with code `cv_missing`
- THEN the page shows a CV-missing message telling the user to add a CV
- AND the message is structured so a future link to the CV page (issue #45) can be added without a UI rewrite

#### Scenario: Evaluate fails with job_content_missing

- GIVEN the user clicks "Evaluate" on a manually-added job with no scraped content
- WHEN the API responds 422 with code `job_content_missing`
- THEN the page shows a JD-unavailable message distinct from the CV-missing message

### Requirement: Job-detail page renders array-shaped `blocks_json`

The report-rendering section of `web/app/jobs/[id]/page.tsx` MUST continue to treat `report.blocks_json` as an array (`Array.isArray(report.blocks_json) && report.blocks_json.length > 0`) and MUST render each block as a collapsible section, now actually receiving array data end-to-end from the fixed repository write.

#### Scenario: Report with 7 array blocks renders

- GIVEN a report whose `blocks_json` is a 7-element array
- WHEN the job-detail page loads the report
- THEN 7 collapsible sections render, one per block, labeled A-G

#### Scenario: Backward-compat — pre-existing object-shaped `blocks_json` row

- GIVEN a `reports` row written before this change, where `blocks_json` is a plain object (not an array)
- WHEN the job-detail page loads that report
- THEN the page does not throw (guarded by the `Array.isArray` check) and instead renders no blocks
- AND re-evaluating that job (see worker-evaluate-job re-evaluation scenario) produces a new array-shaped row that then renders correctly
