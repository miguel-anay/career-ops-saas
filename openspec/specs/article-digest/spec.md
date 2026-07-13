# Spec: Article Digest CRUD & Evaluation Enrichment

## Purpose

This capability spec documents the article-digest feature — a user-facing table for storing per-project proof-point entries (title + markdown body), with automatic injection into job evaluations as a bounded, cached prompt block.

## Context

Job evaluations read two signals about a candidate: `users.cv_markdown` and `users.profile_json`. This is insufficient for rich evaluation — a CV line like "Built fraud detection pipeline" carries none of the hero metrics, architecture decisions, or proof points that actually win an evaluation. The article-digest feature lets users supply project-level evidence (portfolio write-ups, README-style digests) so evaluators can cite concrete achievements.

## Requirements

### Requirement: Create a digest entry scoped to the authenticated user

`POST /api/article-digests` MUST accept `{title, content_md}`, insert a row into `article_digests` owned by the requesting user (`user_id` taken from the authenticated session, never from the request body), and return HTTP 201 with the created row (including server-assigned `id` and `created_at`).

#### Scenario: Authenticated user creates a digest entry

- GIVEN an authenticated user with a valid session
- WHEN they POST `/api/article-digests` with `{title, content_md}`
- THEN the API responds 201 with the created row
- AND the row's `user_id` equals the requesting user's ID
- AND the row is persisted in `article_digests`

### Requirement: List returns only the current user's entries, newest first

`GET /api/article-digests` MUST return exclusively the requesting user's `article_digests` rows, ordered by `created_at DESC`, regardless of how many entries other users have.

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

`DELETE /api/article-digests/{id}` MUST remove the entry only when it belongs to the requesting user. The delete query itself MUST scope on both `id` and `user_id` (defense-in-depth alongside RLS, matching this project's existing tenant-delete discipline) — a delete attempt against another user's entry MUST NOT succeed via either layer.

#### Scenario: User deletes their own entry

- GIVEN an authenticated user who owns a digest entry with a given `id`
- WHEN they DELETE `/api/article-digests/{id}`
- THEN the row is removed from `article_digests`
- AND a subsequent GET by that user no longer lists it

#### Scenario: User attempts to delete another user's entry

- GIVEN two users A and B, where B owns a digest entry with `id = X`
- WHEN A sends DELETE `/api/article-digests/X`
- THEN the delete affects zero rows (RLS hides B's row from A's session, and the query's explicit `WHERE id = $1 AND user_id = $2` scope independently excludes it even if RLS were ever misconfigured)
- AND B's entry still exists afterward
- AND the API does not report success as if A's own row had been deleted

### Requirement: Row-level security enforces tenant isolation on `article_digests`

`article_digests` MUST have `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`, plus a `tenant_article_digests` policy using the same `user_id = NULLIF(current_setting('app.current_user_id', true), '')::uuid` shape as `tenant_cvs` (`db/rls.sql`), applied to both `USING` and `WITH CHECK`. This MUST land in the same migration as the table (ADR-3 — no table-then-later-RLS gap).

#### Scenario: Cross-tenant read is denied at the database level

- GIVEN user A has one `article_digests` row and user B has none
- WHEN a query runs with `app.current_user_id` set to user B's ID
- THEN `SELECT * FROM article_digests` returns zero rows
- AND a direct `SELECT * FROM article_digests WHERE id = <user A's row id>` also returns zero rows

#### Scenario: Cross-tenant delete is denied at the database level

- GIVEN user A has one `article_digests` row
- WHEN a `DELETE FROM article_digests WHERE id = <user A's row id>` runs with `app.current_user_id` set to user B's ID
- THEN zero rows are affected
- AND user A's row is unaffected

#### Scenario: Owner can read and delete their own row

- GIVEN a user with `app.current_user_id` set to their own ID
- WHEN they SELECT or DELETE their own `article_digests` row by ID
- THEN the operation succeeds normally, unaffected by the tenant policy
