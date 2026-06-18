# Apply Progress — `ingest-cv`

> Batch: 1 (Seam A only — DB schema + migration + sqlc) + Batch 2 (T-83/T-84 close-out + legacy RLS fix)
> Branch: `feat/ingest-cv-db`
> Strict TDD: active (RED -> GREEN per task, sequential T-79 -> T-84)
> Seam A status: **COMPLETE**. Do not start Seam B/C/D/E from this record without a fresh tasks read.

## Task status

| ID | Description | Status | Commit |
|----|--------------|--------|--------|
| T-79 | pgTAP RLS test `db/tests/cv_ingestions_rls.test.sql` | done | `907dfe2` (test), revised in `8a9e048` |
| T-80 | `db/migrations/002_ingest_cv.sql` — `cv_ingestions` table + RLS + `usage.ingestions_count` | done | `274ba2c` |
| T-81 | Mirror DDL into `db/schema.sql` + `db/rls.sql` | done | `b60cff5` |
| T-82 | sqlc queries: `cv_ingestions.sql` (Insert/Get/UpdateStatus) + `usage.sql` (`UpsertIncrementIngestions`) | done | `f90d415` |
| T-83 | Regenerate sqlc Go types (`CvIngestion` struct, query methods, `Usage.IngestionsCount`) | **done** | `614b61d` |
| T-84 | Run `make test-rls`, confirm T-79 passes against the live migration | **done** | n/a (verification only) — see Batch 2 below |
| — | Fix legacy `db/tests/rls_test.sql` false-positive (user-approved, folded into Seam A) | **done** | `876643e` |

## Batch 2 — close-out (this session)

**Unblocked T-83**: the `api/internal/db/` file ownership issue from Batch 1 was resolved externally (files are
now `k3n5h1n:k3n5h1n`, writable). Ran `sqlc generate` from `db/` with the same binary
(`/home/k3n5h1n/gopath/bin/sqlc`, v1.31.1) used in Batch 1. Clean exit, no errors. Generated:
- `api/internal/db/cv_ingestions.sql.go` (new) — `CvIngestion` struct usage via `GetCVIngestion`,
  `InsertCVIngestion`, `UpdateCVIngestionStatus` (+ `UpdateCVIngestionStatusParams`).
- `api/internal/db/models.go` (modified) — added `CvIngestion` struct, added `Usage.IngestionsCount` field.
- `api/internal/db/usage.sql.go` (modified) — `UpsertIncrementIngestions` + `UpsertIncrementIngestionsParams`,
  and the `IngestionsCount` field threaded through all three existing `usage` query scans.

Verified `cd api && go build ./...` (clean) and `go test ./... -count=1` (all packages pass, no regressions).
Committed as `614b61d` — `chore(db): regenerate sqlc types for cv_ingestions`.

**Fixed the legacy `db/tests/rls_test.sql` false positive** (user-approved scope addition, still Seam A —
this is the DB/RLS layer, not Seam B/C/D/E):
- Root cause #1: ran as `careerops` (the `POSTGRES_USER`, a Postgres superuser). Superusers bypass RLS
  unconditionally regardless of `FORCE ROW LEVEL SECURITY` — every cross-tenant assertion in the file was a
  false positive that happened to read `0` rows by coincidence of context, not because RLS was enforced.
- Root cause #2: asserted on `pg_class.rowsecurity`, which does not exist on PostgreSQL 16 (correct column:
  `relrowsecurity`). This made the file fail outright (24/24 subtests) before it could even reach the
  superuser-bypass problem.
- Root cause #3 (discovered while fixing, not previously documented): `plan(24)` undercounted — the file has
  always contained 25 `ok()`/`is()` assertions, not 24. This was masked previously because execution died on
  assertion #1 before the plan-mismatch could surface. Fixed to `plan(25)`.
- Fix applied: seed both tenant users via `auth_upsert_user` (SECURITY DEFINER, bypasses RLS for setup exactly
  as production OAuth signup does) instead of a direct `INSERT INTO users` with hardcoded UUIDs — a direct
  insert for "user B" while `app.current_user_id` is set to user A would violate the `tenant_users` `WITH CHECK`
  policy under `app_user`. Captured the generated UUIDs via `set_config('test.user_a'/'test.user_b', ..., false)`
  and used them as FKs for all other seeded rows (watched_companies, jobs, applications, reports, cvs,
  scan_runs, usage — those tables' own PKs stay as the original hardcoded literals, only the `user_id` FK
  changed). Switched every `pg_class` assertion from `rowsecurity` to `relrowsecurity`. Switched the
  `SET app.current_user_id = '...'` literal-role-switch lines to `set_config` calls (wrapped in `DO $$ BEGIN
  PERFORM ... END $$;` to avoid emitting a spurious extra TAP line — a bare top-level `SELECT set_config(...)`
  is counted by pg_prove/psql's TAP harness as its own subtest result line).
- Verified RED first: ran the unmodified file as `careerops` — confirmed `Failed 24/24 subtests`,
  `ERROR: column "rowsecurity" does not exist`. This is the evidence the test was broken.
- Verified GREEN after: ran as `app_user` — `Tests=25 ... Result: PASS`, all genuine RLS assertions.
- Updated `Makefile`'s `test-rls` target: `rls_test.sql` now runs via the same `app_user`/`PGPASSWORD=app_pw`
  `pg_prove` invocation pattern as `cv_ingestions_rls.test.sql` (was `-U careerops`).
- Committed as `876643e` — `fix(db): run legacy RLS pgTAP test as app_user and use relrowsecurity`.

**Final verification — full `make test-rls` target, clean run from a stopped container:**
```
docker compose exec ... pg_prove -U app_user -d careerops /db/tests/rls_test.sql
  /db/tests/rls_test.sql .. ok
  All tests successful.
  Files=1, Tests=25,  Result: PASS

docker compose exec ... pg_prove -U app_user -d careerops /db/tests/cv_ingestions_rls.test.sql
  /db/tests/cv_ingestions_rls.test.sql .. ok
  All tests successful.
  Files=1, Tests=4,  Result: PASS
```
Target exits 0 end-to-end (no longer halts on the legacy file). `make test-go` also reconfirmed green after the
sqlc regeneration (11 Go packages, all `ok` or no-test-files, zero failures).

## Deviations from the plan

1. **T-79 test rewritten to seed users via `auth_upsert_user` (SECURITY DEFINER) instead of a direct `INSERT INTO users`.**
   Discovery: the Docker Postgres role `careerops` (`POSTGRES_USER`) is a **superuser**, and Postgres superusers
   unconditionally bypass RLS regardless of `FORCE ROW LEVEL SECURITY`. Running the pgTAP assertions as `careerops`
   would be a false positive — it never exercises the policy. The test now seeds the two tenant users through the
   `auth_upsert_user` SECURITY DEFINER helper (mirrors production OAuth signup) and runs entirely as `app_user`,
   the real RLS-enforced runtime role. Verified standalone:
   ```
   docker compose exec -T -e PGPASSWORD=app_pw postgres pg_prove -U app_user -d careerops /db/tests/cv_ingestions_rls.test.sql
   # Files=1, Tests=4, Result: PASS
   ```

2. **`docker-compose.yml` and `Makefile` updated (infra fix, in scope for T-84 to be meaningful).**
   - Added a bind mount `./db/tests:/db/tests` on the `postgres` service — previously no test files were reachable
     inside the container at all except via `docker cp`.
   - `Makefile`'s `test-rls` target used `docker compose run --rm ...` for the pgtap-extension-create and pg_prove
     steps. `run` spins a brand-new ephemeral container from the base image, which never has the `pgtap`/`pg_prove`
     packages that an earlier `apt-get install` (via `exec`, against the long-lived service container) put in place.
     Changed both steps to `exec` against the running `postgres` service.
   - `Makefile`'s `apt-get install -y -qq pgtap` resolves to `postgresql-18-pgtap` on this image's apt mirror, even
     though the running server is PostgreSQL 16 — the extension control file then doesn't exist for PG16 and
     `CREATE EXTENSION pgtap` fails. Pinned to `postgresql-16-pgtap` explicitly.
   - Added an explicit apply of `002_ingest_cv.sql` before running tests (the existing line only re-applies
     `001_initial.sql`).
   - Split the `pg_prove` invocation. NOTE (corrected): at Batch 1 time the legacy `rls_test.sql` was left running
     as `careerops`; this was **subsequently changed in the review-fix batch** (see Blockers → T-84 below) to run as
     `app_user` so it genuinely exercises RLS. The new `cv_ingestions_rls.test.sql` runs as `app_user` via a second
     `pg_prove` call with `PGPASSWORD=app_pw`.

## Blockers (Batch 1) — both resolved in Batch 2

### T-83 — sqlc Go code generation blocked by file ownership — RESOLVED

Originally blocked: `api/internal/db/*.go` were owned `root:root` (leftover from an earlier `docker compose` run
that wrote there as root), and this session's user had no way to `chown`/`chmod` them without an interactive
sudo password. **Resolution (Batch 2)**: ownership was fixed externally to `k3n5h1n:k3n5h1n` before this batch
started. Verified writable, ran `sqlc generate` cleanly, regenerated `CvIngestion`, `InsertCVIngestion`,
`GetCVIngestion`, `UpdateCVIngestionStatus`, `UpsertIncrementIngestions`, `Usage.IngestionsCount`. `go build ./...`
and `go test ./... -count=1` both green. Committed as `614b61d`. Seam B is now unblocked.

### T-84 — `make test-rls` fails as a whole (pre-existing bug in `rls_test.sql`) — RESOLVED

Originally: the full `make test-rls` target halted on the pre-existing `db/tests/rls_test.sql` (`pg_class.rowsecurity`
does not exist on PG16 — correct column is `relrowsecurity`), and even after that fix the file ran as `careerops`
(superuser), which bypasses RLS unconditionally — a false positive. **Resolution (Batch 2)**: user explicitly
approved folding this fix into Seam A/PR-A (see task instructions). Fixed `rowsecurity` -> `relrowsecurity`,
switched seeding to `auth_upsert_user` (SECURITY DEFINER) so the whole file can run as `app_user`, fixed a
previously-undiscovered `plan(24)` vs. actual-25-assertions mismatch, and updated the `Makefile` to invoke
`pg_prove -U app_user` for `rls_test.sql` (was `-U careerops`). Verified RED (24/24 fail, `rowsecurity` error)
before the fix and GREEN (25/25 pass) after, both as evidence in this same session. Committed as `876643e`.
`make test-rls` as a whole now exits 0.

## Files changed (Batch 1 + Batch 2, cumulative)

- `db/tests/cv_ingestions_rls.test.sql` (new, then revised) — Batch 1
- `db/migrations/002_ingest_cv.sql` (new) — Batch 1
- `db/schema.sql` (mirrored DDL) — Batch 1
- `db/rls.sql` (mirrored DDL) — Batch 1
- `db/queries/cv_ingestions.sql` (new) — Batch 1
- `db/queries/usage.sql` (extended) — Batch 1
- `Makefile` (test-rls target fix — infra in Batch 1, app_user switch for legacy test in Batch 2) — Batch 1 + 2
- `docker-compose.yml` (postgres service: added `db/tests` mount) — Batch 1
- `api/internal/db/cv_ingestions.sql.go` (new, sqlc-generated) — Batch 2
- `api/internal/db/models.go` (sqlc-generated: `CvIngestion` struct, `Usage.IngestionsCount`) — Batch 2
- `api/internal/db/usage.sql.go` (sqlc-generated: `UpsertIncrementIngestions`) — Batch 2
- `db/tests/rls_test.sql` (fixed: `relrowsecurity`, `app_user` seeding via `auth_upsert_user`, `plan(25)`) — Batch 2

## Commits (in order)

1. `907dfe2` — `test(db): pgTAP RLS for cv_ingestions`
2. `274ba2c` — `feat(db): cv_ingestions table + usage.ingestions_count migration`
3. `8a9e048` — `fix(db): run pgTAP cv_ingestions test as RLS-enforced app_user`
4. `b60cff5` — `feat(db): mirror cv_ingestions DDL into schema.sql and rls.sql`
5. `f90d415` — `feat(db): sqlc queries for cv_ingestions`
6. `614b61d` — `chore(db): regenerate sqlc types for cv_ingestions`
7. `876643e` — `fix(db): run legacy RLS pgTAP test as app_user and use relrowsecurity`

## Seam A status: COMPLETE

All of T-79..T-84 are done, plus the user-approved legacy `rls_test.sql` fix. `make test-rls` (25/25 + 4/4) and
`make test-go` (11 packages, all pass) are both green end-to-end on branch `feat/ingest-cv-db`. Not pushed, no PR
opened — orchestrator reviews first per task constraints.

## Next steps (Seam B and beyond — NOT started in this batch)

1. Seam B (Go API ingest/status endpoints) can now start — it depends on the generated `CvIngestion` struct and
   `Usage.IngestionsCount` field, both of which now exist and compile.
2. Do not start Seam B/C/D/E from this apply-progress record without first re-reading the `tasks` artifact —
   this record only covers Seam A.
