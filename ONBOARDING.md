# ONBOARDING.md

AI-powered job search pipeline as a multi-tenant SaaS. Three-service monorepo backed by a single PostgreSQL 16 instance.

## Stack

| Service | Stack | Role |
|---------|-------|------|
| `api/` | Go 1.25 · chi · pgx/v5 · golang-jwt/v5 · gorilla/websocket | Control plane — auth, routing, WebSocket hub, pg-boss enqueue |
| `worker/` | Node 20 (ESM) · pg-boss 10.4.2 · Anthropic SDK · Playwright | Data plane — ATS scraping, Anthropic evaluation, PDF generation |
| `web/` | Next.js 14 (App Router) · React · Tailwind · shadcn/ui | Presentation — talks only to the Go API over HTTP + WebSocket |
| `db/` | PostgreSQL 16 · sqlc · pgTAP | Schema, RLS policies, queue (pg-boss), LISTEN/NOTIFY |

## Estructura del proyecto

```
api/internal/        hexagonal por dominio (handler → Servicer → service → repo)
  auth companies cv evaluate jobs scan tracker ws queue platform middleware db
worker/
  domain/ application/ adapters/   DDD-lite (evaluate path)
  jobs/ lib/ providers/            job handlers, libs, ATS adapters
web/
  app/                             rutas (App Router)
  features/{jobs,companies,cv}/    componentes/hooks/api/types por feature
  components/ui/                   shadcn (global)
  lib/                             cliente API genérico, auth (global)
db/
  schema.sql rls.sql migrations/ queries/ tests/(pgTAP)
openspec/
  specs/                           specs canónicos (capabilities)
  changes/archive/                trail de cambios SDD cerrados
```

## Flujo de trabajo (Trunk-Based Development)

- **Feature / cambio grande**: SDD (explore → propose → spec → design → tasks) → `/tbd` (crea issue + branch + board "Todo") → apply → verify → `/tbd` (PR + board "Done")
- **Bug / cambio pequeño**: `/tbd "descripción"` → fix → `/tbd` (PR + cierre)
- Las ramas viven 1-2 días máximo. Todo mergea directo a `main`. No hay `develop`.

## Branch strategy

- `main`: única rama permanente, siempre deployable.
- `feat/N-nombre`: features (vida corta). `fix/N-nombre`: bugs/cambios chicos (vida corta).
- Conventional commits (`feat`/`fix`/`refactor`/`chore`/`docs`/`test`/`ci`). Sin atribución AI.
- Stacked-to-main para PRs encadenados.

## Comandos frecuentes

```bash
docker compose up                    # levantar todo (postgres + api + worker + web)
make test-all                        # Go + worker + web unit tests
make test-rls                        # pgTAP RLS (Docker, corre como app_user)
make pgboss-install                  # provisionar el schema pg-boss v10 (admin) + grants
cd db && sqlc generate               # regenerar tipos Go desde db/queries/
```

Tests por capa: `make test-go` · `make test-worker` · `make test-web`.

## Variables de entorno

Copiar `.env.example` → `.env`. Requeridas: `GOOGLE_CLIENT_ID/SECRET`, `GOOGLE_REDIRECT_URL`, `JWT_SECRET`, `JWT_REFRESH_SECRET`, `DATABASE_URL`, `ANTHROPIC_API_KEY`. Opcionales: `R2_*` (PDF upload), `SERPER_API_KEY`.

## Decisiones de arquitectura

- **RLS multi-tenant**: una sola DB, `FORCE ROW LEVEL SECURITY`, GUC `app.current_user_id` por transacción (`WithTenantTx` en Go, `tenantQuery` en worker). `companies_catalog` es reference data global (sin RLS).
- **pg-boss v10** como cola (sin Redis). Schema provisionado admin-side; worker corre `migrate:false` (app_user no tiene CREATE).
- **Arquitectura por servicio según complejidad del dominio**: worker = DDD-lite (dominio rico de evaluación), Go API = hexagonal (orquestación), web = cohesión por feature (presentación). No por etiqueta de servicio.

## Cambios completados

_(se actualiza al cerrar cada issue)_
