# career-ops-saas

AI-powered job search pipeline as a SaaS — multi-tenant, production-ready.

## Architecture

Three-service monorepo backed by a single PostgreSQL instance:

```
career-ops-saas/
├── api/          # Go + chi — auth, routing, WebSocket hub, job queue
├── worker/       # Node.js + Playwright — ATS scan, Anthropic evaluation, PDF
├── web/          # Next.js + shadcn/ui — dashboard, job detail, tracker
├── db/           # SQL schema, RLS policies, sqlc queries, migrations
├── docker-compose.yml
├── .env.example
└── README.md
```

## Responsibility split

- **Go API** — control plane: auth (Google OAuth2 + JWT), routing, WebSocket hub, RLS tenant variable, enqueues work via pg-boss. Never scrapes, never calls Anthropic, never launches Chromium.
- **Node.js worker** — data plane: consumes pg-boss jobs, runs ATS providers, calls Anthropic, renders PDFs with Playwright.
- **Next.js web** — presentation layer: communicates only with Go API over HTTP and WebSocket.

## Quick start

```bash
cp .env.example .env
# Fill in your credentials in .env
docker compose up
```

The API listens on :8080, the worker on :3001, and the web on :3000.

## Database

PostgreSQL 16 with full Row-Level Security. Each service uses a dedicated DB role (`app_user`) that is subject to RLS — tenant isolation is enforced at the database layer, not the application layer.

Run migrations:
```bash
docker compose exec postgres psql -U careerops -f /docker-entrypoint-initdb.d/001_initial.sql
```

## Key architectural decisions

| # | Decision | Reason |
|---|---|---|
| ADR-1 | pg-boss over Redis/BullMQ | PostgreSQL already in stack; transactional enqueue; no extra broker |
| ADR-2 | Node.js worker retains providers | 6 production-ready providers with battle-tested logic; Playwright has no mature Go equivalent |
| ADR-3 | RLS over app-layer filtering | RLS is a DB invariant; a missed WHERE clause = data breach |
| ADR-4 | Monorepo at MVP | Schema changes + Go queries + worker updates in a single atomic PR |
| ADR-5 | Cloudflare R2 over AWS S3 | Zero egress fees; S3-compatible API |
| ADR-6 | UUID PKs | Non-guessable; minteable in worker before INSERT |
