# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run all unit tests
make test-all

# Per layer
make test-go          # cd api && go test ./... -count=1
make test-worker      # cd worker && npm test
make test-web         # cd web && npm test -- --run
make test-rls         # pgTAP RLS tests (requires Docker)

# Single Go test
cd api && go test ./internal/jobs/... -run TestDetectPlatform -v

# Single vitest file (worker or web)
cd worker && npx vitest run tests/scan.test.mjs
cd web && npx vitest run __tests__/jobs.test.tsx

# Regenerate sqlc Go types from db/queries/
cd db && sqlc generate

# Start all services
docker compose up
```

## Architecture

Three-service monorepo backed by a single PostgreSQL 16 instance:

| Service | Role |
|---------|------|
| `api/` | Control plane — auth, routing, WebSocket hub, pg-boss enqueue |
| `worker/` | Data plane — ATS scraping, Anthropic evaluation, PDF generation |
| `web/` | Presentation — communicates only with Go API over HTTP + WebSocket |

The Go API **never** scrapes, **never** calls Anthropic, **never** launches Chromium. The Node.js worker **never** handles auth or routing.

## Go API — `api/`

**Hexagonal pattern per domain**: each package under `internal/` has:
- `handler.go` — defines a local `Servicer` interface + `Handler` struct; wires HTTP routes
- `service.go` — implements `Servicer`; contains business logic
- `repo.go` — thin wrapper around sqlc `*db.Queries`

```
internal/
├── auth/         # Google OAuth2 + JWT (HS256, 1h access / 7d refresh with rotation)
├── jobs/         # Job CRUD, platform detection
├── scan/         # Trigger scan runs, read scan status
├── evaluate/     # Enqueue evaluation, read reports
├── cv/           # CV upload/read
├── companies/    # Watched company management
├── tracker/      # Application status tracker
├── ws/           # WebSocket hub (keyed by scan_run_id)
├── queue/        # pg-boss enqueue helpers
├── db/           # sqlc-generated code (DO NOT EDIT)
├── middleware/   # JWT auth middleware + SetUserID/GetUserID context helpers
├── platform/     # pgxpool setup, R2 client
└── config/       # Env var loader
```

**Tenant isolation in Go**: services call `middleware.SetUserID(ctx, id)` to inject the user UUID; sqlc queries use the pgxpool directly — RLS is enforced at the DB layer by setting `app.current_user_id` as a `SET LOCAL` inside each transaction.

**Test injection**: use `middleware.SetUserID(ctx, userID)` to set auth context in tests. Use `testify/mock` for Servicer mocks.

## Node.js Worker — `worker/`

Entry: `index.mjs` — boots pg-boss, registers three job types:
- `scan-company` → `jobs/scan.mjs` — fetches job listings from ATS providers
- `evaluate-job` → `jobs/evaluate.mjs` — calls Anthropic (`lib/anthropic.mjs`)
- `generate-pdf` → `jobs/pdf.mjs` — renders CV PDF with Playwright

**Tenant isolation in Node**: every DB write goes through `tenantQuery(userId, sql, params)` in `lib/db.mjs`, which wraps queries in `BEGIN / SET LOCAL app.current_user_id = $1 / COMMIT`.

**ATS Providers** (`providers/`): `greenhouse`, `ashby`, `lever`, `recruitee`, `smartrecruiters`, `workable`. Each exports a default object with `id` and `fetch(ctx)`.

**Anthropic call** (`lib/anthropic.mjs`): uses `claude-sonnet-4-6`, `max_tokens: 8000`, `temperature: 0.2`, with prompt caching on system + CV prefix blocks.

## Next.js Frontend — `web/`

- All API calls go through `lib/api.ts` — handles token refresh automatically.
- `lib/auth.ts` — localStorage token storage helpers.
- App Router structure under `app/` — routes: `/`, `/login`, `/jobs`, `/companies`, `/tracker`, `/auth`.
- Tests in `__tests__/` using vitest + `@testing-library/react`.

## Database — `db/`

- Schema: `db/schema.sql` — 8 tables, all with `FORCE ROW LEVEL SECURITY`.
- RLS policies: `db/rls.sql` — policies read `current_setting('app.current_user_id')`.
- sqlc config: `db/sqlc.yaml` — queries in `db/queries/`, output to `api/internal/db/`.
- Migrations: `db/migrations/001_initial.sql` (single bootstrap file at MVP).
- Queue tables: managed by pg-boss internally; no manual schema needed.

## Key Architectural Decisions

| ADR | Decision |
|-----|----------|
| ADR-1 | pg-boss over Redis — transactional enqueue, no extra broker |
| ADR-3 | RLS over app-layer filtering — DB invariant; missed WHERE = breach |
| ADR-5 | Cloudflare R2 — zero egress, S3-compatible |

## Environment Variables

Required for `api/`: `DATABASE_URL`, `JWT_SECRET`, `JWT_REFRESH_SECRET`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL`.  
Optional for `api/`: `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, `PORT` (default `:8080`), `WEB_ORIGIN`.  
Required for `worker/`: `DATABASE_URL`, `ANTHROPIC_API_KEY`.

Copy `.env.example` → `.env` before starting.
