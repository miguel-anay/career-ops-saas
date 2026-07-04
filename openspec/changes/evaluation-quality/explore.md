# Exploration: evaluation-quality

> Mirror of engram `sdd/evaluation-quality/explore` (obs #364), 2026-07-03.

## Problem

The SaaS evaluator produced a useless 1.3/5 evaluation for a Bumeran job while the user's previous agent-based workflow produced a rich 3.9/5 report for the same job. Verified root causes: (1) manual/email-ingested jobs never get `scraped_content` fetched (only the 6 ATS providers populate it), (2) `users.cv_markdown` NULL / `profile_json = {}` at evaluation time, (3) zero input guards — tokens burned on empty CV + empty JD.

## Current State

Traced flow: `POST /api/jobs/{id}/evaluate` (`api/internal/evaluate/handler.go:38`) → `EnqueueEvaluation` (`api/internal/evaluate/service.go:50`, only ownership + usage limit, **zero content guards**) → pg-boss `evaluate-job` → `worker/jobs/evaluate.mjs:31` → `worker/application/EvaluateJob.mjs:30` → `worker/adapters/LlmEvaluator.mjs:33` → `worker/lib/prompt.mjs:17-127`.

Root cause lines: `worker/lib/prompt.mjs:36` (`cvMarkdown = user.cv_markdown || ''`) and `:101` (falls back to `'(No scraped content available...)'` and still calls the LLM).

The prompt (`worker/lib/prompt.mjs:49-79`) **already implements the 7-block A-G contract** — richness gap is STAR mapping + negotiation advice (prompt-text-only) and inputs, not structure.

**Independent bug (high ROI):** `worker/adapters/PgEvaluationRepository.mjs:49` stores `blocks_json` as an OBJECT, but `web/app/jobs/[id]/page.tsx:214` expects an ARRAY (`report.blocks_json.map(...)`; `web/__tests__/app/jobs-detail.test.tsx:59-64` mocks it as array). The collapsible A-G UI has likely never rendered for any evaluation.

**Job content acquisition:** only the 6 ATS providers (`worker/jobs/scan.mjs:11-18`) populate `scraped_content`. `AddManual` (`api/internal/jobs/service.go:32-70`) accepts any `https://` URL, leaves `scraped_content=NULL`; `detectPlatform` (line 118) only knows the 6 ATS hosts (Bumeran → `unknown`). No generic page-fetch exists. **Playwright is already a dependency** (`worker/package.json:20`, used by `worker/jobs/pdf.mjs`) — reusable, zero new deps. SSRF allowlist patterns to mirror: `worker/lib/gmail.mjs:8,13-24` and `worker/lib/url-normalize.mjs:24-29` (`HOST_RULES` already has bumeran/linkedin/indeed/computrabajo regexes).

**CV precondition:** ingest-cv flow intact on this branch (pg-boss v10 + tenantQuery fixes applied). Gap is UX/guard only: no "CV missing" state in `web/app/jobs/[id]/page.tsx`; API never blocks evaluation. Exact 422 pattern to mirror: `api/internal/emailingest/service.go:21-24` (`ErrGmailNotConnected`) + `handler.go:48-51`.

**Enrichment:** no search API/tool-use in the worker; single-shot LLM. Free win: pass `job.received_at` age into the prompt as a real data point for Block G. Full web-search enrichment needs new infra + per-eval cost — conflicts with 0-token-default preference.

## Affected Areas

- `api/internal/evaluate/service.go` / `handler.go` — guard errors + 422 mapping + blocks_json shape fix
- `worker/lib/prompt.mjs` — posting-age data point, STAR/negotiation prompt text (no schema change)
- `api/internal/jobs/service.go` — natural enqueue point for a content-fetch job
- `worker/index.mjs` — registry for a 6th `fetch-job-content` job type
- `web/app/jobs/[id]/page.tsx` + tests — CV/JD-missing states, blocks_json rendering

## Approaches (JD fetch)

1. **Generic HTTP fetch + text extraction** — reuses `fetchText`; fails on JS-rendered SPA boards (Bumeran unverified — needs a 2-min spike). Low effort if static, else dead end.
2. **Playwright render (reuse existing dep)** — zero new deps, uniform across SPA boards, needs SSRF allowlist. Medium effort. **Recommended** after spike rules out (1).
3. **Per-board detail parsers** — highest fidelity and maintenance; doesn't generalize. High effort.

## Recommended Slices

1. Input guards (`ErrCVMissing`/`ErrJobContentMissing` → 422, mirror `ErrGmailNotConnected`) + `blocks_json` array shape fix — stops token burn, fixes UI bug.
2. Web UX for the new 422s (CV-missing / JD-unavailable states).
3. `fetch-job-content` pg-boss job (Playwright + SSRF allowlist), enqueued from AddManual/email-ingest when `scraped_content` NULL — separate, larger PR.
4. Prompt-only enrichment (posting-age signal, STAR/negotiation text) — cheap.
5. **Out of scope:** web-search enrichment, live apply-button verification.

## Risks

- SSRF if fetch guard skipped (AddManual accepts arbitrary URLs).
- Unverified SPA-vs-SSR for Bumeran/LinkedIn — spike before committing slice 3.
- Playwright must run as an async job, not inline in evaluate-job.
- Scope creep from broad framing; slice 3 threatens the 400-line PR budget.

## Ready for Proposal

Yes — recommend MVP = slices 1-2, slices 3-4 as follow-on phase given Playwright+SSRF size/risk.
