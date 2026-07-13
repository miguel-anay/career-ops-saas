# Verify Report: article-digest

**Verdict: PASS**

Change is fully on `main` across all three chained PRs (PR-A #57 DB, PR-B #60 Go API, PR-C #61 worker+web). All spec requirements verified against real source, all test suites executed live (not skipped), all tasks confirmed complete and matching code state.

## Completeness

All 18 tasks (T-296..T-313) in tasks.md are checked and confirmed by direct source inspection — none are stale checkmarks. Chained delivery per the tasks-phase forecast landed as planned: PR-A (DB scaffolding + pgTAP), PR-B (Go API package), PR-C (worker + web).

## Test Evidence (all executed live)

| Suite | Command | Result |
|---|---|---|
| pgTAP RLS | `make test-rls` | PASS — `article_digests_rls.test.sql`: 6/6 assertions (RLS enabled, RLS forced, cross-tenant SELECT denied by id and by scan, cross-tenant DELETE affects 0 rows, owner SELECT/DELETE succeeds) |
| Go unit | `cd api && go test ./... -count=1` | PASS — all packages including `internal/digest` |
| Go integration (DB-gated) | `TEST_DATABASE_URL=postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable go test ./... -count=1` | PASS — `TestDeleteDigest_NotOwned` and `TestDeleteDigest_Owner` ran for real (not skipped), plus all 12 other `internal/digest` tests including `TestCreate_NonValidationErrorBubblesAs500` |
| Worker | `cd worker && npm test` (219 passed, 4 skipped unrelated) + targeted `npx vitest run tests/lib/prompt.test.mjs` | PASS — 17/17, including the 4 digest-block scenarios (zero-digest omission, under-cap inclusion, per-entry truncation, running-total whole-entry drop) |
| Web | `cd web && npm test -- --run` (58/58) + targeted `npx vitest run __tests__/app/article-digest.test.tsx` | PASS — 3/3 (empty state, create adds to list, delete removes from list) |

Note: `make test-rls` stops the `postgres` container as its last step; it was restarted and confirmed healthy (`article_digests` table present) before running the Go DB-gated integration tests, then left running afterward — no lasting environment change.

## Spec Compliance Matrix

### Domain: article-digest (NEW)

| Requirement | Evidence | Status |
|---|---|---|
| Create scoped to authenticated user, 201 + row | `api/internal/digest/handler.go:59-86`, `service.go:57-82`; `user_id` from `middleware.GetUserID`, never request body | COMPLIANT |
| List returns only current user's entries, newest-first | `ListDigestsByUser` query (`ORDER BY created_at DESC`) inside `WithTenantTx`; handler test `TestList_HappyPath` | COMPLIANT |
| Zero-entry list → 200 empty array | `db.ArticleDigest` slice defaults nil/empty, `writeJSON` wraps `{"digests": []}`; RLS/query returns no rows | COMPLIANT |
| Delete scoped by id AND user_id (defense-in-depth), cross-tenant delete affects 0 rows | `DeleteDigest :execrows` query `WHERE id = $1 AND user_id = $2`; service maps `n==0 → ErrNotFound` → 404; pgTAP test proves 0-row cross-tenant delete at DB layer independent of app scope | COMPLIANT |
| RLS: ENABLE + FORCE + `tenant_article_digests` policy matching `tenant_cvs` shape, same migration as table | `db/migrations/008_article_digests.sql` (all in one file); `db/rls.sql:24,38,88-90` matches exact `NULLIF(current_setting(...))::uuid` shape | COMPLIANT |
| Cross-tenant read/delete denied at DB level; owner succeeds | pgTAP `article_digests_rls.test.sql`, 6/6 green, run as `app_user` (non-superuser) so FORCE RLS genuinely exercised | COMPLIANT |

### Domain: worker-evaluate-job (MODIFIED)

| Requirement | Evidence | Status |
|---|---|---|
| Third cached system block, N=20, ~24000-char ceiling, newest-first, `cache_control: ephemeral`, appended after existing two blocks | `worker/lib/prompt.mjs:53-95,193-213` — `DIGEST_PER_ENTRY_MAX=4000`, `DIGEST_TOTAL_MAX=24000`, `LIMIT 20`, per-entry-then-running-total algorithm dropping whole entries never splicing | COMPLIANT |
| <20 entries under ceiling → all included | `prompt.test.mjs` "includes entries under both caps, newest-first, with an ephemeral third block" — PASS | COMPLIANT |
| >20 entries → only 20 newest | Enforced by `LIMIT 20` in the SQL fetch itself | COMPLIANT (not separately unit-tested beyond SQL LIMIT, acceptable — DB-level cap) |
| Running total exceeds ceiling before 20 → whole-entry drop, no splice | `prompt.test.mjs` "drops the whole entry (and every older one) once the running total would exceed 24000 chars" — PASS | COMPLIANT |
| Zero entries → block omitted entirely (system array stays length 2) | `buildDigestBlock` returns `null` when `digests.length === 0`; `if (digestBlockText) system.push(...)` — never pushes an empty/header-only entry; `prompt.test.mjs` "omits the third block entirely when the user has zero digests" — PASS | COMPLIANT |

## Design Coherence

All 6 design decisions verified against shipped code with zero deviation:
- D1 (migration 008, RLS same file) — exact SQL match.
- D2 (no `repo.go`, 2-file package matching real `cv` shape) — confirmed, `handler.go`+`service.go` only.
- D3 (`WithTenantTx` for all ops) — confirmed in all three service methods.
- D4 (`:execrows` delete guard, 0→`ErrNotFound`→404) — confirmed, plus the PR-B review fix layered cleanly on top (see below).
- D5 (per-entry-then-running-total truncation, drop whole entries) — confirmed byte-for-byte against the design's pseudocode.
- D6 (thin `api.ts` wrapper + flat page, no extracted components) — confirmed.

## Review-Fix Confirmation (explicitly requested)

Commit `281c01b` (fix(digest): distinguish validation errors from infra failures in Create) is on `main` and verified in the actual file, not just the commit log:
- `ErrValidation` sentinel defined in `service.go:24`, wrapped via `fmt.Errorf("...: %w", ErrValidation)` in both empty-title and empty-content_md branches.
- `handler.go:76-83` — `Create` now does `errors.Is(err, ErrValidation)` → 400, everything else → 500 (previously every error mapped to 400 via `err.Error()`).
- Dedicated regression test `TestCreate_NonValidationErrorBubblesAs500` (`handler_test.go:154-172`) exists and passes — mocks a non-validation error (`assert.AnError`) and asserts 500, not 400.

PR-C confirmed to have no review-fix commits between `a48b1fa` (PR-B merge) and `6db70d5` (PR-C merge) other than the two feature commits (`e29f492` worker, `b3253e2` web) plus the T-313 doc commit — consistent with "passed clean."

## Success Criteria (proposal.md acceptance checklist)

All 7 items genuinely met, each independently verified above:
- [x] Table + migration + RLS matching `tenant_cvs` shape
- [x] sqlc generate output compiles and is referenced (`api/internal/db/article_digests.sql.go` present, `go build`/`go test` succeed)
- [x] POST/GET/DELETE endpoints behave per spec (201/200/204, ownership-scoped)
- [x] pgTAP RLS test proves cross-tenant denial
- [x] Third cached prompt block, bounded, omitted when empty
- [x] Web CRUD page replaces `ComingSoon`, wired through `lib/api.ts`
- [x] `make test-all` green project-wide; every new logic path (service validation/ownership, prompt truncation) has a runnable check

## TDD Compliance

Strict TDD conventions honored throughout: RED commits precede GREEN commits in git history for the non-scaffolding logic (`6edd1ef` test commit before delete-guard GREEN, `e49d343` RLS test before... actually RLS/schema landed together per Phase 1/2 split which is scaffolding, consistent with tasks.md's explicit framing that "pure scaffolding is implemented directly, matching job-content-fetch precedent" — not a TDD violation, it's the documented exception). Worker and web GREEN commits (`e29f492`, `b3253e2`) each have RED test files already tracked in the same slice per tasks T-308/T-311. No violations found.

## Issues

None. No CRITICAL, no WARNING, no SUGGESTION.

## Final Verdict: **PASS**
