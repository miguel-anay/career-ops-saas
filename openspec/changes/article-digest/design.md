# Design: article-digest

> Forward design for the `article-digest` change (proposal.md, approved). Scope
> is slice #2 of `candidate-profile-kb` (explore Part 4) — a near-verbatim copy
> of the shipped `cvs` table + CRUD, plus one additional cached prompt block.
> This document fixes the concrete SQL, Go signatures, and prompt-truncation
> mechanics that the proposal deferred to design.

## Technical Approach

Four thin layers, each mirroring an in-repo template, zero new dependencies:

1. **DB** — one new tenant table `article_digests` (mirror of `cvs` minus
   `is_master`), shipped via migration `008_article_digests.sql` with RLS
   `ENABLE`+`FORCE`+policy+`GRANT` in the SAME file (ADR-3). Reflected in
   `db/schema.sql` and `db/rls.sql`.
2. **sqlc** — `db/queries/article_digests.sql` with three queries; regenerated
   into `api/internal/db/`.
3. **Go API** — new package `api/internal/digest/` (`handler.go` + `service.go`,
   NO `repo.go` — see Decision 2), three routes, all reads/writes through
   `platform.WithTenantTx` so RLS gates every op.
4. **Worker** — a THIRD `cache_control: ephemeral` system block appended AFTER
   the two existing blocks in `buildEvaluationPrompt`, built only when the user
   has digests, bounded by per-entry + running-total caps (Decision 5).
5. **Web** — real CRUD page replacing the `ComingSoon` stub, with a thin
   `web/features/article-digest/api.ts` wrapper (mirrors `web/features/cv/api.ts`).

The control/data-plane split holds: Go serves CRUD, the worker reads digests at
evaluation time. No new queue, no WS, no R2, no Anthropic call for CRUD.

## Architecture Decisions

### Decision 1 — Migration `008_article_digests.sql`, RLS in the same file

**Choice**: The next free sequential number was `007`, but the sibling
`profile-persistence` change (independent, in-flight) also claims `007` in its
own design — **orchestrator-resolved**: `profile-persistence` (slice #1) keeps
`007`, `article-digest` (slice #2) takes `008`. This migration creates the
table, the index, enables + forces RLS, creates `tenant_article_digests`, and
grants CRUD to `app_user` — all in one file, exactly like `006_gmail_ingestion.sql`.
Whichever of the two changes actually lands in `main` first at apply time,
`sdd-tasks`/`sdd-apply` must still verify against the real filesystem state of
`db/migrations/` before writing the file — this assignment is correct as of
this design pass but is not a filesystem lock.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| New numbered migration `008`, RLS inline | Matches the ACTUAL 001–006 convention; never a table without its policy | **Chosen** |
| Amend `001_initial.sql` ("single bootstrap file") | CLAUDE.md says this, but the filesystem shows 002–006 already broke it; amending bootstrap would desync deployed DBs | Rejected — stale doc |
| Table in migration, RLS in a later migration | Violates ADR-3 (table-without-policy gap) | Rejected |

**Discovered fact (relevant to the sibling `profile-persistence` change too):**
CLAUDE.md's "single bootstrap file at MVP" is STALE. The real convention is
sequential numbered migrations; `db/migrations/` contains `001`–`006`. Next
free numbers are `007`/`008`, assigned as described above.

Exact migration SQL:

```sql
-- Migration 008: article-digest — per-project proof-point entries.
-- Mirrors cvs (db/schema.sql:124-133) minus is_master; digests are an additive
-- list, not a single-active record. RLS forced from day one (ADR-3).

CREATE TABLE article_digests (
  id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text        NOT NULL,
  content_md text        NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_article_digests_user ON article_digests(user_id, created_at DESC);

ALTER TABLE article_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_digests FORCE  ROW LEVEL SECURITY;

CREATE POLICY tenant_article_digests ON article_digests
  USING      (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid)
  WITH CHECK (user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE, DELETE ON article_digests TO app_user;
```

**Index refinement (deliberate, noted):** the proposal says "index on
`user_id`". I use the composite `(user_id, created_at DESC)` because every read
path (`ListDigestsByUser`, the worker's `ORDER BY created_at DESC LIMIT 20`)
sorts by `created_at DESC` within a user — the composite serves the sort for
free and matches `idx_jobs_user_received` / `idx_scan_runs_user`. Strictly
better, same cost. `GRANT ... UPDATE` is included for parity with the 006
template even though no UPDATE query ships this slice (edit-in-place is out of
scope); harmless and keeps the grant identical to siblings.

`db/schema.sql` gets the table + index appended after the `cvs` block;
`db/rls.sql` gets `article_digests` added to the ENABLE list, the FORCE list,
and a `tenant_article_digests` policy — keeping `rls.sql` the canonical full
picture (it already lists all ten tables including `email_ingest_runs`).

### Decision 2 — New package `api/internal/digest/`, `handler.go` + `service.go` only (NO `repo.go`)

**Choice**: A new domain package, two files, mirroring the ACTUAL `cv` package
structure — not the "handler/service/repo trio" the proposal describes. `cv/`
has NO `repo.go`; its `Service` calls sqlc `*db.Queries` directly inside
`platform.WithTenantTx`. `digest` does the same. Inventing a `repo.go` here
would introduce a structural convention this codebase does not use.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| New `digest` package, handler+service, sqlc direct | Matches real `cv`/`tracker` shape; tiny review surface | **Chosen** |
| Add `repo.go` layer | Proposal wording, but zero precedent in this repo; extra indirection for 3 queries | Rejected — no such pattern exists |
| Extend `cv` package | `cv` already carries 6 routes / 7 Servicer methods across 3 concerns; zero shared code path with digests | Rejected — bloats an overloaded package |

`Servicer` interface (handler depends on it; `testify/mock` in tests):

```go
type Servicer interface {
    ListDigests(ctx context.Context, userID uuid.UUID) ([]db.ArticleDigest, error)
    CreateDigest(ctx context.Context, userID uuid.UUID, title, contentMd string) (*db.ArticleDigest, error)
    DeleteDigest(ctx context.Context, userID, digestID uuid.UUID) error
}
```

Routes (`RegisterRoutes`, wired in `api/cmd/api/main.go` alongside the others —
`digestHandler := digest.NewHandler(digest.NewService(pool)); digestHandler.RegisterRoutes(r)`):

```go
r.Get("/api/article-digests", h.List)          // 200 {"digests": [...]}
r.Post("/api/article-digests", h.Create)       // 201 <row>
r.Delete("/api/article-digests/{id}", h.Delete) // 204 No Content
```

DTOs:
- **Create request**: `{ "title": string, "content_md": string }` — decoded into
  a local anonymous struct exactly like `CreateCV` (handler.go:151-155). Service
  validates non-empty `title` and `content_md`, returns 400 on empty.
- **Create response**: the inserted `db.ArticleDigest` row directly, status 201
  (mirrors `CreateCV` returning `cvRecord` at 201). No sql.Null* fields exist on
  this row, so no wire-shape mapper is needed (unlike `applicationJSON`).
- **List response**: `{"digests": [...]}` wrapper (mirrors `{"cvs": ...}`).
- **Delete**: no body; 204 on success.

### Decision 3 — `WithTenantTx` for all three ops; RLS is the primary gate

**Choice**: Every service method wraps its query in
`platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {...})`,
byte-for-byte the `cv` idiom (service.go:161-204). RLS sets
`app.current_user_id` via `SET LOCAL` inside the tx, so a non-owner's row is
invisible for reads and unaffected by writes.

- `ListDigests` → `q.ListDigestsByUser(ctx, userID)`.
- `CreateDigest` → validate, then `q.InsertDigest(ctx, db.InsertDigestParams{UserID: userID, Title: title, ContentMd: contentMd})`.
- `DeleteDigest` → see Decision 4.

### Decision 4 — Delete ownership guard via `:execrows`, 0 rows → 404

**Choice**: The delete query scopes on BOTH `id` and `user_id` (defense-in-depth
on top of RLS) and is declared `:execrows` so sqlc returns the affected row
count. The service maps `rows == 0` to `ErrNotFound`; the handler returns 404.
A successful delete returns 204.

```sql
-- name: DeleteDigest :execrows
DELETE FROM article_digests
WHERE id = $1 AND user_id = $2;
```

```go
func (s *Service) DeleteDigest(ctx context.Context, userID, digestID uuid.UUID) error {
    return platform.WithTenantTx(ctx, s.pool, userID, func(q *db.Queries) error {
        n, err := q.DeleteDigest(ctx, db.DeleteDigestParams{ID: digestID, UserID: userID})
        if err != nil {
            return err
        }
        if n == 0 {
            return ErrNotFound // deleting another tenant's row, or a nonexistent id
        }
        return nil
    })
}
```

| Option | Tradeoff | Decision |
|--------|----------|----------|
| `:execrows`, 0 → ErrNotFound → 404 | Distinguishes "not found / not owned" from success; honest status codes | **Chosen** |
| `:exec` (no row count) | Can't tell a no-op delete from a real one; would always 204 even for a foreign/missing id | Rejected — silent success on bad delete |

`ErrNotFound` is a package-local sentinel (`var ErrNotFound = errors.New("not found")`),
mirroring `cv.ErrNotFound`. Handler maps it to 404, all else to 500.

### Decision 5 — Third cached prompt block, appended AFTER the two stable blocks; per-entry cap THEN running-total cap

**Choice**: In `buildEvaluationPrompt` (`worker/lib/prompt.mjs`), after the
existing `staticSystemPrompt` (system[0]) and `cvAndProfileBlock` (system[1]),
fetch the user's digests and, ONLY IF there is at least one, push a third
`{ type:'text', text: digestBlock, cache_control:{ type:'ephemeral' } }` entry.
The first two blocks are NOT touched in content or order — appending after them
preserves their cache-hit behavior (a new trailing block does not invalidate a
prior cache breakpoint).

Fetch (via the same `tenantQuery` already destructured from `db`):

```js
const digestResult = await tenantQuery(
  userId,
  `SELECT title, content_md FROM article_digests
   WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 20`,
  [userId]
)
const digests = digestResult.rows || []
```

**Truncation algorithm (the proposal's flagged open question — resolved):**

```
PER_ENTRY_MAX = 4000    // chars of content_md per entry
TOTAL_MAX     = 24000   // chars of the concatenated block body

1. Skip the block entirely if digests.length === 0 (no header emitted).
2. For each digest (already newest-first from SQL):
     a. cap content_md to PER_ENTRY_MAX chars; if it was longer, append
        the marker "\n…[truncated]".
     b. render the entry as `### ${title}\n${cappedContent}`.
     c. if (runningTotal + entry.length) > TOTAL_MAX: STOP — do NOT append
        this entry (drop the WHOLE entry; never cut an entry mid-way).
        Break the loop.
     d. else append the entry, add its length to runningTotal.
3. Prefix the surviving entries with a "## Project Proof Points\n\n" header
   and join with "\n\n". This is the block only if ≥1 entry survived step 2.
```

Ordering matters: **per-entry cap FIRST, then accumulate**. A single giant
entry is capped to 4000 before it ever competes for the 24000 budget, so one
pathological entry can never consume the whole block. Once appending the next
whole entry would breach 24000, we drop that entry and every entry after it
(they're older / lower priority by `created_at DESC`) rather than splicing a
half-entry — a half-entry would be confusing evidence for the evaluator.

**Confirming the numbers against the real prompt:** the two existing blocks are
the ~1KB static instructions and the CV+profile block (unbounded today, but the
CV is the candidate's own résumé, typically a few KB). Adding a hard-capped
24000-char (~6K-token) third block keeps total prefix growth bounded and, more
importantly, keeps the CACHE-WRITE cost of this block flat regardless of how
many digests a user accumulates. The example digests run ~600–900 chars, so
`PER_ENTRY_MAX=4000` is generous headroom and `TOTAL_MAX=24000` fits ~6 full
entries or ~30 typical ones. **Numbers confirmed as-is** — no reason to adjust:
they are guardrails against pathological accumulation, not limits real users
will hit, and `LIMIT 20` in SQL caps the row fetch below any plausible portfolio
size anyway. The `PER_ENTRY_MAX × count` worst case (20 × 4000 = 80000) is
deliberately capped down to 24000 by the running total — the two caps compose,
they are not redundant.

| Option | Tradeoff | Decision |
|--------|----------|----------|
| Per-entry cap THEN running-total, drop whole entries | Predictable ceiling; no giant-entry monopoly; no half-entries | **Chosen** |
| Running-total only (no per-entry cap) | One 24KB entry could fill the whole block, starving newer ones | Rejected |
| Cut the entry that breaches the ceiling mid-way | Squeezes a few more chars in | Rejected — half an entry is misleading evidence |

Empty-state correctness (proposal risk): step 1 guarantees zero users with no
digests get an empty `## Project Proof Points` header — the `system` array stays
exactly two blocks for them, identical to today.

### Decision 6 — Web: thin feature-api wrapper + inline page components

**Choice**: Mirror `web/features/cv/api.ts` — a thin module of typed functions
over `lib/api.ts`. Create `web/features/article-digest/api.ts`:

```ts
import { apiGet, apiPost, apiDelete } from '@/lib/api'

export interface ArticleDigest {
  id: string
  user_id: string
  title: string
  content_md: string
  created_at: string
}

export const listDigests   = () => apiGet<{ digests: ArticleDigest[] }>('/api/article-digests')
export const createDigest   = (title: string, content_md: string) =>
  apiPost<ArticleDigest>('/api/article-digests', { title, content_md })
export const deleteDigest   = (id: string) => apiDelete(`/api/article-digests/${id}`)
```

The page (`web/app/(app)/article-digest/page.tsx`, replacing the `ComingSoon`
one-liner) is a `'use client'` component:

- **State**: `digests: ArticleDigest[]`, `title`, `contentMd`, `loading`, `error`.
- **Load**: `useEffect` → `listDigests()` on mount, populate list.
- **Create form**: `<input>` (title) + `<textarea>` (markdown body, plain — no
  rich editor, project convention) + submit → `createDigest()` → prepend the
  returned row to state, clear the form.
- **List**: map `digests`, each row shows `title` + a Delete `<button>` →
  `deleteDigest(id)` → filter it out of state on success.

Component breakdown is intentionally FLAT — form + list live in the page file
(the cv feature folder ships only `api.ts` + `hooks.ts`, no component library),
so this slice does not invent a components convention the codebase lacks. Extract
a `DigestForm` / `DigestList` component only if a second consumer appears (YAGNI).

## Data Flow

    Web page (mount) ── GET /api/article-digests ──▶ digest.List
                                                       └ WithTenantTx → ListDigestsByUser → {"digests":[...]}
    Web form submit  ── POST /api/article-digests ─▶ digest.Create
                                                       └ validate → WithTenantTx → InsertDigest → 201 row
    Web delete btn   ── DELETE /api/article-digests/{id} ─▶ digest.Delete
                                                       └ WithTenantTx → DeleteDigest(:execrows)
                                                             ├ n==0 → ErrNotFound → 404
                                                             └ n==1 → 204

    (later) worker evaluate-job ── buildEvaluationPrompt(userId, jobId, db)
        ── tenantQuery SELECT title, content_md ... LIMIT 20
        ── digests.length===0 ? system=[static, cv]          (unchanged)
                              : system=[static, cv, digestBlock]  (3rd ephemeral block)

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `db/migrations/008_article_digests.sql` | New | Table + composite index + ENABLE/FORCE RLS + policy + GRANT (Decision 1) |
| `db/schema.sql` | Modify | Append `article_digests` table + index after `cvs` |
| `db/rls.sql` | Modify | Add `article_digests` to ENABLE + FORCE lists and a `tenant_article_digests` policy |
| `db/queries/article_digests.sql` | New | `ListDigestsByUser :many`, `InsertDigest :one`, `DeleteDigest :execrows` |
| `api/internal/db/article_digests.sql.go` + models | Generated | `sqlc generate` output (do not hand-edit) |
| `api/internal/digest/handler.go` | New | 3 routes, DTOs, `Servicer`, `ErrNotFound` mapping |
| `api/internal/digest/service.go` | New | `WithTenantTx` create/list/delete; `:execrows` guard |
| `api/cmd/api/main.go` | Modify | Wire `digest.NewHandler(digest.NewService(pool))` + `RegisterRoutes(r)` |
| `worker/lib/prompt.mjs` | Modify | Fetch digests + append bounded 3rd cached block (Decision 5) |
| `web/features/article-digest/api.ts` | New | Typed `listDigests`/`createDigest`/`deleteDigest` over `lib/api.ts` |
| `web/app/(app)/article-digest/page.tsx` | Modify | Replace `ComingSoon` with create-form + deletable-list CRUD page |

## Open Questions / Follow-ups

- **O-1 (migration number race) — RESOLVED by orchestrator**: `007` and `008`
  were both free when this design and the sibling `profile-persistence` design
  were written in parallel; both independently picked `007`. Orchestrator
  assigned `profile-persistence` (slice #1) → `007`, `article-digest` (slice #2)
  → `008`. `sdd-apply` should still verify against the real filesystem state of
  `db/migrations/` before writing the file, in case landing order changes which
  numbers are actually free by then.
- **F-1 (edit-in-place deferred)** — no `UPDATE`/`PATCH` this slice (out of
  scope). The `GRANT` already includes `UPDATE` and `cvs` carries an `UpdateCV`
  template, so a later `PATCH /api/article-digests/{id}` is a clean add, not a
  migration.
- **F-2 (RLS test)** — the acceptance checklist requires a pgTAP/RLS test proving
  cross-tenant read+delete denial on `article_digests`, consistent with the
  existing `tenant_cvs` coverage. That is a `tasks.md` item, not a design open
  question — recorded here so it is not lost.

## Readiness

Design is complete and self-consistent with the approved proposal. All three
proposal-flagged design questions are resolved: (1) migration number + real
convention (`008`, sequential — NOT the stale bootstrap; `007` reserved for
the sibling `profile-persistence` change), (2) truncation
mechanics (per-entry 4000 THEN running-total 24000, drop whole entries,
numbers confirmed), (3) exact sqlc/Go wiring (`:execrows` delete guard, 2-file
package matching real `cv` shape). Ready for `sdd-tasks`.
