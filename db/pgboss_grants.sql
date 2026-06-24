-- Grant the restricted runtime role (app_user) DML on the pg-boss schema.
--
-- The pgboss schema is OWNED by the admin role and installed out-of-band by
-- worker/scripts/install-pgboss.mjs (app_user has no CREATE on the database,
-- so the worker runs pg-boss with migrate:false). This file gives app_user
-- exactly the privileges pg-boss needs at runtime and nothing more.
--
-- Run as the schema owner (admin), AFTER install-pgboss.mjs has created it:
--   psql "$PGBOSS_ADMIN_URL" -f db/pgboss_grants.sql

GRANT USAGE ON SCHEMA pgboss TO app_user;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA pgboss TO app_user;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA pgboss TO app_user;

-- Cover any pg-boss objects created later (queue/partition tables) so a future
-- pg-boss minor upgrade doesn't silently break the runtime role.
ALTER DEFAULT PRIVILEGES IN SCHEMA pgboss
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA pgboss
  GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO app_user;
