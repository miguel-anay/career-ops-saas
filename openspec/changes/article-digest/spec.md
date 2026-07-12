# Spec Delta: article-digest

> Formalizes `openspec/changes/article-digest/proposal.md` into requirements +
> scenarios. Describes WHAT must be true after the change ships — no
> implementation mechanics beyond what the proposal already fixed (table
> shape, RLS policy shape, endpoint list, N=20/24KB ceilings). Truncation
> mechanics, migration numbering, and exact Go/sqlc wiring are `sdd-design`'s
> job per the proposal's "Next phases" section.

## Domain: article-digest (NEW)

### Requirement: Create a digest entry scoped to the authenticated user

`POST /api/article-digests` MUST accept `{title, content_md}`, insert a row
into `article_digests` owned by the requesting user (`user_id` taken from
the authenticated session, never from the request body), and return HTTP
201 with the created row (including server-assigned `id` and `created_at`).

#### Scenario: Authenticated user creates a digest entry

- GIVEN an authenticated user with a valid session
- WHEN they POST `/api/article-digests` with `{title, content_md}`
- THEN the API responds 201 with the created row
- AND the row's `user_id` equals the requesting user's ID
- AND the row is persisted in `article_digests`

### Requirement: List returns only the current user's entries, newest first

`GET /api/article-digests` MUST return exclusively the requesting user's
`article_digests` rows, ordered by `created_at DESC`, regardless of how
many entries other users have.

#### Scenario: User lists their own entries

- GIVEN an authenticated user who has created 3 digest entries at different times
- WHEN they GET `/api/article-digests`
- THEN the response contains exactly those 3 entries
- AND they are ordered newest-first by `created_at`

#### Scenario: User with zero digest entries gets an empty list

- GIVEN an authenticated user who has never created a digest entry
- WHEN they GET `/api/article-digests`
- THEN the API responds 200 with an empty array
- AND no error is raised

### Requirement: Delete removes exactly one owned entry, scoped by user

`DELETE /api/article-digests/{id}` MUST remove the entry only when it
belongs to the requesting user. The delete query itself MUST scope on both
`id` and `user_id` (defense-in-depth alongside RLS, matching this project's
existing tenant-delete discipline) — a delete attempt against another
user's entry MUST NOT succeed via either layer.

#### Scenario: User deletes their own entry

- GIVEN an authenticated user who owns a digest entry with a given `id`
- WHEN they DELETE `/api/article-digests/{id}`
- THEN the row is removed from `article_digests`
- AND a subsequent GET by that user no longer lists it

#### Scenario: User attempts to delete another user's entry

- GIVEN two users A and B, where B owns a digest entry with `id = X`
- WHEN A sends DELETE `/api/article-digests/X`
- THEN the delete affects zero rows (RLS hides B's row from A's session, and
  the query's explicit `WHERE id = $1 AND user_id = $2` scope independently
  excludes it even if RLS were ever misconfigured)
- AND B's entry still exists afterward
- AND the API does not report success as if A's own row had been deleted

### Requirement: Row-level security enforces tenant isolation on `article_digests`

`article_digests` MUST have `ENABLE ROW LEVEL SECURITY` and
`FORCE ROW LEVEL SECURITY`, plus a `tenant_article_digests` policy using the
same `user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid`
shape as `tenant_cvs` (`db/rls.sql:60-62`), applied to both `USING` and
`WITH CHECK`. This MUST land in the same migration as the table (ADR-3 — no
table-then-later-RLS gap).

#### Scenario: Cross-tenant read is denied at the database level

- GIVEN user A has one `article_digests` row and user B has none
- WHEN a query runs with `app.current_user_id` set to user B's ID
- THEN `SELECT * FROM article_digests` returns zero rows
- AND a direct `SELECT * FROM article_digests WHERE id = <user A's row id>` also returns zero rows

#### Scenario: Cross-tenant delete is denied at the database level

- GIVEN user A has one `article_digests` row
- WHEN a `DELETE FROM article_digests WHERE id = <user A's row id>` runs with
  `app.current_user_id` set to user B's ID
- THEN zero rows are affected
- AND user A's row is unaffected

#### Scenario: Owner can read and delete their own row

- GIVEN a user with `app.current_user_id` set to their own ID
- WHEN they SELECT or DELETE their own `article_digests` row by ID
- THEN the operation succeeds normally, unaffected by the tenant policy

## Domain: worker-evaluate-job (MODIFIED)

> Adds to the canonical spec at `openspec/specs/worker-evaluate-job/spec.md`.
> This section MUST be merged into that canonical spec at archive time (per
> the project's `sdd-archive` convention) — it is not merged here.
> Current prompt shape (`worker/lib/prompt.mjs`, confirmed by reading the
> file): two cached `system[]` blocks — `staticSystemPrompt` and
> `cvAndProfileBlock`, both `cache_control: { type: 'ephemeral' }`. This
> requirement adds a third.

### Requirement: A third cached system block carries the user's article digests, bounded and ordered newest-first

`buildEvaluationPrompt` MUST append a third `system[]` entry (after the
existing static-prompt and CV+profile blocks), built from the requesting
user's `article_digests` ordered `created_at DESC`, capped at **N=20
entries** and an **overall ~24,000-character ceiling** on the concatenated
block text, with the same `cache_control: { type: 'ephemeral' }` treatment
as the existing two blocks. Rows beyond the cap (by count or by running
total length) MUST NOT be included; the newest entries always win the cap
over older ones.

#### Scenario: User has fewer than 20 entries, all under the character ceiling

- GIVEN a user with 5 `article_digests` rows totaling well under 24,000 characters
- WHEN `buildEvaluationPrompt` runs for that user
- THEN the third system block includes all 5 entries, newest-first
- AND the block carries `cache_control: { type: 'ephemeral' }`

#### Scenario: User has more than 20 entries

- GIVEN a user with 30 `article_digests` rows
- WHEN `buildEvaluationPrompt` runs for that user
- THEN the third system block includes at most the 20 most recent entries
- AND the 10 oldest entries are excluded

#### Scenario: Concatenated entries exceed the character ceiling before reaching 20

- GIVEN a user with entries whose combined `content_md` length exceeds
  24,000 characters before all 20 most-recent entries are included
- WHEN `buildEvaluationPrompt` runs for that user
- THEN the block stops including further (older) entries once the ceiling
  is reached, keeping the newest entries that fit
- AND the total block text does not exceed the ~24,000-character ceiling

### Requirement: The digest block is omitted entirely when the user has zero entries

When the requesting user has no `article_digests` rows, `buildEvaluationPrompt`
MUST NOT add a third `system[]` entry at all — not an empty block, not a
block with a header and no content. The resulting `system[]` array MUST
have exactly the same two entries as before this change.

#### Scenario: User has zero digest entries

- GIVEN a user with no rows in `article_digests`
- WHEN `buildEvaluationPrompt` runs for that user
- THEN the returned `system[]` array has exactly 2 entries (static prompt +
  CV/profile block)
- AND no third entry — empty, header-only, or otherwise — is present

## Open questions for `sdd-design`

- **N=20 / ~24,000-char ceiling numbers are the proposal's estimate, not a
  measured token count.** The proposal itself flags this ("the ceiling is a
  guess... Design should confirm the numbers against a real token count").
  This spec treats the numbers as fixed, testable requirements per the task
  instructions, but `sdd-design` should validate them against actual
  digest-entry sizes before `sdd-apply` and may need to adjust — if so, this
  spec's numbers must be updated to match, not silently diverge.
- **Per-entry truncation (the proposal's "~4,000 chars per entry" cap) is
  not written as its own testable requirement here** — the proposal frames
  exact truncation mechanics (per-entry vs. running-total, ellipsis marker)
  as a design-time decision. Only the two ceilings the proposal commits to
  (N=20 count, ~24,000 total chars) are specified as requirements above.
  `sdd-design` should decide whether per-entry truncation becomes a third
  testable requirement or stays an implementation detail of how the
  24,000-char ceiling is enforced.
