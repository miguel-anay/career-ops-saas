# Verify Report: evaluation-quality

**Verdict: PASS**

Shipped to `main` via PR #49 (`feat/48-evaluation-guards`) and PR #50
(`feat/48-evaluation-web`), both merged. Issue #48 closed. T-266 executed live
in this verification pass (previously deferred/unchecked in tasks.md).

## Test Evidence (T-266, run live)

| Suite | Result | Notes |
|---|---|---|
| `cd api && go test ./... -count=1` | **PASS** — 14/14 packages | Required `TEST_DATABASE_URL=postgres://app_user:app_pw@...` (the `careerops` role is a Postgres superuser and bypasses RLS entirely, causing all 6 RLS-integration packages to falsely fail with "expected error but got nil" on first attempt — not a code defect, a role-choice issue in my first run). |
| `cd worker && npm test` | **PASS** — 172/176 (4 skipped, pre-existing DB-gated, unrelated) across 32 files | Required Node 20 (`fnm exec --using=20`) — default shell Node was v16.16, which lacks `crypto.getRandomValues`, crashing Vite/Vitest startup. Environment issue, not a code issue. |
| `cd web && npm test -- --run` | **PASS** — 53/53 across 10 files | Matches tasks.md's declared acceptance target exactly. |

### One flake found and root-caused (not a code bug)
`TestEvaluateRLS_Integration/owner_EnqueueEvaluation_still_succeeds` failed on
first run with `evaluation limit reached for free plan`. Root cause: the test
seeds a fixed-email user (`evaluate-itest-a@test.invalid`) via
`auth_upsert_user`, which is idempotent by email — the `usage` row for that
user accumulates across every historical run against this shared dev
Postgres instance (`evaluations_count` was 6, over the free-plan limit of 5).
Deleted the stale `usage` row, re-ran, test passed clean. This is a
pre-existing test-isolation gap in `rlsdb` fixtures (no `usage` reset between
runs), **not caused by this change's code**. Confirmed by re-running the full
Go suite clean afterward — 100% pass.

## Spec Compliance (spec.md, cross-checked against `main`)

| Requirement | Evidence | Status |
|---|---|---|
| 422 `cv_missing` before enqueue | `service.go:80-86` — `GetUserByID` + `isBlank` guard, runs inside `WithTenantTx` before usage check/enqueue | COMPLIANT |
| 422 `job_content_missing` before enqueue | `service.go:87-89` — guard on `job.ScrapedContent`, JD checked after CV per design's stated order | COMPLIANT |
| 404/402 precedence preserved | `service.go:66-75` (ownership) and `:91-103` (usage) run before the new guards; `handler.go:54-66` switch unchanged shape | COMPLIANT |
| `blocks_json` persisted as array | `EvaluationParser.mjs:55-57` — fixed A→G `BLOCK_LETTERS.filter(...).map(...)` array of `{label, content}` | COMPLIANT |
| `parseError` array-safe | `Evaluation.parseError` object; web guard `Array.isArray(x) && x.length>0` false for it — verified via design.md's documented (non-)conflict with spec wording | COMPLIANT |
| Prompt: posting age + STAR/negotiation | `prompt.mjs:25-31` (`describePostingAge`), `:102-105` (guidance text), 7-block schema unchanged | COMPLIANT |
| Web: distinct 422 panels | `page.tsx:217-234`, `lib/api.ts:7-17` (`ApiError{status,code}`) | COMPLIANT |
| Web: array `blocks_json` render, legacy-safe | `page.tsx:240-266` — `Array.isArray`-equivalent length guard, `content_md` fallback | COMPLIANT |

## Tasks (tasks.md) vs Code State

All T-252..T-265 marked done and verified present in code. T-266 was the sole
unchecked task — now executed with full pass evidence above.

## Findings

**CRITICAL**: none.

**WARNING**:
1. `rlsdb` integration-test fixtures (`evaluate`, and likely other packages
   using fixed test emails) don't reset the `usage` table between runs
   against a persistent dev DB, so `TestEvaluateRLS_Integration`'s
   usage-limit subtest can flake after ~5 cumulative historical runs. Not
   introduced by this change, but this change's PR-A added the first test
   that surfaces it. Suggest a follow-up: `DELETE FROM usage` (or scope by a
   unique per-run email/month) in `rlsdb.Harness` teardown or test setup.
2. Local dev environment note (not a code issue): running `worker`/`web`
   tests requires Node ≥18 (`crypto.getRandomValues`); default shell Node
   was v16.16 and crashed Vitest startup silently until `fnm exec --using=20`
   was used. Worth adding a `.nvmrc` or engines-check note if not already
   present, so contributors don't hit this blind.

**SUGGESTION**:
1. Apply-progress's open discovery (pdf.mjs reading `blocksJson.blockA.score`)
   is already resolved on `main` (commit `bc938d0`, prior to this change's
   PRs) — no action needed, confirmed by grep (no `blockA`/`blocks_json`
   references remain in `worker/jobs/pdf.mjs`, which now reads
   `applications.score` directly).
2. Per-block `score` capture was dropped per the Open Question in design.md
   (YAGNI, no consumer) — confirmed no regression; `applications.score`
   (overall) is the only score path used by both web and PDF.

## Conclusion

Implementation matches spec.md and design.md on every requirement checked.
All three test suites pass live. The only incomplete task (T-266) is now
complete. Recommend proceeding to `sdd-archive`.
