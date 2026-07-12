# Proposal: article-digest

> Slice #2 of the `candidate-profile-kb` exploration (see `openspec/changes/candidate-profile-kb/explore.md`, Part 4). Scope is exactly that Part 4 and nothing more. No dependency on the sibling `profile-persistence` change (slice #1) in either direction.

## Intent

### Problem

Job evaluations today read only two signals about the candidate: `users.cv_markdown` and `users.profile_json` (`worker/lib/prompt.mjs:37-42`). A CV is necessarily compressed — a line like "Built fraud detection pipeline" carries none of the hero metrics, architecture decisions, or proof points that actually win an evaluation. When Claude scores Block A (Role Fit) and Block B (Technical Match), it has no way to cite concrete project-level evidence that isn't already on the résumé. The candidate has richer material (portfolio write-ups, README-style project digests) but nowhere to put it so the evaluator sees it.

The `career-ops` CLI solved this with an `article-digest.md` file — a list of per-project proof-point entries read at evaluation time (see `career-ops/examples/article-digest-example.md`: `## Project`, `**Hero metrics:**`, `**Architecture:**`, `**Key decisions:**`, `**Proof points:**`, all free-text markdown). The SaaS has no equivalent. The web app even ships a `[Próximamente]` stub page for it (`web/app/(app)/article-digest/page.tsx`), so the surface is already wired into the nav and expected by users — it just does nothing.

### Why now

This is the lowest-risk, most mechanical slice of the whole `candidate-profile-kb` ask (explore Part 4, Recommendation). It is a near-exact copy of the already-shipped, already-RLS'd `cvs` table + CRUD pattern, plus one additional cached block in an existing prompt builder. It ships independently and immediately improves evaluation quality with real candidate-supplied evidence.

### Success looks like

- A user can paste a project write-up (title + markdown body), see it listed, and delete it — from the page that currently shows a "coming soon" stub.
- The next job evaluation for that user injects those digest entries into the Anthropic prompt as a bounded, cached system block, so the evaluation can reference concrete project proof points.
- The new table has RLS from the first migration (ADR-3), identical in shape to `tenant_cvs`.

## Scope

### In scope

1. **DB — new `article_digests` table.** Columns: `id uuid PK DEFAULT gen_random_uuid()`, `user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE`, `title text NOT NULL`, `content_md text NOT NULL`, `created_at timestamptz NOT NULL DEFAULT now()`. Index on `user_id`. No `is_master` (digests are an additive list, not single-active like `cvs`). Added to `db/schema.sql` and shipped via a new numbered migration in `db/migrations/`.
2. **DB — RLS from day one.** `ALTER TABLE article_digests ENABLE ROW LEVEL SECURITY; ... FORCE ROW LEVEL SECURITY;` plus a `tenant_article_digests` policy using the exact `NULLIF(current_setting('app.current_user_id', true), '')::uuid` shape as `tenant_cvs` (`db/rls.sql:60-62`). Both `USING` and `WITH CHECK`.
3. **DB — sqlc queries** in a new `db/queries/article_digests.sql`: `ListDigestsByUser :many` (ORDER BY created_at DESC), `InsertDigest :one` (RETURNING *), `DeleteDigest :exec` (WHERE id = $1 AND user_id = $2). Regenerate via `cd db && sqlc generate`. Mirrors `db/queries/cvs.sql`.
4. **Go API — new package `api/internal/digest/`** (hexagonal: `handler.go` + `service.go` + `repo.go`), following the `cv` package structure. Endpoints:
   - `POST /api/article-digests` — create `{title, content_md}`, returns the row (201).
   - `GET /api/article-digests` — list current user's entries.
   - `DELETE /api/article-digests/{id}` — delete one owned row.
   All three run through `platform.WithTenantTx(ctx, pool, userID, ...)` so RLS gates every read/write; delete additionally scopes on `user_id = $2` in the query as defense-in-depth, matching the project's existing tenant-delete discipline.
5. **Worker — third cached system block** in `worker/lib/prompt.mjs`'s `buildEvaluationPrompt`. After the existing `staticSystemPrompt` block and the `cvAndProfileBlock`, add a bounded digest block built from `SELECT title, content_md FROM article_digests WHERE user_id = $1 ORDER BY created_at DESC LIMIT N`, via the same `tenantQuery` already used there. Same `cache_control: { type: 'ephemeral' }` treatment. Block is omitted entirely when the user has no digests (no empty header injected).
6. **Web — real CRUD page** replacing the `ComingSoon` stub at `web/app/(app)/article-digest/page.tsx`: a form (title input + markdown `<textarea>`) to add an entry, and a list of existing entries each with a Delete button. All calls go through `web/lib/api.ts`. No rich editor — plain textarea (project convention).

### Out of scope (non-goals)

- **Edit-in-place.** First cut is add + delete only. To change an entry, the user deletes and re-adds. This is an intentional simplification, not an oversight — it keeps the API to three endpoints and the UI to one form, and delete+re-add fully covers the correction case for short free-text entries. Add `PATCH /api/article-digests/{id}` in a later slice if churn proves it's needed. (The `cvs` table already carries an `UpdateCV` query we could mirror later — the pattern exists, we're deferring it, not blocked on it.)
- **R2 file upload.** Digest entries are small free-text markdown stored directly in Postgres `content_md`. R2 in this codebase is reserved for binary generated artifacts (PDFs from `generate-pdf`). Storing markdown as a blob in R2 would add a signed-URL round-trip on every evaluation read for no benefit — Postgres is cheaper to store and re-read here. **This decision is explicit so it does not get re-litigated in design.**
- **AI-assisted digest generation/summarization.** Manual paste only for the first cut. No worker job, no Anthropic call to draft or condense entries.
- **Anything from the sibling `profile-persistence` change (slice #1).** No `profile_overrides`, no `profile_edits`, no `cv_markdown` merge. Mentioned only for context; zero dependency either direction. Digests are purely additive, so there is no merge conflict with that work and the two can land in any order.
- **Connecting the pre-existing `cvs`-vs-`cv_markdown` drift** noted in the exploration. Not this slice's job; this slice must not deepen it.

## Approach & rationale

**Mirror the `cvs` pattern, don't invent.** Every piece here has a proven in-repo template: table shape (`db/schema.sql:124-133`), RLS policy (`db/rls.sql:60-62`), queries (`db/queries/cvs.sql`), and the hexagonal `handler/service/repo` trio (`api/internal/cv/`). The `cv` service's `WithTenantTx` + app-layer ownership guard idiom (`service.go:161-204`) is copied verbatim for the digest service. This keeps the review surface tiny and the tenant-isolation guarantees identical to code already in production.

**Why a new `api/internal/digest/` package instead of extending `api/internal/cv/`** — confirming the exploration's lean: **new package.** `cv/handler.go` already carries six routes across three concerns (PDF generation, multi-CV CRUD, ingestion) and its `Servicer` interface is seven methods deep. Digests are a distinct domain noun with their own table and lifecycle; bolting three more routes and three more `Servicer` methods onto `cv` would push a already-crowded package further from single-responsibility, for zero shared logic (digests share no code path with PDF gen or ingestion). The hexagonal-per-domain convention in `CLAUDE.md` exists exactly for this. A new package is ~120 lines of near-boilerplate that reads cleanly; the alternative bloats an already-overloaded file. New package it is.

**Prompt injection sizing (the one real design decision here):**

- **N = 20 entries** (`LIMIT 20`, most recent first). Comfortably above any realistic portfolio size (the CLI reference file ships 2), so in practice every user's full digest set is injected; the cap exists only as a guardrail against pathological accumulation.
- **~4,000 chars per entry** (truncate `content_md` at ingest-to-prompt time if longer) and a **~24,000-char total ceiling** on the concatenated block (stop appending once reached). The example entries run ~600–900 chars, so 4,000 is generous headroom; 24 KB is roughly 6K tokens.
- **Why these numbers:** the digest block is a fourth thing competing for the cached prefix alongside the static prompt and the CV+profile block. Anthropic's 200K window is not the binding constraint — **cost and cache-write predictability are.** Prompt caching bills cache *writes* at a premium, and this block's cache key changes whenever the user adds/deletes a digest, so it will be re-written more often than the static block. A hard, bounded size keeps that write cost flat and predictable no matter how many entries a user accumulates over time. The `ORDER BY created_at DESC LIMIT N` means the newest, most relevant proof points survive the cap. Exact truncation mechanics (per-entry vs. running-total, ellipsis marker) are for `sdd-design`; the proposal fixes the ceilings.

## Risks

- **Prompt length / cost growth as entries accumulate** (flagged in explore, Part 4 & Risks). Mitigated by the N=20 / 24 KB ceiling above, but the ceiling is a guess — if real users write long entries, the total block could still add ~6K tokens to every evaluation, and because its cache key changes on every CRUD op, cache-write cost is non-trivial. Design should confirm the numbers against a real token count and decide whether truncation is per-entry or global. **This is the main thing to validate before `sdd-apply`.**
- **Empty-state correctness.** The block must be fully omitted (not an empty `## Proof Points` header) when the user has no digests, or evaluations for the many users with zero entries get a useless cached block. Small but easy to get wrong.
- **Migration ordering.** New migration must be the next number in sequence and land RLS in the same migration as the table — never a table without its policy (ADR-3 invariant). No table-then-later-RLS gap.
- **Delivery (ask-on-risk).** This is a small, single-PR-sized slice (one table, three thin Go files, one prompt block, one web page). Chained PRs are almost certainly unnecessary; expect the tasks phase to forecast well under the 400-line budget. Flagging per the delivery strategy, but no chain decision is needed here.

## Rollback plan

- **DB:** drop the `article_digests` table (CASCADE removes rows and the policy). The table is standalone — nothing else FKs into it, so a drop is clean with no orphan references.
- **Worker:** removing the third block is a localized revert in `buildEvaluationPrompt`; evaluations fall back to CV + profile exactly as today. No data migration, no dependent state.
- **Web:** restore the `ComingSoon` stub (one-line component swap).
- **API:** unmount the three routes / delete the `digest` package. No other package imports it.

Every layer reverts independently; there is no cross-cutting state to unwind.

## Success criteria / acceptance checklist

- [ ] `article_digests` table exists via a new `db/migrations/` file, reflected in `db/schema.sql`, with `ENABLE` + `FORCE ROW LEVEL SECURITY` and a `tenant_article_digests` policy matching `tenant_cvs`'s shape.
- [ ] `sqlc generate` produces working Go types for `ListDigestsByUser`, `InsertDigest`, `DeleteDigest` from `db/queries/article_digests.sql`.
- [ ] `POST /api/article-digests` creates an entry for the authenticated user and returns 201 with the row; `GET` lists only that user's entries; `DELETE /{id}` removes one owned entry and cannot touch another user's row (RLS + `user_id` scope).
- [ ] A pgTAP/RLS test proves cross-tenant reads and deletes on `article_digests` are denied (consistent with existing `tenant_cvs` coverage).
- [ ] `buildEvaluationPrompt` emits a third cached system block from the user's digests, bounded to N=20 / ~24 KB, and omits the block entirely when the user has none.
- [ ] The `article-digest` web page renders a create form and a deletable list, wired through `web/lib/api.ts`, replacing the `ComingSoon` stub.
- [ ] Existing test suites (`make test-all`) stay green; new logic (service create/list/delete, prompt block bounding) has at least one runnable check each.

## Next phases

- `sdd-spec` and `sdd-design` can run in parallel from this proposal.
- Design owns: exact truncation mechanics for the prompt block, migration number, and the precise sqlc/Go type wiring.
