# Exploration: worker DDD-lite + web feature-folder ("screaming") refactor

> Mirror of engram `sdd/worker-ddd-web-screaming/explore` (hybrid). Behavior-preserving internal architecture refactor of two services.

## Current State

- **worker/jobs/evaluate.mjs** (145 lines): `parseEvaluationResponse` (regex A-G block + score parse + T-58 never-lose-the-row guard) + `handleEvaluateJob` (build prompt → Anthropic → parse → 4 sequential `tenantQuery` writes: applications, reports, usage upsert, jobs.status). **No `RECOMMEND_THRESHOLD=4` logic exists today — the Score-threshold rule is NEW behavior to design, not an extraction.**
- **worker/jobs/ingest-cv.mjs** (115 lines): identical shape — `parseIngestResponse` with the same never-throw guard, documented as "mirrors parseEvaluationResponse." Duplicated pattern across two files.
- **worker/jobs/scan.mjs** (223 lines): already delegates to `providers/*.mjs` — confirmed ports-and-adapters (`_types.js` defines the `Provider` contract id/detect/fetch). scan.mjs itself is pure I/O orchestration + SQL + NOTIFY — no business-rule logic to extract.
- **worker/jobs/pdf.mjs** (208 lines): mechanical `buildCVHtml` templating + reads/write — string formatting, not domain.
- **Worker tests**: `tests/jobs/evaluate.test.mjs` mocks exact relative paths and asserts `tenantQuery` called **exactly 4 times in order**. `scan.test.mjs` similar + mocks dynamic provider imports. Fragile to internal restructuring BY DESIGN — must be rewritten to assert through new seams, not relocated.
- **web/app/page.tsx** (277 lines) and **companies/page.tsx** (253 lines): confirmed fat — local interfaces, fetch, tables, forms/dialogs, WebSocket inline. `web/hooks/{useScanProgress,useJobProgress}.ts` global but each used by one feature. `web/lib/api.ts` (106 lines) mixes generic client (apiGet/Post/Patch/Delete + token refresh) with CV-ingest-specific exports (postIngest, getIngestion). `web/components/ui/*` pure shadcn primitives, correctly global.
- **Critical test gap**: no test for `companies/page.tsx` or `jobs/[id]/page.tsx`. Existing web tests mock `../../lib/api` and `../../hooks/useScanProgress` by relative path — moving files breaks mocks unless updated in the same PR.

## Approaches

**Worker**: (1) Full DDD — over-engineered for a 145-line script (High). (2) **DDD-lite** (domain/application/adapters, extends existing `providers/` ports pattern) — isolates the real rule (T-58), gives the new Score rule a home (Medium). (3) Extract only the pure parser, no layering — cheapest, doesn't fix orchestration/SQL mixing (Low).

**Web**: (1) Full features/ incl. routes — not viable (Next.js requires `app/<route>/page.tsx`). (2) **Feature-folder colocation** (features/<name>/{components,hooks,api,types} + thin route composers) — fixes confirmed smells (Medium). (3) Split only the two fattest pages — fastest, leaves coupling (Low).

## Recommendation

- **Worker — DDD-lite, scoped to evaluate-job ONLY.** Earned: T-58 is real duplicated business logic, untestable in isolation today, and the new Score/threshold rule is exactly when extraction stops being cosmetic. scan.mjs/pdf.mjs do NOT earn it (pure I/O). ingest-cv.mjs has the same shape → tracked **fast-follow**, not bundled now.
- **Web — feature-folder colocation, sequenced**: split `page.tsx` + `companies/page.tsx` first (PR1), migrate hooks + split `lib/api.ts` second (PR2). Worth doing now (`lib/api.ts` mixing is active coupling) but lower priority than the worker.
- **Sequencing & delivery**: **TWO SEPARATE SDD changes**, not one combined — architecturally independent, reviewed by different cognitive modes. Worker first (higher risk/value), web second (lower risk colocation). Split each if >400-line review budget.
- **Mandatory precondition**: characterization tests for `companies/page.tsx` and `jobs/[id]/page.tsx` (untested), and a parser/Score unit test using today's `parseEvaluationResponse` output as the oracle — BEFORE moving code.

## Risks
- evaluate.test.mjs / scan.test.mjs assert exact call count/order — must be rewritten, not relocated, or the safety net is meaningless.
- companies/page.tsx and jobs/[id]/page.tsx have zero coverage — splitting blind risks silent behavior change.
- Web tests mock files by relative path — moves break mocks unless updated in the same PR.
- The Score/RECOMMEND_THRESHOLD rule is NEW behavior, not pure refactor — must be flagged explicitly in proposal/spec, not hidden inside "refactor."
- ingest-cv.mjs's identical pattern, if left untouched, creates an architecture inconsistency — acceptable short-term if tracked.

## Ready for Proposal
Yes — but recommend splitting into two SDD changes first: `worker-ddd-evaluate` (DDD-lite for evaluate-job only, characterization tests first) and a separate web feature-folder change (fat pages first, hooks/lib/api.ts second).
