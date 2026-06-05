.PHONY: test test-go test-worker test-web test-rls test-all

test-go:
	cd api && go test ./... -count=1

test-worker:
	cd worker && npm test

test-web:
	cd web && npm test -- --run

test-rls:
	@echo "Starting postgres for pgTAP..."
	docker compose up -d postgres
	@echo "Waiting for postgres to be healthy..."
	@until docker compose exec -T postgres pg_isready -U careerops; do sleep 1; done
	@echo "Installing pgTAP..."
	docker compose exec -T postgres bash -c "apt-get update -qq && apt-get install -y -qq pgtap 2>/dev/null || true"
	@echo "Running RLS tests..."
	docker compose exec -T postgres psql -U careerops -d careerops -f /docker-entrypoint-initdb.d/001_initial.sql 2>/dev/null || true
	docker compose run --rm -v $(PWD)/db/tests:/tests postgres psql -U careerops -h postgres -d careerops -c "CREATE EXTENSION IF NOT EXISTS pgtap;" 2>/dev/null || true
	docker compose run --rm -v $(PWD)/db/tests:/tests postgres pg_prove -U careerops -h postgres -d careerops /tests/rls_test.sql
	docker compose stop postgres

test-all: test-go test-worker test-web
	@echo "All unit tests passed. Run 'make test-rls' for DB integration tests (requires Docker)."
