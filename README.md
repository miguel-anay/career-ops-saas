# career-ops-saas

AI-powered job search pipeline as a SaaS — multi-tenant, production-ready.

## Architecture

Three-service monorepo backed by a single PostgreSQL instance:

```
career-ops-saas/
├── api/          # Go + chi — auth, routing, WebSocket hub, job queue
├── worker/       # Node.js + Playwright — ATS scan, Anthropic evaluation, PDF
├── web/          # Next.js + shadcn/ui — dashboard, jobs, companies, CV, tracker
├── db/           # SQL schema, RLS policies, sqlc queries, migrations
├── docker-compose.yml
├── .env.example
└── README.md
```

## Responsibility split

- **Go API** — control plane: auth (Google OAuth2 + JWT), routing, WebSocket hub, RLS tenant variable, enqueues work via pg-boss. Never scrapes, never calls Anthropic, never launches Chromium.
- **Node.js worker** — data plane: consumes pg-boss jobs, runs ATS providers, calls Anthropic, renders PDFs with Playwright.
- **Next.js web** — presentation layer: communicates only with Go API over HTTP and WebSocket.

## Development setup

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) + Docker Compose
- [Go 1.25+](https://go.dev/dl/) (only needed if running the API locally)
- Node.js 20+

### Option A — Full Docker (simplest)

```bash
cp .env.example .env
# Fill in GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, ANTHROPIC_API_KEY
docker compose up
```

The API listens on :8080, the web on :3000, and the worker exposes a health endpoint on :3001 (it consumes jobs via pg-boss, not HTTP).  
Migrations run automatically on first Postgres start.

### Option B — Hybrid (recommended for active development)

Run only Postgres in Docker; start the other services locally for hot-reload.

```bash
# 1. Start the database
docker compose up postgres

# 2. API  (new terminal)
cp .env.example .env          # set DATABASE_URL=postgres://app_user:app_pw@localhost:5432/careerops?sslmode=disable
cd api && go run ./cmd/api

# 3. Worker  (new terminal)
cd worker && node --watch index.mjs

# 4. Web  (new terminal)
cd web && npm install && npm run dev
```

### Required environment variables

| Variable | Where | Notes |
|----------|-------|-------|
| `GOOGLE_CLIENT_ID` | api | OAuth app from [console.cloud.google.com](https://console.cloud.google.com) |
| `GOOGLE_CLIENT_SECRET` | api | Same OAuth app |
| `GOOGLE_REDIRECT_URL` | api | `http://localhost:8080/auth/google/callback` for local dev |
| `JWT_SECRET` | api | Min 32 chars — generate with `openssl rand -base64 32` |
| `JWT_REFRESH_SECRET` | api | Min 32 chars — generate with `openssl rand -base64 32` |
| `DATABASE_URL` | api + worker | Postgres connection string |
| `ANTHROPIC_API_KEY` | worker | Required for job evaluation |
| `R2_*` | api + worker | Optional — PDF upload disabled if absent |

### Running tests

```bash
make test-all          # Go + worker + web unit tests
make test-rls          # pgTAP RLS tests (requires Docker)

# Single test
cd api && go test ./internal/jobs/... -run TestDetectPlatform -v
cd worker && npx vitest run tests/scan.test.mjs
cd web && npx vitest run __tests__/jobs.test.tsx
```

## Production setup

### Database

Use a managed Postgres 16 instance — **never** run the database on the same server as the application. Recommended options: [Neon](https://neon.tech) (serverless, free tier), [Railway](https://railway.app), or RDS.

Run migrations once after provisioning. The migrations are the source of truth —
each one is self-contained (schema + RLS policies + the `auth_upsert_user` function),
the same set Docker applies on first boot:

```bash
# 1. Schema + RLS + companies catalog — every migration, in order
for f in db/migrations/*.sql; do psql "$DATABASE_URL" -f "$f"; done

# 2. pg-boss queue schema. The runtime role (app_user) is RLS-subject and can't
#    self-migrate, so the worker installs the schema as the admin role, then we
#    grant app_user exactly the runtime privileges it needs.
node worker/scripts/install-pgboss.mjs              # connects as the admin role
psql "$PGBOSS_ADMIN_URL" -f db/pgboss_grants.sql    # run as the schema owner
```

> `db/rls.sql` and `db/auth_upsert_user.sql` are legacy standalone files kept for
> reference — their contents already live inside the migrations above, so you don't
> run them separately. `db/pgboss_schema.generated.sql` is a test fixture, not a
> deploy artifact.

### Services

Each service has its own `Dockerfile` and can be deployed independently:

| Service | Port | Deploy target |
|---------|------|---------------|
| `api/` | 8080 | Any container platform (Railway, Fly.io, Cloud Run) |
| `worker/` | 3001 | Same — needs `shm_size: 1gb` for Playwright |
| `web/` | 3000 | Vercel (recommended) or any Node host |

### Production environment variables

Same variables as development, with these changes:

- `GOOGLE_REDIRECT_URL` → your real domain, e.g. `https://api.yourdomain.com/auth/google/callback`
- `WEB_ORIGIN` → `https://yourdomain.com`
- `JWT_SECRET` / `JWT_REFRESH_SECRET` → strong random values (`openssl rand -base64 32`)
- `DATABASE_URL` → connection string from your managed Postgres provider

### Google OAuth setup for production

In [console.cloud.google.com](https://console.cloud.google.com), add your production domain to **Authorized redirect URIs**:  
`https://api.yourdomain.com/auth/google/callback`

## Database

PostgreSQL 16 with full Row-Level Security. Each service uses a dedicated DB role (`app_user`) that is subject to RLS — tenant isolation is enforced at the database layer, not the application layer.

## Key architectural decisions

| # | Decision | Reason |
|---|---|---|
| ADR-1 | pg-boss over Redis/BullMQ | PostgreSQL already in stack; transactional enqueue; no extra broker |
| ADR-2 | Node.js worker retains providers | 6 production-ready providers with battle-tested logic; Playwright has no mature Go equivalent |
| ADR-3 | RLS over app-layer filtering | RLS is a DB invariant; a missed WHERE clause = data breach |
| ADR-4 | Monorepo at MVP | Schema changes + Go queries + worker updates in a single atomic PR |
| ADR-5 | Cloudflare R2 over AWS S3 | Zero egress fees; S3-compatible API |
| ADR-6 | UUID PKs | Non-guessable; minteable in worker before INSERT |
