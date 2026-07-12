# Tasks: Article Digest

> Forward plan for `openspec/changes/article-digest/{proposal,spec,design}.md`.
> Mirrors the shipped `cvs` table + CRUD pattern (design.md's own framing) plus
> one additional cached prompt block. Strict TDD is active for this project
> (`make test-all`); RED/GREEN pairs are used only where real logic exists —
> service validation/ownership-guard and the prompt truncation algorithm. Pure
> scaffolding (migration SQL, sqlc query files, route registration) is
> implemented directly, matching this project's `job-content-fetch` precedent.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~730-780 total (DB ~65, Go API ~300, pgTAP RLS test ~100, worker ~95, web ~175) — excludes sqlc-generated `api/internal/db/article_digests.sql.go` + models (auto-generated, "DO NOT EDIT", low review burden) |
| 400-line budget risk | **High** — design.md's own "small, single-PR-sized" guess does not hold once every layer's tests are counted; total is nearly double the 400-line budget and larger than `evaluation-quality`'s ~430-480 (which itself was chained) |
| Chained PRs recommended | **Yes** |
| Suggested split | PR-A: DB scaffolding + pgTAP RLS test (~163 lines) → PR-B: Go API package (~300 lines) → PR-C: worker prompt block + web CRUD page (~270 lines) |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main (per orchestrator precedent from `evaluation-quality`) |

Decision needed before apply: **Yes**
Chained PRs recommended: **Yes**
400-line budget risk: **High**

**Why this diverges from the proposal/design guess**: the proposal and design
both estimated "small, single-PR-sized" by counting only production code (one
table, three thin Go files, one prompt block, one web page). That undercounts
because strict TDD is active project-wide — every RED/GREEN pair adds a test
file or test block on top of the production line, and this change touches
five independent layers (DB, Go API, pgTAP, worker, web) each needing at least
one runnable check per the acceptance checklist. Once those are added, the
realistic total is ~730-780 lines, not "well under 400." This is a case where
the boilerplate-mirror framing was right about *implementation risk* (low —
every piece has an in-repo template) but wrong about *review size* (test
surface area scales with layer count, not novelty).

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | DB: migration, `schema.sql`/`rls.sql` canonical updates, sqlc queries, pgTAP RLS test | PR-A | Foundation; no dependency on Units 2/3 |
| 2 | Go API: `digest` package (handler+service+tests) + `main.go` wiring | PR-B | Depends on Unit 1 (needs the table + sqlc-generated types) |
| 3 | Worker prompt block + web CRUD page | PR-C | Worker sub-part depends only on Unit 1 (raw SQL via `tenantQuery`, no Go API dependency); web sub-part depends on Unit 2's endpoints. Bundled into one PR per stacked-to-main ordering — splitting further is optional if either sub-part regresses independently |

## Phase 1: DB Scaffolding (Unit 1, PR-A)

- [ ] T-296 `db/migrations/008_article_digests.sql` — new migration: `CREATE TABLE article_digests` (`id`, `user_id` FK CASCADE, `title`, `content_md`, `created_at`), composite index `idx_article_digests_user (user_id, created_at DESC)`, `ENABLE`+`FORCE ROW LEVEL SECURITY`, `tenant_article_digests` policy (`USING`+`WITH CHECK`, `NULLIF(current_setting('app.current_user_id', true), '')::uuid` shape), `GRANT SELECT, INSERT, UPDATE, DELETE ON article_digests TO app_user`. Exact SQL per design.md Decision 1. Verify against the real filesystem state of `db/migrations/` before writing — `007` may already be claimed by the sibling `profile-persistence` change by the time this lands; `008` is design's assignment but is not a filesystem lock. Est. ~25 lines.
- [ ] T-297 `db/schema.sql` — append the `article_digests` table + composite index after the `cvs` block, keeping schema.sql the canonical full picture. Est. ~12 lines.
- [ ] T-298 `db/rls.sql` — add `article_digests` to the `ENABLE ROW LEVEL SECURITY` list, the `FORCE ROW LEVEL SECURITY` list, and a `tenant_article_digests` policy block, matching `tenant_cvs`'s exact shape. Est. ~10 lines.
- [ ] T-299 `db/queries/article_digests.sql` — `ListDigestsByUser :many` (`ORDER BY created_at DESC`), `InsertDigest :one` (`RETURNING *`), `DeleteDigest :execrows` (`WHERE id = $1 AND user_id = $2`). Est. ~16 lines.
- [ ] T-300 `cd db && sqlc generate` — regenerate `api/internal/db/article_digests.sql.go` + `ArticleDigest` model; run `cd api && go build ./...` to confirm the generated types compile and are referenced correctly. No hand-edits to generated output.

**Acceptance (T-296..T-300)**: migration applies cleanly against a fresh DB; `db/schema.sql`/`db/rls.sql` reflect the migration's final state exactly; `sqlc generate` produces `ListDigestsByUser`/`InsertDigest`/`DeleteDigest` with no manual fixups; `go build ./...` succeeds.

## Phase 2: pgTAP RLS Test (Unit 1, PR-A)

- [ ] T-301 `db/tests/article_digests_rls.test.sql` — new pgTAP file mirroring `db/tests/cv_ingestions_rls.test.sql`'s structure (`plan(6)`): (1) RLS enabled on `article_digests`, (2) RLS forced, (3) cross-tenant `SELECT` returns 0 rows (spec scenario "Cross-tenant read is denied"), (4) cross-tenant `SELECT ... WHERE id = <row>` also returns 0 rows, (5) cross-tenant `DELETE` affects 0 rows and the owner's row is unaffected (spec scenario "Cross-tenant delete is denied"), (6) owner's own `SELECT`/`DELETE` succeeds normally (spec scenario "Owner can read and delete their own row"). Run as `app_user` via `make test-rls`. Est. ~100 lines.

**Acceptance (T-301)**: `make test-rls` passes including the new file; user B cannot read or delete user A's `article_digests` row via either RLS or the app-layer `WHERE user_id` scope; user A's own row is fully readable/deletable.

## Phase 3: Go API — `digest` package (Unit 2, PR-B)

- [ ] T-302 RED: `api/internal/digest/service_test.go` — `TestCreateDigest_EmptyTitle` and `TestCreateDigest_EmptyContentMd`: call `Service.CreateDigest` with a `nil` pool and assert a validation error is returned. Validation must short-circuit BEFORE `platform.WithTenantTx` ever touches the pool, so a `nil` pool proves no DB call was attempted. Est. ~25 lines.
- [ ] T-303 GREEN: `api/internal/digest/service.go` — `Service{pool}`, `NewService`, `ErrNotFound` sentinel (mirrors `cv.ErrNotFound`), `ListDigests` (`q.ListDigestsByUser` inside `WithTenantTx`), `CreateDigest` (validate non-empty `title`/`content_md` first, then `q.InsertDigest` inside `WithTenantTx`). Est. ~55 lines.
- [ ] T-304 RED: `api/internal/digest/service_integration_test.go` (DB-gated via `TEST_DATABASE_URL`, skips cleanly otherwise — mirrors `api/internal/jobs/addmanual_enqueue_integration_test.go`'s `rlsdb` harness pattern) — `TestDeleteDigest_NotOwned`: seed a digest for user A via `h.AdminPool`, call `Service.DeleteDigest` as user B, assert `ErrNotFound` and that A's row still exists; `TestDeleteDigest_Owner`: owner deletes their own row successfully (n==1 path, row gone afterward). Est. ~50 lines.
- [ ] T-305 GREEN: `api/internal/digest/service.go` — `DeleteDigest` calls `q.DeleteDigest` (`:execrows`) inside `WithTenantTx`, maps `n == 0` → `ErrNotFound` per design.md Decision 4. Est. ~15 lines.
- [ ] T-306 `api/internal/digest/handler.go` + `api/internal/digest/handler_test.go` — `Servicer` interface (`ListDigests`/`CreateDigest`/`DeleteDigest`), `Handler`, `RegisterRoutes` (`GET /api/article-digests` → `{"digests":[...]}`, `POST /api/article-digests` → decode `{title, content_md}` → 201 row, `DELETE /api/article-digests/{id}` → 204, `ErrNotFound` → 404, all else → 500). `handler_test.go` mirrors `api/internal/cv/handler_test.go` (mocked `Servicer` via `testify/mock` + `newChiCtx` helper): List 200, Create 201, Create 400 on empty title (validation error bubbling from a mocked error), Delete 204, Delete 404 on `ErrNotFound`. Est. ~70 lines handler.go + ~90 lines handler_test.go.
- [ ] T-307 `api/cmd/api/main.go` — wire `digestHandler := digest.NewHandler(digest.NewService(pool)); digestHandler.RegisterRoutes(r)` alongside the other handlers. Est. ~3 lines.

**Acceptance (T-302..T-307)**: `cd api && go test ./internal/digest/... -v` green (integration test skips cleanly without `TEST_DATABASE_URL`, passes when set); empty `title`/`content_md` never reaches the DB; a delete against another user's row returns `ErrNotFound` → HTTP 404; all three routes reachable once `main.go` is wired.

## Phase 4: Worker Prompt Block (Unit 3, PR-C)

- [ ] T-308 RED: `worker/tests/lib/prompt.test.mjs` (or nearest existing prompt test file) — add cases for `buildEvaluationPrompt`: (a) zero digests → `system` array length unchanged at 2, no third entry, not even an empty/header-only block; (b) entries under both the 4000-char per-entry cap and the 24000-char running-total cap → all included, newest-first, third block carries `cache_control: { type: 'ephemeral' }`; (c) one entry's `content_md` exceeds 4000 chars → truncated to 4000 chars with a `"…[truncated]"` marker appended; (d) entries whose running total would exceed 24000 chars before all fit → the whole entry that would breach the ceiling AND every entry after it are dropped (never split mid-entry), keeping only the newest ones that fit. Est. ~60 lines.
- [ ] T-309 GREEN: `worker/lib/prompt.mjs` — in `buildEvaluationPrompt`, after the existing `staticSystemPrompt`/`cvAndProfileBlock` entries, fetch digests via the existing `tenantQuery` (`SELECT title, content_md FROM article_digests WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 20`), apply the per-entry-cap-then-running-total-cap algorithm from design.md Decision 5 (`PER_ENTRY_MAX=4000`, `TOTAL_MAX=24000`, drop whole entries never split), render surviving entries under a `"## Project Proof Points\n\n"` header joined by `"\n\n"`, and push a third `{ type:'text', text, cache_control:{ type:'ephemeral' } }` system entry ONLY IF ≥1 entry survives. Est. ~35 lines.

**Acceptance (T-308..T-309)**: `cd worker && npx vitest run tests/lib/prompt.test.mjs` green; all four spec.md scenarios (zero-digests omission, under-cap inclusion, per-entry truncation, running-total whole-entry drop) pass; the existing two system blocks are untouched in content and order.

## Phase 5: Web CRUD Page (Unit 3, PR-C)

- [ ] T-310 `web/features/article-digest/api.ts` — `ArticleDigest` interface + `listDigests`/`createDigest`/`deleteDigest` typed wrappers over `lib/api.ts`, mirroring `web/features/cv/api.ts`. Est. ~15 lines.
- [ ] T-311 RED: `web/__tests__/app/article-digest.test.tsx` (mirrors `web/__tests__/app/companies.test.tsx` structurally, mocking `web/features/article-digest/api.ts`) — cases: empty state renders with no digests and no stray list markup; submitting the create form adds the returned entry to the rendered list; clicking Delete on an entry removes it from the rendered list. Est. ~70 lines.
- [ ] T-312 GREEN: `web/app/(app)/article-digest/page.tsx` — replace the `ComingSoon` stub with a `'use client'` component: `useEffect` → `listDigests()` on mount; a form (`<input>` title + `<textarea>` markdown body, no rich editor) → `createDigest()` → prepend the returned row to state, clear the form; each list row renders `title` + a Delete `<button>` → `deleteDigest(id)` → filter it out of state on success. Flat — form and list live in the page file, no extracted sub-components (design.md Decision 6 / YAGNI). Est. ~85 lines.

**Acceptance (T-310..T-312)**: `cd web && npx vitest run __tests__/app/article-digest.test.tsx` green; the page renders a create form and a deletable list, replacing `ComingSoon`; create/delete update the in-memory list without a full reload.

## Phase 6: Cross-cutting Verification

- [ ] T-313 Run `make test-all`; confirm Go/worker/web suites — including every test added in T-296..T-312 — pass together, and that no other caller of `worker/lib/prompt.mjs`'s `system` array output broke now that it can be length-2 or length-3 depending on the user's digest count.

## Dependencies Between Slices

- Unit 1 (DB scaffolding + pgTAP RLS test, PR-A) has no dependency on Units 2/3 — it is the foundation.
- Unit 2 (Go API, PR-B) depends on Unit 1: needs the `article_digests` table to exist and `sqlc generate`'s output (`db.ArticleDigest`, `InsertDigestParams`, `DeleteDigestParams`) to compile against.
- Unit 3 (worker + web, PR-C) has a split dependency: the worker sub-part (T-308/280) depends only on Unit 1 (raw SQL via `tenantQuery`, no Go API involved); the web sub-part (T-310..283) depends on Unit 2's three routes existing. Both are bundled into one PR under stacked-to-main ordering (PR-C targets main after PR-B merges) — split further only if one sub-part needs to ship independently of the other.
- T-313 (cross-cutting `make test-all`) runs last, after all three units have landed.
