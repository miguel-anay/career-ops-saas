# Apply Progress — `ingest-cv`

> Batch: 1 (Seam A only — DB schema + migration + sqlc)
> Branch: `feat/ingest-cv-db`
> Strict TDD: active (RED -> GREEN per task, sequential T-79 -> T-84)

## Task status

| ID | Description | Status | Commit |
|----|--------------|--------|--------|
| T-79 | pgTAP RLS test `db/tests/cv_ingestions_rls.test.sql` | done | `907dfe2` (test), revised in `8a9e048` |
| T-80 | `db/migrations/002_ingest_cv.sql` — `cv_ingestions` table + RLS + `usage.ingestions_count` | done | `274ba2c` |
| T-81 | Mirror DDL into `db/schema.sql` + `db/rls.sql` | done | `b60cff5` |
| T-82 | sqlc queries: `cv_ingestions.sql` (Insert/Get/UpdateStatus) + `usage.sql` (`UpsertIncrementIngestions`) | done | `f90d415` |
| T-83 | Regenerate sqlc Go types (`CvIngestion` struct, query methods, `Usage.IngestionsCount`) | **blocked** | n/a — see Blockers |
| T-84 | Run `make test-rls`, confirm T-79 passes against the live migration | **partial** | n/a — see Deviations |

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
   - Split the `pg_prove` invocation: the legacy `rls_test.sql` still runs as `careerops` (unchanged from before —
     out of scope to redesign), and the new `cv_ingestions_rls.test.sql` runs as `app_user` via a second
     `pg_prove` call with `PGPASSWORD=app_pw`.

## Blockers

### T-83 — sqlc Go code generation blocked by file ownership

`sqlc generate` (v1.31.1, found at `/home/k3n5h1n/gopath/bin/sqlc`) runs successfully and **does** parse the new
queries correctly — it generated a correct `cv_ingestions.sql.go` on the first attempt (proven by inspection, then
removed to avoid leaving inconsistent code, see below). However it failed to **overwrite** every other generated
file in `api/internal/db/` (`usage.sql.go`, `models.go`, `db.go`, `jobs.sql.go`, etc.):

```
open /home/k3n5h1n/Escritorio/career-saas/career-ops-saas/api/internal/db/usage.sql.go: permission denied
```

Root cause: those files are owned `root:root`, mode `644`, with no group/world write bit:

```
-rw-r--r-- 1 root root 2202 jun  4 00:36 usage.sql.go
```

This is almost certainly leftover from an earlier `docker compose` run that executed `sqlc generate` (or wrote to
that bind-mounted path) as the container's root user. My user (`k3n5h1n`, uid 1000) is in the `sudo` group but this
session has no way to supply an interactive sudo password, so I cannot `chown`/`chmod` those files myself.

**I removed the partially-generated `cv_ingestions.sql.go`** (it referenced the `CvIngestion` struct, which would
not exist in `models.go` since that file could not be overwritten — leaving it would have produced a
non-compiling package). The `api/internal/db/` directory is therefore unchanged from `main` after this batch.

**Action needed before Seam B (PR-B) can compile:** someone with sudo/root needs to run, once:
```
sudo chown -R $(whoami):$(whoami) api/internal/db/
```
or equivalently fix the file ownership, then re-run:
```
cd db && /home/k3n5h1n/gopath/bin/sqlc generate
```
This regenerates `CvIngestion`, `InsertCVIngestion`, `GetCVIngestion`, `UpdateCVIngestionStatus`,
`UpsertIncrementIngestions`, and `Usage.IngestionsCount` — all required before Seam B's Go service code compiles.

### T-84 — `make test-rls` fails as a whole (pre-existing, out-of-scope bug)

Running the full `make test-rls` target halts on the **pre-existing** `db/tests/rls_test.sql` (not touched by
this change), independent of any `cv_ingestions` work:

```
psql:/db/tests/rls_test.sql:70: ERROR:  column "rowsecurity" does not exist
LINE 2:   (SELECT rowsecurity FROM pg_class WHERE relname = 'users')...
HINT:  Perhaps you meant to reference the column "pg_class.relrowsecurity".
```

`rls_test.sql` references `pg_class.rowsecurity`, which does not exist on PostgreSQL 16 (the real column is
`relrowsecurity`). This means `rls_test.sql` could never have passed in this environment, for any table, old or
new — it has 24/24 subtests fail on the very first assertion. Additionally, even if that column name were fixed,
`rls_test.sql` is invoked as `careerops` (superuser), which bypasses RLS unconditionally — so its cross-tenant
assertions would also be false positives until it's switched to run as `app_user` (the same fix applied to the
new `cv_ingestions` test in this batch).

Both issues are in a file outside Seam A's scope (`cv_ingestions`), so I did not modify `rls_test.sql`. The new
`cv_ingestions_rls.test.sql` test (T-79) **does** pass, verified standalone as documented above. `make test-rls`
as a single target does not yet exit 0 because Make stops at the first failing command (the legacy file), before
reaching the new one.

**Recommendation:** a follow-up task (outside this PR-A) should fix `db/tests/rls_test.sql`'s `rowsecurity` ->
`relrowsecurity` column name and switch its `pg_prove` invocation to run as `app_user`, mirroring this batch's fix.

## Files changed (this batch)

- `db/tests/cv_ingestions_rls.test.sql` (new, then revised)
- `db/migrations/002_ingest_cv.sql` (new)
- `db/schema.sql` (mirrored DDL)
- `db/rls.sql` (mirrored DDL)
- `db/queries/cv_ingestions.sql` (new)
- `db/queries/usage.sql` (extended)
- `Makefile` (test-rls target fix)
- `docker-compose.yml` (postgres service: added `db/tests` mount)

## Commits (in order)

1. `907dfe2` — `test(db): pgTAP RLS for cv_ingestions`
2. `274ba2c` — `feat(db): cv_ingestions table + usage.ingestions_count migration`
3. `8a9e048` — `fix(db): run pgTAP cv_ingestions test as RLS-enforced app_user`
4. `b60cff5` — `feat(db): mirror cv_ingestions DDL into schema.sql and rls.sql`
5. `f90d415` — `feat(db): sqlc queries for cv_ingestions`

## Next steps

1. Fix `api/internal/db/` file ownership (needs sudo) and re-run `sqlc generate` to complete T-83.
2. Re-run `make test-rls` after fixing `rls_test.sql`'s column name + role (separate, out-of-scope follow-up) to
   get a clean exit 0 for the whole target — or accept the documented standalone PASS for `cv_ingestions_rls.test.sql`
   as T-84's evidence for this PR.
3. Seam B (Go API ingest/status endpoints) cannot start until T-83 completes — it depends on the generated
   `CvIngestion` struct and `Usage.IngestionsCount` field.
